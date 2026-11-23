package worker

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/apiclient"
	"github.com/ARAFATH-SH/researchsphere/compile/internal/sandbox"
)

type fakeAPI struct {
	mu        sync.Mutex
	sourceErr error
	pdfErr    error
	pdfs      []string
	finished  []apiclient.Result
}

func (f *fakeAPI) Claim(context.Context, string) (*apiclient.Job, error) { return nil, nil }

func (f *fakeAPI) Source(context.Context, string) (io.ReadCloser, error) {
	if f.sourceErr != nil {
		return nil, f.sourceErr
	}
	return io.NopCloser(strings.NewReader("tar")), nil
}

func (f *fakeAPI) StorePDF(_ context.Context, _ string, pdf io.Reader, _ int64) error {
	if f.pdfErr != nil {
		return f.pdfErr
	}
	b, _ := io.ReadAll(pdf)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pdfs = append(f.pdfs, string(b))
	return nil
}

func (f *fakeAPI) Finish(_ context.Context, _ string, r apiclient.Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, r)
	return nil
}

type fakeRunner struct {
	res sandbox.Result
	err error
	ran int
}

func (r *fakeRunner) Run(_ context.Context, job sandbox.Job) (sandbox.Result, error) {
	r.ran++
	_, _ = io.ReadAll(job.Input)
	return r.res, r.err
}

func (r *fakeRunner) Sweep(context.Context, time.Duration) (int, error) { return 0, nil }

func pdfFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("%PDF-1.5")
	return f
}

func job() apiclient.Job {
	return apiclient.Job{ID: "j1", ProjectID: "p1", MainFilePath: "main.tex", Engine: "pdflatex"}
}

func TestHandle(t *testing.T) {
	code := 0
	tests := []struct {
		name        string
		api         *fakeAPI
		res         sandbox.Result
		runErr      error
		pdf         bool
		wantOutcome string
		wantFailure string
		wantPDFs    int
		wantRun     bool
	}{
		{"success", &fakeAPI{}, sandbox.Result{ExitCode: &code, Log: "log"}, nil, true, "completed", "", 1, true},
		{"errors without a PDF", &fakeAPI{}, sandbox.Result{Log: "! Emergency stop."}, nil, false, "completed", "", 0, true},
		{"timeout", &fakeAPI{}, sandbox.Result{TimedOut: true, Log: "loop"}, nil, true, "timed_out", "", 0, true},
		{"out of memory", &fakeAPI{}, sandbox.Result{Failure: sandbox.FailOutOfMemory}, nil, false, "failed", "out_of_memory", 0, true},
		{"sandbox did not run", &fakeAPI{}, sandbox.Result{}, errors.New("no daemon"), false, "failed", "sandbox_error", 0, true},
		{"PDF refused as too large", &fakeAPI{pdfErr: apiclient.ErrPDFTooLarge}, sandbox.Result{}, nil, true, "failed", "output_too_large", 0, true},
		{"files could not be read", &fakeAPI{sourceErr: errors.New("connection refused")}, sandbox.Result{}, nil, false, "failed", "sandbox_error", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := tt.res
			if tt.pdf {
				res.PDF, res.PDFSize = pdfFile(t), 8
			}
			r := &fakeRunner{res: res, err: tt.runErr}
			New(tt.api, r, "w", 1, nil).Handle(t.Context(), job())
			if len(tt.api.finished) != 1 {
				t.Fatalf("finished %d times", len(tt.api.finished))
			}
			got := tt.api.finished[0]
			failure := ""
			if got.Failure != nil {
				failure = *got.Failure
			}
			if got.Outcome != tt.wantOutcome || failure != tt.wantFailure || len(tt.api.pdfs) != tt.wantPDFs || (r.ran > 0) != tt.wantRun {
				t.Errorf("outcome %s failure %q pdfs %d ran %d", got.Outcome, failure, len(tt.api.pdfs), r.ran)
			}
			if res.PDF != nil {
				if _, err := os.Stat(res.PDF.Name()); !os.IsNotExist(err) {
					t.Error("the PDF's temporary file was left behind")
				}
			}
		})
	}
}

// A job the API already ended (input too large, lease lost) is not reported again.
func TestHandleEndedJobs(t *testing.T) {
	for _, err := range []error{apiclient.ErrInputTooLarge, apiclient.ErrGone} {
		api := &fakeAPI{sourceErr: err}
		r := &fakeRunner{}
		New(api, r, "w", 1, nil).Handle(t.Context(), job())
		if len(api.finished) != 0 || r.ran != 0 {
			t.Errorf("%v: finished %d, ran %d", err, len(api.finished), r.ran)
		}
	}
	api := &fakeAPI{}
	r := &fakeRunner{}
	New(api, r, "w", 1, nil).Handle(t.Context(), apiclient.Job{ID: "j", MainFilePath: "../x.tex"})
	if r.ran != 0 || len(api.finished) != 1 || *api.finished[0].Failure != "sandbox_error" {
		t.Errorf("bad main file: ran %d, finished %v", r.ran, api.finished)
	}
}

// NFR-4, fail closed: a sandbox that cannot enforce its file rules fails the job and stops
// the worker, which returns sandbox.ErrUnavailable (main exits; the API answers
// compile_unavailable once no worker polls).
func TestUnavailableSandboxStopsTheWorker(t *testing.T) {
	code := 78
	api := &claimingAPI{}
	r := &fakeRunner{res: sandbox.Result{Unavailable: true, Failure: sandbox.FailSandbox, ExitCode: &code}}
	done := make(chan error, 1)
	go func() { done <- New(api, r, "w", 2, nil).Run(t.Context()) }()
	select {
	case err := <-done:
		if !errors.Is(err, sandbox.ErrUnavailable) {
			t.Errorf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the worker kept running")
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.finished) == 0 || api.finished[0].Outcome != "failed" || *api.finished[0].Failure != "sandbox_error" {
		t.Errorf("finished %+v", api.finished)
	}
}

// A worker stopped normally returns nil.
func TestRunStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := New(&fakeAPI{}, &fakeRunner{}, "w", 1, nil).Run(ctx); err != nil {
		t.Errorf("Run = %v", err)
	}
}

// claimingAPI hands out a job on every claim.
type claimingAPI struct{ fakeAPI }

func (c *claimingAPI) Claim(context.Context, string) (*apiclient.Job, error) {
	j := job()
	return &j, nil
}
