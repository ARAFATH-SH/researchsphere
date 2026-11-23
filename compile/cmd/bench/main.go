// Command bench measures NFR-3 ("a 10-page document shall compile within 20 seconds"): it
// compiles the sample paper in compile/bench/sample (sections, maths, a TikZ figure, a table
// and 20 BibTeX references) in the real sandbox several times and prints the times. make
// bench-compile runs it; docs/evaluation.md keeps the result.
package main

import (
	"archive/tar"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/docker"
	"github.com/ARAFATH-SH/researchsphere/compile/internal/sandbox"
)

var pagesRe = regexp.MustCompile(`Output written on .+ \((\d+) pages?`)

func main() {
	dir := flag.String("dir", "bench/sample", "the sample project")
	runs := flag.Int("runs", 5, "compiles to time")
	image := flag.String("image", os.Getenv("COMPILE_IMAGE"), "the sandbox image")
	flag.Parse()
	if err := run(*dir, *runs, *image); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

func run(dir string, runs int, image string) error {
	if image == "" {
		image = "researchsphere-compile:dev"
	}
	files, err := tarOf(dir)
	if err != nil {
		return err
	}
	dc, err := docker.New(os.Getenv("COMPILE_DOCKER_HOST"))
	if err != nil {
		return err
	}
	r := sandbox.New(sandbox.FromClient(dc), sandbox.Config{Image: image, MaxPDFBytes: 100 << 20, Runtime: os.Getenv("COMPILE_RUNTIME")}, nil)
	ctx := context.Background()
	var times []time.Duration
	pages := ""
	for i := range runs {
		start := time.Now()
		res, err := r.Run(ctx, sandbox.Job{ID: fmt.Sprintf("bench-%d", i), MainFile: "main.tex", Input: bytes.NewReader(files)})
		total := time.Since(start)
		if err != nil {
			return err
		}
		if res.PDF == nil {
			return fmt.Errorf("run %d made no PDF (timed out %v, failure %q):\n%s", i+1, res.TimedOut, res.Failure, res.Log)
		}
		_ = res.PDF.Close()
		_ = os.Remove(res.PDF.Name())
		if m := pagesRe.FindStringSubmatch(res.Log); m != nil {
			pages = m[1]
		}
		times = append(times, total)
		fmt.Printf("run %d: %.2f s (sandbox %.2f s), PDF %d bytes\n", i+1, total.Seconds(), res.Duration.Seconds(), res.PDFSize)
	}
	slices.Sort(times)
	fmt.Printf("\nNFR-3 sample: %s pages, %d runs, p50 %.2f s, max %.2f s (target: under 20 s): %s\n",
		pages, runs, times[len(times)/2].Seconds(), times[len(times)-1].Seconds(), verdict(times[len(times)-1]))
	return nil
}

func verdict(d time.Duration) string {
	if d < 20*time.Second {
		return "PASS"
	}
	return "FAIL"
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
		data, err := os.ReadFile(p) //nolint:gosec // the bench's own sample
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
	return b.Bytes(), tw.Close()
}
