// Command trace runs one project in the compile sandbox under strace and lists what it touched
// that matters to the sandbox (docs/compile.md, "Tracing a compile"): every path Landlock (or
// anything else) refused, every write outside /work and /tmp, the writes under /tmp by folder,
// and the devices it opened. make compile-trace runs it. Development only: it uses the image
// researchsphere-compile:debug (compile/image/debug, the sandbox image plus strace), with the
// worker's own container settings (sandbox.Runner.Spec), only the entrypoint wrapped in strace.
// strace starts rs-landlock and so stays outside its Landlock domain: it sees every refusal.
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/docker"
	"github.com/ARAFATH-SH/researchsphere/compile/internal/sandbox"
)

func main() {
	dir := flag.String("dir", "", "the project folder to compile")
	mainFile := flag.String("main", "main.tex", "the main file, relative to -dir")
	image := flag.String("image", "researchsphere-compile:debug", "the debug image (make compile-trace builds it)")
	out := flag.String("out", "", "keep the raw trace and the log in this folder")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: trace -dir PROJECT [-main main.tex] [-out FOLDER]")
		os.Exit(2)
	}
	if err := run(*dir, *mainFile, *image, *out, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "trace:", err)
		os.Exit(1)
	}
}

// straceArgs wrap the image's entrypoint: every call that names a file, with long strings, in
// every child process.
var straceArgs = []string{"/usr/bin/strace", "-f", "-qq", "-s", "512", "-e", "trace=%file", "--"}

func run(dir, mainFile, image, keep string, w io.Writer) error {
	input, err := tarOf(dir)
	if err != nil {
		return err
	}
	dc, err := docker.New(os.Getenv("COMPILE_DOCKER_HOST"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	r := sandbox.New(sandbox.FromClient(dc), sandbox.Config{Image: image, MaxPDFBytes: 100 << 20}, nil)
	spec := r.Spec(sandbox.Job{ID: "trace", MainFile: mainFile})
	spec.Entrypoint = append(slices.Clone(straceArgs), sandbox.LandlockPath, "--", "/usr/local/bin/rs-compile")
	spec.Labels = map[string]string{sandbox.Label: "1", sandbox.Label + ".job": "trace"}

	id, err := dc.Create(ctx, fmt.Sprintf("rs-compile-trace-%d", time.Now().UnixNano()), spec)
	if err != nil {
		return err
	}
	defer func() { _ = dc.Remove(context.Background(), id) }()
	att, err := dc.Attach(ctx, id)
	if err != nil {
		return err
	}
	defer func() { _ = att.Close() }()
	if err := dc.Start(ctx, id); err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- att.Demux(&stdout, &stderr) }()
	if _, err := att.Write(input); err != nil {
		return err
	}
	if err := att.CloseStdin(); err != nil {
		return err
	}
	code, err := dc.Wait(ctx, id)
	if err != nil {
		return err
	}
	if err := <-done; err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	outputs := untar(stdout.Bytes())
	if keep != "" {
		if err := os.MkdirAll(keep, 0o750); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(keep, "trace.txt"), stderr.Bytes(), 0o600)
		_ = os.WriteFile(filepath.Join(keep, "output.log"), outputs["output.log"], 0o600)
		_ = os.WriteFile(filepath.Join(keep, "output.blg"), outputs["output.blg"], 0o600)
		if pdf, ok := outputs["output.pdf"]; ok {
			_ = os.WriteFile(filepath.Join(keep, "output.pdf"), pdf, 0o600)
		}
	}
	rep := analyse(stderr.String())
	fmt.Fprintf(w, "exit %d, status %q, PDF %d bytes, log %d bytes\n", code,
		strings.TrimSpace(string(outputs["status"])), len(outputs["output.pdf"]), len(outputs["output.log"]))
	rep.print(w)
	return nil
}

// call is one traced system call that names a file.
type call struct {
	name, path, flags, result string
}

var (
	callRe   = regexp.MustCompile(`^(?:\[pid\s+)?\d*\]?\s*([a-z0-9_]+)\((.*)\)\s+=\s+(.*)$`)
	pathRe   = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	unfinRe  = regexp.MustCompile(`^(?:\[pid\s+)?(\d+)\]?\s*(.*) <unfinished \.\.\.>$`)
	resumeRe = regexp.MustCompile(`^(?:\[pid\s+)?(\d+)\]?\s*<\.\.\. [a-z0-9_]+ resumed>(.*)$`)
)

// writeCalls change the filesystem whatever their flags.
var writeCalls = map[string]bool{
	"mkdir": true, "mkdirat": true, "unlink": true, "unlinkat": true, "rmdir": true,
	"rename": true, "renameat": true, "renameat2": true, "creat": true, "link": true, "linkat": true,
	"symlink": true, "symlinkat": true, "truncate": true, "chmod": true, "fchmodat": true,
	"chown": true, "fchownat": true, "lchown": true, "utimensat": true, "mknod": true, "mknodat": true,
}

