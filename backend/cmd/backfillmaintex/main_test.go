package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeBackfill struct {
	n   int
	err error
}

func (f fakeBackfill) BackfillMainFiles(context.Context) (int, error) { return f.n, f.err }

func TestRun(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		b       fakeBackfill
		openErr error
		want    string
		wantErr string
	}{
		{"adds", fakeBackfill{n: 3}, nil, "Added main.tex to 3 projects without files.", ""},
		{"nothing to do", fakeBackfill{}, nil, "Added main.tex to 0 projects", ""},
		{"fails part way", fakeBackfill{n: 2, err: boom}, nil, "", "after adding main.tex to 2 projects: boom"},
		{"no database", fakeBackfill{}, boom, "", "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			closed := false
			open := func(context.Context) (backfiller, func(), error) {
				if tt.openErr != nil {
					return nil, nil, tt.openErr
				}
				return tt.b, func() { closed = true }, nil
			}
			var out bytes.Buffer
			err := run(t.Context(), open, &out)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tt.want) {
				t.Errorf("output = %q, want %q", out.String(), tt.want)
			}
			if tt.openErr == nil && !closed {
				t.Error("the store was not closed")
			}
		})
	}
}
