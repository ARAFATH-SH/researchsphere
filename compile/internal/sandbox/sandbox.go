// Package sandbox runs one LaTeX compile in one container (NFR-4, docs/compile.md): no
// network, a read-only root filesystem, a non-root user, no capabilities, no new privileges,
// the default seccomp and AppArmor profiles, 1 CPU, 1 GiB of memory without swap, 64
// processes, and a 60-second hard kill (FR-13.5). The project's files go in as a tar on the
// container's stdin into a memory-backed /work, and the PDF and logs come out as a tar on its
// stdout: nothing is mounted from the host, so the container sees this project's files only
// and cannot fill the host's disk, and nothing is left behind when it is removed.
package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/docker"
)

// The limits of NFR-4 and FR-13.5. They are constants, not settings.
const (
	HardTimeout = 60 * time.Second
	// InnerTimeout is when the entrypoint stops LaTeX itself, so its log still comes out; the
	// hard kill at HardTimeout is the backstop.
	InnerTimeout = 55 * time.Second
	MemoryBytes  = 1 << 30
	NanoCPUs     = 1_000_000_000
	PidsLimit    = 64
	// WorkBytes is the memory-backed /work: the project's files, the outputs and LaTeX's
	// own files. Its pages count against MemoryBytes.
	WorkBytes = 768 << 20
	TmpBytes  = 64 << 20
	// UID and GID run the compile; the image has no other user.
	UID = 10001
	GID = 10001

	MaxLogBytes       = 2 << 20
	MaxBibtexLogBytes = 256 << 10
	maxStderrBytes    = 16 << 10

	// Label marks every compile container, so the worker can find and remove leftovers.
	Label = "org.researchsphere.compile"

	// LandlockPath is the sandbox's file wall in the image (compile/image/landlock): the
	// image's entrypoint runs every compile under it. exitUnavailable and unavailableMarker
	// are how it says it cannot enforce its rules; they mirror that program.
	LandlockPath      = "/usr/local/bin/rs-landlock"
	exitUnavailable   = 78
	unavailableMarker = "rs-landlock: sandbox unavailable: "
)

// ErrUnavailable means the sandbox cannot enforce its file rules (no Landlock, or too old a
// one): no compile may run on this host (docs/compile.md, docs/deploy.md).
var ErrUnavailable = errors.New("sandbox: the sandbox cannot enforce its file rules (Linux with Landlock ABI 8 needed, docs/deploy.md)")

// Config is the runner's settings.
type Config struct {
	Image         string // pinned by digest
	Runtime       string // "" (runc) or "runsc" (gVisor)
	MaxPDFBytes   int64
	MaxInputBytes int64
	// Hard is the hard kill; tests shorten it. 0 means HardTimeout.
	Hard time.Duration
	// Inner is the entrypoint's own limit in seconds; 0 means InnerTimeout.
	Inner time.Duration
}

