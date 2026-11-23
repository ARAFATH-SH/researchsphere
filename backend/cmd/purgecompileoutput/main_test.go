package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	compiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
	mediadomain "github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

type fakePurger struct {
	jobs     []compiledomain.Output
	err      error
	drain    mediadomain.DrainResult
	drainErr error
	before   time.Time
	applied  bool
	drained  bool
}

func (f *fakePurger) PurgeOutputBefore(_ context.Context, before time.Time, apply bool) ([]compiledomain.Output, error) {
	f.before, f.applied = before, apply
	return f.jobs, f.err
}

func (f *fakePurger) DeleteQueuedObjects(context.Context) (mediadomain.DrainResult, error) {
	f.drained = true
	return f.drain, f.drainErr
}

func TestRun(t *testing.T) {
	finished := time.Date(2026, 11, 23, 13, 51, 20, 0, time.UTC)
	two := []compiledomain.Output{
		{JobID: "j1", ProjectID: "p1", FinishedAt: &finished, HasPDF: true},
		{JobID: "j2", ProjectID: "p2", FinishedAt: &finished, HasPDF: true, HasLog: true},
	}
	boom := errors.New("boom")
	tests := []struct {
		name      string
		args      []string
		p         fakePurger
		want      []string
		wantErr   string
		wantApply bool
		drained   bool
	}{
		{"dry run lists and deletes nothing", []string{"-before", "2026-11-23T21:10:27+06:00"}, fakePurger{jobs: two},
			[]string{"compile j1  project p1  finished 2026-11-23T13:51:20Z  [PDF]", "compile j2", "[PDF log]", "Dry run: 2 compiles before 2026-11-23T21:10:27+06:00 hold 2 PDFs and 1 logs. Add -apply"}, "", false, false},
		{"apply clears and deletes the objects", []string{"-before", "2026-11-23T21:10:27+06:00", "-apply"}, fakePurger{jobs: two, drain: mediadomain.DrainResult{Claimed: 2, Deleted: 2}},
			[]string{"Deleted 2 PDFs and 1 logs of 2 compiles", "Deleted 2 objects from storage."}, "", true, true},
		{"objects left for the API", []string{"-before", "2026-11-23T21:10:27+06:00", "-apply"}, fakePurger{jobs: two, drain: mediadomain.DrainResult{Deleted: 1, Failed: 1}},
			[]string{"1 could not be deleted yet"}, "", true, true},
		{"storage fails", []string{"-before", "2026-11-23T21:10:27+06:00", "-apply"}, fakePurger{jobs: two, drainErr: boom},
			nil, "the database is cleared, but deleting the objects failed (the API's cleanup retries them): boom", true, true},
		{"no cutoff", nil, fakePurger{}, nil, "-before is required: the time the fixed sandbox went live, such as 2026-11-23T21:10:27+06:00", false, false},
		{"no zone", []string{"-before", "2026-11-23 21:10:27"}, fakePurger{}, nil, `-before "2026-11-23 21:10:27" is not an RFC 3339 time with a zone`, false, false},
		{"refused", []string{"-before", "2026-11-23T21:10:27+06:00"}, fakePurger{err: boom}, nil, "boom", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.p
			closed := false
			open := func(context.Context) (purger, func(), error) { return &p, func() { closed = true }, nil }
			var out bytes.Buffer
			err := run(t.Context(), tt.args, open, &out)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(out.String(), w) {
					t.Errorf("output = %q, want %q", out.String(), w)
				}
			}
			if p.applied != tt.wantApply || p.drained != tt.drained {
				t.Errorf("applied %v drained %v", p.applied, p.drained)
			}
			if len(tt.args) > 1 && tt.name != "no zone" {
				if !closed {
					t.Error("not closed")
				}
				if want := time.Date(2026, 11, 23, 15, 10, 27, 0, time.UTC); !p.before.Equal(want) {
					t.Errorf("before = %v", p.before)
				}
			}
		})
	}
}
