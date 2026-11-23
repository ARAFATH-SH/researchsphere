package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/docker"
)

// NFR-4: every isolation setting is in the container spec.
func TestSpecIsolation(t *testing.T) {
	r := New(nil, Config{Image: "img@sha256:x", MaxPDFBytes: 100 << 20}, nil)
	s := r.Spec(Job{ID: "j1", MainFile: "paper/main.tex"})
	h := s.HostConfig
	checks := []struct {
		name string
		ok   bool
	}{
		{"no network", h.NetworkMode == "none" && s.NetworkDisabled},
		{"read-only root", h.ReadonlyRootfs},
		{"non-root user", s.User == "10001:10001"},
		{"no capabilities", slices.Equal(h.CapDrop, []string{"ALL"})},
		{"no new privileges", slices.Contains(h.SecurityOpt, "no-new-privileges:true")},
		{"default seccomp", !slices.ContainsFunc(h.SecurityOpt, func(o string) bool { return strings.Contains(o, "unconfined") })},
		{"not privileged", !h.Privileged},
		{"1 CPU", h.NanoCPUs == 1_000_000_000},
		{"1 GiB", h.Memory == 1<<30},
		{"no swap", h.MemorySwap == h.Memory},
		{"process limit", h.PidsLimit == 64},
		{"memory-backed /work with a size", strings.Contains(h.Tmpfs["/work"], "size=") && strings.Contains(h.Tmpfs["/work"], "noexec")},
		{"memory-backed /tmp with a size", strings.Contains(h.Tmpfs["/tmp"], "size=67108864")},
		{"nothing mounted from the host", len(h.Binds) == 0},
		{"no logs on the host", h.LogConfig.Type == "none"},
		{"no IPC", h.IpcMode == "none"},
		{"no core dumps", slices.Contains(h.Ulimits, docker.Ulimit{Name: "core"})},
		{"labelled", s.Labels[Label] == "1" && s.Labels[Label+".job"] == "j1"},
		{"main file as the only argument", slices.Equal(s.Cmd, []string{"paper/main.tex"})},
		{"stdin closes after the files", s.OpenStdin && s.StdinOnce && !s.Tty},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s: %+v", c.name, s)
		}
	}
	if New(nil, Config{Runtime: "runsc"}, nil).Spec(Job{}).HostConfig.Runtime != "runsc" {
		t.Error("the runtime setting is not passed")
	}
}

func TestValidMain(t *testing.T) {
	for p, want := range map[string]bool{
		"main.tex": true, "paper/Thesis.TEX": true, "a_b-c/d.e.tex": true,
		"": false, "/etc/x.tex": false, "../x.tex": false, "a/../x.tex": false, ".hidden.tex": false,
		"main.pdf": false, "a b.tex": false, "main.tex;rm": false, "-x.tex": true,
	} {
		if ValidMain(p) != want {
			t.Errorf("ValidMain(%q) = %v", p, !want)
		}
	}
}

func tarOf(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, h := range entries {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write(bytes.Repeat([]byte("x"), int(h.Size)))
		}
	}
	_ = tw.Close()
	return b.Bytes()
}

// Only folders and regular files at clean relative paths pass (defense in depth).
func TestSanitize(t *testing.T) {
	good := tarOf(t,
		&tar.Header{Typeflag: tar.TypeDir, Name: "chapters/", Mode: 0o777, Uid: 0},
		&tar.Header{Typeflag: tar.TypeReg, Name: "chapters/a.tex", Size: 3, Mode: 0o4777, Uname: "root"},
	)
	var out bytes.Buffer
	if err := Sanitize(bytes.NewReader(good), &out, 0); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&out)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Mode&0o7000 != 0 || h.Uname != "" {
			t.Errorf("%s kept mode %o owner %q", h.Name, h.Mode, h.Uname)
		}
	}
	bad := []*tar.Header{
		{Typeflag: tar.TypeSymlink, Name: "x.tex", Linkname: "/etc/passwd"},
		{Typeflag: tar.TypeLink, Name: "x.tex", Linkname: "main.tex"},
		{Typeflag: tar.TypeReg, Name: "../x.tex", Size: 1},
		{Typeflag: tar.TypeReg, Name: "/etc/x", Size: 1},
		{Typeflag: tar.TypeReg, Name: "a/./b.tex", Size: 1},
		{Typeflag: tar.TypeReg, Name: ".latexmkrc", Size: 1},
		{Typeflag: tar.TypeChar, Name: "dev"},
		{Typeflag: tar.TypeFifo, Name: "fifo"},
	}
	for _, h := range bad {
		if err := Sanitize(bytes.NewReader(tarOf(t, h)), io.Discard, 0); !errors.Is(err, ErrBadInput) {
			t.Errorf("%q (type %c): err = %v", h.Name, h.Typeflag, err)
		}
	}
	if err := Sanitize(bytes.NewReader(tarOf(t, &tar.Header{Typeflag: tar.TypeReg, Name: "a", Size: 10})), io.Discard, 9); !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("over the input limit: %v", err)
	}
}