// Docker is the part of the daemon the runner uses.
type Docker interface {
	Create(ctx context.Context, name string, spec docker.ContainerSpec) (string, error)
	Attach(ctx context.Context, id string) (Stream, error)
	Start(ctx context.Context, id string) error
	Wait(ctx context.Context, id string) (int, error)
	Inspect(ctx context.Context, id string) (docker.State, error)
	Kill(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	List(ctx context.Context, key, value string) ([]docker.Listed, error)
}

// Stream is an attached container's stdin and multiplexed output.
type Stream interface {
	io.Writer
	CloseStdin() error
	Demux(stdout, stderr io.Writer) error
	Close() error
}

// Job is one compile to run.
type Job struct {
	ID       string
	MainFile string    // project path of the main .tex file
	Input    io.Reader // the project's files as a tar, as the API sends them
}

// Failure says why a run ended without its outputs; the values are the API's.
type Failure string

// Failures the runner reports.
const (
	FailOutputTooLarge Failure = "output_too_large"
	FailOutOfMemory    Failure = "out_of_memory"
	FailSandbox        Failure = "sandbox_error"
)

// Result is how a run ended. PDF is a temporary file the caller removes; nil without a PDF.
type Result struct {
	TimedOut  bool
	Failure   Failure // "" when the run completed
	ExitCode  *int
	Duration  time.Duration
	PDF       *os.File
	PDFSize   int64
	Log       string
	BibtexLog string
	Stderr    string
	// Unavailable: the sandbox refused to run because it cannot enforce its file rules
	// (ErrUnavailable). The worker stops: no compile may run without them.
	Unavailable bool
}

// Runner runs compiles.
type Runner struct {
	docker Docker
	cfg    Config
	logger *slog.Logger
}

// New returns a Runner.
func New(d Docker, cfg Config, logger *slog.Logger) *Runner {
	if cfg.Hard <= 0 {
		cfg.Hard = HardTimeout
	}
	if cfg.Inner <= 0 {
		cfg.Inner = InnerTimeout
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Runner{docker: d, cfg: cfg, logger: logger}
}

// mainPath is a project path to a .tex file (the API's path rules: no leading dot, no "..").
var mainPath = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*(/[A-Za-z0-9_-][A-Za-z0-9_.-]*)*\.[tT][eE][xX]$`)

// ValidMain reports whether p is a main file the runner accepts.
func ValidMain(p string) bool {
	return len(p) <= 255 && mainPath.MatchString(p) && !strings.Contains(p, "..")
}

// Spec is the container a job runs in. Every isolation setting of NFR-4 is here; the tests
// check each one.
func (r *Runner) Spec(job Job) docker.ContainerSpec {
	return docker.ContainerSpec{
		Image: r.cfg.Image,
		Cmd:   []string{job.MainFile},
		Env: []string{
			"RS_MAX_PDF_BYTES=" + strconv.FormatInt(r.cfg.MaxPDFBytes, 10),
			"RS_MAX_LOG_BYTES=" + strconv.Itoa(MaxLogBytes),
			"RS_MAX_BLG_BYTES=" + strconv.Itoa(MaxBibtexLogBytes),
			"RS_TIMEOUT=" + strconv.Itoa(int(r.cfg.Inner.Seconds())),
		},
		User:       strconv.Itoa(UID) + ":" + strconv.Itoa(GID),
		WorkingDir: "/work",
		Labels:     map[string]string{Label: "1", Label + ".job": job.ID},
		// stdin carries the files in; it closes after them.
		AttachStdin: true, AttachStdout: true, AttachStderr: true, OpenStdin: true, StdinOnce: true,
		NetworkDisabled: true,
		HostConfig: docker.HostConfig{
			NetworkMode:    "none",
			ReadonlyRootfs: true,
			CapDrop:        []string{"ALL"},
			SecurityOpt:    []string{"no-new-privileges:true"},
			Tmpfs: map[string]string{
				"/work": fmt.Sprintf("rw,noexec,nosuid,nodev,size=%d,mode=0700,uid=%d,gid=%d", WorkBytes, UID, GID),
				"/tmp":  fmt.Sprintf("rw,noexec,nosuid,nodev,size=%d,mode=1777", TmpBytes),
			},
			Memory:     MemoryBytes,
			MemorySwap: MemoryBytes, // equal to Memory: no swap
			NanoCPUs:   NanoCPUs,
			PidsLimit:  PidsLimit,
			Ulimits: []docker.Ulimit{
				{Name: "nofile", Soft: 1024, Hard: 1024},
				{Name: "core", Soft: 0, Hard: 0},
			},
			IpcMode:   "none",
			Init:      true,
			Runtime:   r.cfg.Runtime,
			LogConfig: docker.LogConfig{Type: "none"}, // the PDF never lands in the daemon's logs
		},
	}
}

// Run compiles job in a new container and removes the container whatever happens. The error
// is for a run that could not start at all (no image, no daemon); a run that started reports
// its end in Result.
func (r *Runner) Run(ctx context.Context, job Job) (Result, error) {
	if !ValidMain(job.MainFile) {
		return Result{}, fmt.Errorf("sandbox: main file %q is not a .tex path", job.MainFile)
	}
	id, err := r.docker.Create(ctx, "rs-compile-"+job.ID, r.Spec(job))
	if err != nil {
		return Result{}, fmt.Errorf("sandbox: create: %w", err)
	}
	defer r.remove(id)

	attachCtx, cancelAttach := context.WithTimeout(ctx, 10*time.Second)
	stream, err := r.docker.Attach(attachCtx, id)
	cancelAttach()
	if err != nil {
		return Result{}, fmt.Errorf("sandbox: attach: %w", err)
	}
	defer func() { _ = stream.Close() }()

	out := newCollector(r.cfg.MaxPDFBytes)
	defer out.discard()
	pr, pw := io.Pipe()
	stderr := &capped{max: maxStderrBytes}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		err := stream.Demux(pw, stderr)
		_ = pw.CloseWithError(err)
	}()
	go func() {
		defer wg.Done()
		out.read(pr)
		_, _ = io.Copy(io.Discard, pr) // keep the stream moving after a bad tar
	}()

	start := time.Now()
	if err := r.docker.Start(ctx, id); err != nil {
		_ = stream.Close()
		wg.Wait()
		return Result{}, fmt.Errorf("sandbox: start: %w", err)
	}
	// The hard limit (FR-13.5) runs from the start, whatever the container does.
	killed := make(chan struct{})
	hard := time.AfterFunc(r.cfg.Hard, func() {
		close(killed)
		kctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := r.docker.Kill(kctx, id); err != nil {
			r.logger.Error("hard kill failed", slog.String("job_id", job.ID), slog.Any("error", err))
		}
		_ = stream.Close()
	})
	defer hard.Stop()

	inputErr := make(chan error, 1)
	go func() {
		err := Sanitize(job.Input, stream, r.cfg.MaxInputBytes)
		if cerr := stream.CloseStdin(); err == nil {
			err = cerr
		}
		inputErr <- err
	}()

	waitCtx, cancelWait := context.WithTimeout(context.WithoutCancel(ctx), r.cfg.Hard+30*time.Second)
	code, waitErr := r.docker.Wait(waitCtx, id)
	cancelWait()
	hard.Stop()
	res := Result{Duration: time.Since(start)}
	wg.Wait()
	ierr := <-inputErr

	timedOut := false
	select {
	case <-killed:
		timedOut = true
	default:
	}
	if waitErr != nil && !timedOut {
		return r.failed(res, FailSandbox, fmt.Errorf("wait: %w", waitErr), job.ID), nil
	}
	res.ExitCode = &code
	res.Stderr = stderr.String()
	if code == exitUnavailable && strings.Contains(res.Stderr, unavailableMarker) {
		res.Unavailable = true
		return r.failed(res, FailSandbox, ErrUnavailable, job.ID), nil
	}
	if timedOut {
		res.TimedOut = true
		res.Log, res.BibtexLog = out.log.String(), out.blg.String()
		return res, nil
	}
	if ierr != nil {
		return r.failed(res, FailSandbox, fmt.Errorf("input: %w", ierr), job.ID), nil
	}
	st, err := r.docker.Inspect(context.WithoutCancel(ctx), id)
	if err == nil && st.OOMKilled {
		res.Failure = FailOutOfMemory
		res.Log, res.BibtexLog = out.log.String(), out.blg.String()
		return res, nil
	}
	if out.err != nil {
		return r.failed(res, FailSandbox, out.err, job.ID), nil
	}
	res.Log, res.BibtexLog = out.log.String(), out.blg.String()
	status := out.status
	switch {
	case status["timedout"] == "1":
		res.TimedOut = true
	case out.pdfTooLarge || status["pdf"] == "too_large":
		res.Failure = FailOutputTooLarge
	case status["exit"] == "":
		return r.failed(res, FailSandbox, errors.New("no status from the sandbox: "+res.Stderr), job.ID), nil
	}
	if res.Failure == "" && !res.TimedOut && out.pdf != nil {
		res.PDF, res.PDFSize = out.pdf, out.pdfSize
		out.pdf = nil // the caller owns it now
	}
	return res, nil
}

// Probe runs rs-landlock --probe in a container with the compile's settings: it restricts
// itself, checks that /etc/passwd cannot be opened and /work can be written, and prints
// "landlock abi=N ok". The worker refuses to start unless it succeeds. The error wraps
// ErrUnavailable when the sandbox answered that it cannot enforce its rules.
func (r *Runner) Probe(ctx context.Context) (string, error) {
	spec := r.Spec(Job{ID: "probe", MainFile: "main.tex"})
	spec.Entrypoint, spec.Cmd = []string{LandlockPath, "--probe"}, nil
	id, err := r.docker.Create(ctx, "rs-compile-probe-"+strconv.FormatInt(time.Now().UnixNano(), 36), spec)
	if err != nil {
		return "", fmt.Errorf("sandbox: probe: create: %w", err)
	}
	defer r.remove(id)
	stream, err := r.docker.Attach(ctx, id)
	if err != nil {
		return "", fmt.Errorf("sandbox: probe: attach: %w", err)
	}
	defer func() { _ = stream.Close() }()
	var stdout, stderr capped
	stdout.max, stderr.max = maxStderrBytes, maxStderrBytes
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = stream.Demux(&stdout, &stderr)
	}()
	if err := r.docker.Start(ctx, id); err != nil {
		return "", fmt.Errorf("sandbox: probe: start: %w", err)
	}
	_ = stream.CloseStdin()
	wctx, cancel := context.WithTimeout(ctx, r.cfg.Hard)
	defer cancel()
	code, err := r.docker.Wait(wctx, id)
	if err != nil {
		_ = stream.Close()
		<-done
		return "", fmt.Errorf("sandbox: probe: wait: %w", err)
	}
	<-done
	out := strings.TrimSpace(stdout.String())
	if code == 0 && strings.HasPrefix(out, "landlock abi=") && strings.HasSuffix(out, " ok") {
		return out, nil
	}
	return "", fmt.Errorf("%w: the probe exited %d: %s", ErrUnavailable, code, strings.TrimSpace(stderr.String()+" "+out))
}

func (r *Runner) failed(res Result, f Failure, err error, jobID string) Result {
	r.logger.Error("compile sandbox failed", slog.String("job_id", jobID), slog.Any("error", err), slog.String("stderr", res.Stderr))
	res.Failure = f
	return res
}

// remove deletes the container with a context of its own: it runs after every run, killed,
// failed or cancelled.
func (r *Runner) remove(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.docker.Remove(ctx, id); err != nil {
		r.logger.Error("compile container not removed; the sweep tries again", slog.String("container", id), slog.Any("error", err))
	}
}

// Sweep removes compile containers older than age: leftovers of a worker that crashed. It
// runs when the worker starts and every minute.
func (r *Runner) Sweep(ctx context.Context, age time.Duration) (int, error) {
	list, err := r.docker.List(ctx, Label, "1")
	if err != nil {
		return 0, err
	}
	n := 0
	cut := time.Now().Add(-age).Unix()
	for _, c := range list {
		if c.Created > cut {
			continue
		}
		if err := r.docker.Remove(ctx, c.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// collector reads the entrypoint's output tar: status, output.pdf (to a temporary file, at
// most maxPDF bytes), output.log and output.blg (capped).
type collector struct {
	maxPDF      int64
	status      map[string]string
	pdf         *os.File
	pdfSize     int64
	pdfTooLarge bool
	log         capped
	blg         capped
	err         error
}

func newCollector(maxPDF int64) *collector {
	return &collector{maxPDF: maxPDF, log: capped{max: MaxLogBytes}, blg: capped{max: MaxBibtexLogBytes}}
}

func (c *collector) read(r io.Reader) {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			c.err = fmt.Errorf("read output: %w", err)
			return
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		switch path.Clean(strings.TrimPrefix(h.Name, "./")) {
		case "status":
			b, _ := io.ReadAll(io.LimitReader(tr, 4096))
			c.status = parseStatus(string(b))
		case "output.log":
			_, _ = io.Copy(&c.log, io.LimitReader(tr, MaxLogBytes+1))
		case "output.blg":
			_, _ = io.Copy(&c.blg, io.LimitReader(tr, MaxBibtexLogBytes+1))
		case "output.pdf":
			if h.Size > c.maxPDF {
				c.pdfTooLarge = true
				continue
			}
			f, err := os.CreateTemp("", "compile-*.pdf")
			if err != nil {
				c.err = err
				return
			}
			n, err := io.Copy(f, io.LimitReader(tr, c.maxPDF+1))
			if err != nil {
				_ = f.Close()
				_ = os.Remove(f.Name())
				c.err = err
				return
			}
			if n > c.maxPDF {
				c.pdfTooLarge = true
				_ = f.Close()
				_ = os.Remove(f.Name())
				continue
			}
			c.pdf, c.pdfSize = f, n
		}
	}
}

func (c *collector) discard() {
	if c.pdf != nil {
		_ = c.pdf.Close()
		_ = os.Remove(c.pdf.Name())
	}
}

func parseStatus(s string) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			m[k] = v
		}
	}
	return m
}

// capped keeps the first max bytes written and drops the rest.
type capped struct {
	buf bytes.Buffer
	max int
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
			c.cut = true
		} else {
			c.buf.Write(p)
		}
	} else if len(p) > 0 {
		c.cut = true
	}
	return len(p), nil
}

// String is the kept text as valid UTF-8.
func (c *capped) String() string { return strings.ToValidUTF8(c.buf.String(), "�") }

// FromClient adapts the Docker client to the runner.
func FromClient(c *docker.Client) Docker { return clientAdapter{c} }

type clientAdapter struct{ *docker.Client }

func (a clientAdapter) Attach(ctx context.Context, id string) (Stream, error) {
	return a.Client.Attach(ctx, id)
}