func parse(trace string) []call {
	pending := map[string]string{}
	var calls []call
	sc := bufio.NewScanner(strings.NewReader(trace))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if m := unfinRe.FindStringSubmatch(line); m != nil {
			pending[m[1]] = m[2]
			continue
		}
		if m := resumeRe.FindStringSubmatch(line); m != nil {
			line = pending[m[1]] + m[2]
			delete(pending, m[1])
		}
		m := callRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		p := pathRe.FindStringSubmatch(m[2])
		if p == nil {
			continue
		}
		calls = append(calls, call{name: m[1], path: p[1], flags: m[2], result: m[3]})
	}
	return calls
}

func (c call) writes() bool {
	if writeCalls[c.name] {
		return true
	}
	if c.name == "open" || c.name == "openat" || c.name == "openat2" {
		return strings.Contains(c.flags, "O_WRONLY") || strings.Contains(c.flags, "O_RDWR") || strings.Contains(c.flags, "O_CREAT")
	}
	return false
}

func (c call) refused() string {
	for _, e := range []string{"EACCES", "EPERM", "EROFS"} {
		if strings.HasPrefix(c.result, "-1 "+e) {
			return e
		}
	}
	return ""
}

// inScratch says whether a path is in /work or /tmp; a relative path is in /work (the
// compile's working folder).
func inScratch(p string) bool {
	return !strings.HasPrefix(p, "/") || p == "/work" || p == "/tmp" || strings.HasPrefix(p, "/work/") || strings.HasPrefix(p, "/tmp/")
}

type report struct {
	refused   map[string]map[string]int // path -> "call error" -> count
	writesOut map[string]map[string]int // path -> "call result" -> count
	writesTmp map[string]int            // folder under /tmp -> count
	devices   map[string]map[string]int
	calls     int
}

func analyse(trace string) report {
	rep := report{refused: map[string]map[string]int{}, writesOut: map[string]map[string]int{}, writesTmp: map[string]int{}, devices: map[string]map[string]int{}}
	add := func(m map[string]map[string]int, k, v string) {
		if m[k] == nil {
			m[k] = map[string]int{}
		}
		m[k][v]++
	}
	for _, c := range parse(trace) {
		rep.calls++
		res := c.result
		if i := strings.Index(res, " ("); i > 0 {
			res = res[:i]
		}
		if e := c.refused(); e != "" {
			add(rep.refused, c.path, c.name+" "+e)
		}
		if strings.HasPrefix(c.path, "/dev/") {
			mode := "read"
			if c.writes() {
				mode = "write"
			}
			add(rep.devices, c.path, c.name+" "+mode+" = "+res)
			continue
		}
		if !c.writes() {
			continue
		}
		switch {
		case !inScratch(c.path):
			add(rep.writesOut, c.path, c.name+" = "+res)
		case strings.HasPrefix(c.path, "/tmp/"):
			parts := strings.SplitN(strings.TrimPrefix(c.path, "/tmp/"), "/", 3)
			key := "/tmp/" + parts[0]
			if len(parts) > 1 {
				key += "/" + parts[1]
			}
			rep.writesTmp[key]++
		}
	}
	return rep
}

func (r report) print(w io.Writer) {
	fmt.Fprintf(w, "%d file calls traced\n", r.calls)
	section := func(title string, m map[string]map[string]int) {
		fmt.Fprintf(w, "\n%s: %d\n", title, len(m))
		for _, k := range sortedKeys(m) {
			var parts []string
			for _, v := range sortedKeys(m[k]) {
				parts = append(parts, fmt.Sprintf("%s x%d", v, m[k][v]))
			}
			fmt.Fprintf(w, "  %s  [%s]\n", k, strings.Join(parts, ", "))
		}
	}
	section("Refused paths (EACCES, EPERM, EROFS)", r.refused)
	section("Writes outside /work and /tmp", r.writesOut)
	fmt.Fprintf(w, "\nWrites under /tmp, by folder: %d\n", len(r.writesTmp))
	for _, k := range sortedKeys(r.writesTmp) {
		fmt.Fprintf(w, "  %s  x%d\n", k, r.writesTmp[k])
	}
	section("Devices", r.devices)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func untar(b []byte) map[string][]byte {
	out := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := tr.Next()
		if err != nil {
			return out
		}
		data, _ := io.ReadAll(io.LimitReader(tr, 200<<20))
		out[h.Name] = data
	}
}

func tarOf(dir string) ([]byte, error) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: rel + "/", Mode: 0o755})
		}
		data, err := os.ReadFile(p) //nolint:gosec // a developer's own project folder
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: rel, Size: int64(len(data)), Mode: 0o644}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