func frame(stream byte, p []byte) []byte {
	h := make([]byte, 8)
	h[0] = stream
	binary.BigEndian.PutUint32(h[4:], uint32(len(p))) //nolint:gosec // test sizes
	return append(h, p...)
}

func TestDemux(t *testing.T) {
	in := append(frame(1, []byte("out1")), frame(2, []byte("err"))...)
	in = append(in, frame(1, []byte("out2"))...)
	var out, errb bytes.Buffer
	if err := docker.Demux(bytes.NewReader(in), &out, &errb); err != nil {
		t.Fatal(err)
	}
	if out.String() != "out1out2" || errb.String() != "err" {
		t.Errorf("stdout %q stderr %q", out.String(), errb.String())
	}
}

// fakeDocker plays the daemon: it records every call and answers with the output the test
// sets, after reading stdin as the container would.
type fakeDocker struct {
	mu        sync.Mutex
	created   []docker.ContainerSpec
	removed   []string
	killed    []string
	output    []byte // the entrypoint's stdout tar
	exit      int
	oom       bool
	hang      bool // never exits by itself
	stdin     bytes.Buffer
	createErr error
	list      []docker.Listed
	stop      chan struct{}
}

func (f *fakeDocker) Create(_ context.Context, _ string, spec docker.ContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", f.createErr
	}
	f.created = append(f.created, spec)
	f.stop = make(chan struct{})
	return "c1", nil
}

type fakeStream struct {
	f      *fakeDocker
	once   sync.Once
	closed chan struct{}
}

func (s *fakeStream) Write(p []byte) (int, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	return s.f.stdin.Write(p)
}

func (s *fakeStream) CloseStdin() error {
	if !s.f.hang {
		s.once.Do(func() { close(s.f.stop) })
	}
	return nil
}

func (s *fakeStream) Demux(stdout, _ io.Writer) error {
	select {
	case <-s.f.stop:
	case <-s.closed:
		return errors.New("closed")
	}
	_, err := stdout.Write(s.f.output)
	return err
}

func (s *fakeStream) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

func (f *fakeDocker) Attach(context.Context, string) (Stream, error) {
	return &fakeStream{f: f, closed: make(chan struct{})}, nil
}

func (f *fakeDocker) Start(context.Context, string) error { return nil }

func (f *fakeDocker) Wait(ctx context.Context, _ string) (int, error) {
	select {
	case <-f.stop:
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.exit, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (f *fakeDocker) Inspect(context.Context, string) (docker.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return docker.State{OOMKilled: f.oom, ExitCode: f.exit}, nil
}

func (f *fakeDocker) Kill(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed = append(f.killed, id)
	f.exit = 137
	select {
	case <-f.stop:
	default:
		close(f.stop)
	}
	return nil
}

func (f *fakeDocker) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeDocker) List(context.Context, string, string) ([]docker.Listed, error) {
	return f.list, nil
}

func outputTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, name := range []string{"status", "output.pdf", "output.log", "output.blg"} {
		c, ok := files[name]
		if !ok {
			continue
		}
		_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "./" + name, Size: int64(len(c)), Mode: 0o600})
		_, _ = io.WriteString(tw, c)
	}
	_ = tw.Close()
	return b.Bytes()
}

func input(t *testing.T) io.Reader {
	return bytes.NewReader(tarOf(t, &tar.Header{Typeflag: tar.TypeReg, Name: "main.tex", Size: 4}))
}

