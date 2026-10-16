package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

type fakeSetter struct {
	err         error
	email, role string
	calls       int
}

func (f *fakeSetter) SetRoleByEmail(_ context.Context, email, role string) error {
	f.calls++
	f.email, f.role = email, role
	return f.err
}

func TestRun(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		setErr    error
		wantErr   string // part of the error; empty means success
		wantOut   string
		wantCalls int
	}{
		{name: "make admin", args: []string{"-email", "ada@example.org", "-role", "admin"}, wantOut: "ada@example.org now has the admin role", wantCalls: 1},
		{name: "make unadmin", args: []string{"-email", "ada@example.org", "-role", "researcher"}, wantOut: "ada@example.org now has the researcher role", wantCalls: 1},
		{name: "unknown email", args: []string{"-email", "nobody@example.org", "-role", "admin"}, setErr: domain.ErrNotFound, wantErr: "no account with the email address nobody@example.org", wantCalls: 1},
		{name: "database error", args: []string{"-email", "ada@example.org", "-role", "admin"}, setErr: errors.New("connection refused"), wantErr: "connection refused", wantCalls: 1},
		{name: "no email", args: []string{"-role", "admin"}, wantErr: "usage: make admin email="},
		{name: "blank email", args: []string{"-email", "  ", "-role", "admin"}, wantErr: "usage: make admin email="},
		{name: "role named user", args: []string{"-email", "ada@example.org", "-role", "user"}, wantErr: `role must be "admin" or "researcher"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSetter{err: tt.setErr}
			open := func(context.Context) (roleSetter, func(), error) { return f, func() {}, nil }
			var out bytes.Buffer
			err := run(t.Context(), tt.args, open, &out)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("run() error = %v", err)
				}
				if !strings.Contains(out.String(), tt.wantOut) {
					t.Errorf("output = %q, want it to contain %q", out.String(), tt.wantOut)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("run() error = %v, want it to contain %q", err, tt.wantErr)
			}
			if f.calls != tt.wantCalls {
				t.Errorf("SetRoleByEmail called %d times, want %d", f.calls, tt.wantCalls)
			}
		})
	}
}