// The container is removed after a success, a failure and a kill (NFR-4: always removed).
func TestRunOutcomesAndCleanup(t *testing.T) {
	tests := []struct {
		name     string
		f        *fakeDocker
		check    func(t *testing.T, res Result)
		wantKill bool
	}{
		{"success", &fakeDocker{output: outputTar(t, map[string]string{"status": "exit=0\ntimedout=0\npdf=ok\n", "output.pdf": "%PDF-1.5", "output.log": "This is pdfTeX", "output.blg": "This is BibTeX"})},
			func(t *testing.T, res Result) {
				if res.PDF == nil || res.PDFSize != 8 || res.Log != "This is pdfTeX" || res.BibtexLog != "This is BibTeX" || res.Failure != "" || res.TimedOut {
					t.Errorf("result %+v", res)
				}
			}, false},
		{"LaTeX errors, no PDF", &fakeDocker{exit: 12, output: outputTar(t, map[string]string{"status": "exit=12\ntimedout=0\npdf=missing\n", "output.log": "! Emergency stop."})},
			func(t *testing.T, res Result) {
				if res.PDF != nil || res.Failure != "" || res.Log != "! Emergency stop." {
					t.Errorf("result %+v", res)
				}
			}, false},
		{"inner timeout", &fakeDocker{output: outputTar(t, map[string]string{"status": "exit=124\ntimedout=1\npdf=missing\n", "output.log": "loop"})},
			func(t *testing.T, res Result) {
				if !res.TimedOut || res.Log != "loop" {
					t.Errorf("result %+v", res)
				}
			}, false},
		{"hard kill", &fakeDocker{hang: true},
			func(t *testing.T, res Result) {
				if !res.TimedOut || res.PDF != nil {
					t.Errorf("result %+v", res)
				}
			}, true},
		{"PDF over the cap", &fakeDocker{output: outputTar(t, map[string]string{"status": "exit=0\ntimedout=0\npdf=ok\n", "output.pdf": "%PDF-" + strings.Repeat("x", 100)})},
			func(t *testing.T, res Result) {
				if res.PDF != nil || res.Failure != FailOutputTooLarge {
					t.Errorf("result %+v", res)
				}
			}, false},
		{"PDF the entrypoint found too large", &fakeDocker{output: outputTar(t, map[string]string{"status": "exit=0\ntimedout=0\npdf=too_large\n"})},
			func(t *testing.T, res Result) {
				if res.Failure != FailOutputTooLarge {
					t.Errorf("result %+v", res)
				}
			}, false},
		{"out of memory", &fakeDocker{exit: 137, oom: true},
			func(t *testing.T, res Result) {
				if res.Failure != FailOutOfMemory {
					t.Errorf("result %+v", res)
				}
			}, false},
		{"no status", &fakeDocker{exit: 1},
			func(t *testing.T, res Result) {
				if res.Failure != FailSandbox {
					t.Errorf("result %+v", res)
				}
			}, false},
		{"a log over its cap", &fakeDocker{output: outputTar(t, map[string]string{"status": "exit=0\ntimedout=0\npdf=missing\n", "output.log": strings.Repeat("l", MaxLogBytes+10)})},
			func(t *testing.T, res Result) {
				if len(res.Log) != MaxLogBytes {
					t.Errorf("log of %d bytes", len(res.Log))
				}
			}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(tt.f, Config{Image: "img", MaxPDFBytes: 64, Hard: 200 * time.Millisecond}, nil)
			res, err := r.Run(t.Context(), Job{ID: "j1", MainFile: "main.tex", Input: input(t)})
			if err != nil {
				t.Fatal(err)
			}
			if res.PDF != nil {
				defer func() { _ = res.PDF.Close() }()
			}
			tt.check(t, res)
			tt.f.mu.Lock()
			defer tt.f.mu.Unlock()
			if !slices.Equal(tt.f.removed, []string{"c1"}) {
				t.Errorf("removed %v, want the container removed once", tt.f.removed)
			}
			if (len(tt.f.killed) > 0) != tt.wantKill {
				t.Errorf("killed %v", tt.f.killed)
			}
		})
	}
}

func TestRunRefusals(t *testing.T) {
	f := &fakeDocker{}
	r := New(f, Config{Image: "img"}, nil)
	if _, err := r.Run(t.Context(), Job{ID: "j", MainFile: "../x.tex", Input: input(t)}); err == nil {
		t.Error("a main file outside the project was run")
	}
	if len(f.created) != 0 {
		t.Error("a container was created for a bad main file")
	}
	f.createErr = errors.New("no such image")
	if _, err := r.Run(t.Context(), Job{ID: "j", MainFile: "main.tex", Input: input(t)}); err == nil {
		t.Error("a failed create was not reported")
	}
	// A bad entry in the files stops the run, and the container still goes.
	f.createErr = nil
	f.output = outputTar(t, map[string]string{"status": "exit=0\ntimedout=0\npdf=missing\n"})
	bad := bytes.NewReader(tarOf(t, &tar.Header{Typeflag: tar.TypeSymlink, Name: "x", Linkname: "/etc/passwd"}))
	res, err := r.Run(t.Context(), Job{ID: "j", MainFile: "main.tex", Input: bad})
	if err != nil || res.Failure != FailSandbox {
		t.Errorf("bad input: %+v %v", res, err)
	}
	if !slices.Equal(f.removed, []string{"c1"}) {
		t.Errorf("removed %v", f.removed)
	}
}

func TestSweepRemovesOldContainers(t *testing.T) {
	now := time.Now().Unix()
	f := &fakeDocker{list: []docker.Listed{{ID: "old", Created: now - 600}, {ID: "new", Created: now}}}
	n, err := New(f, Config{}, nil).Sweep(t.Context(), 3*time.Minute)
	if err != nil || n != 1 || !slices.Equal(f.removed, []string{"old"}) {
		t.Errorf("sweep = %d %v, removed %v", n, err, f.removed)
	}
}
