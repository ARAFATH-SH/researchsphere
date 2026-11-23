//go:build linux

package main

import (
	"encoding/binary"
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		args    []string
		cmd     []string
		probe   bool
		wantErr bool
	}{
		{[]string{"--probe"}, nil, true, false},
		{[]string{"--", "/usr/local/bin/rs-compile", "main.tex"}, []string{"/usr/local/bin/rs-compile", "main.tex"}, false, false},
		{nil, nil, false, true},
		{[]string{"--"}, nil, false, true},
		{[]string{"/bin/sh"}, nil, false, true},
		{[]string{"--probe", "x"}, nil, false, true},
	}
	for _, tt := range tests {
		cmd, probe, err := parse(tt.args)
		if (err != nil) != tt.wantErr || probe != tt.probe || !slices.Equal(cmd, tt.cmd) {
			t.Errorf("parse(%q) = %q, %v, %v", tt.args, cmd, probe, err)
		}
	}
}

func TestPathBeneathLayout(t *testing.T) {
	b := pathBeneath(fsReadFile|fsReadDir, 7)
	if got := binary.NativeEndian.Uint64(b[:8]); got != fsReadFile|fsReadDir {
		t.Errorf("access = %#x", got)
	}
	if got := int32(binary.NativeEndian.Uint32(b[8:])); got != 7 { //nolint:gosec // test value
		t.Errorf("fd = %d", got)
	}
}

// The allow-list is the security boundary: nothing under /etc, /proc, /home or /root, no
// write outside /work and /tmp, and the scratch folders cannot make devices.
func TestRules(t *testing.T) {
	writes := fsWriteFile | fsRemoveDir | fsRemoveFile | fsMakeChar | fsMakeDir | fsMakeReg |
		fsMakeSock | fsMakeFifo | fsMakeBlock | fsMakeSym | fsRefer | fsTruncate
	for _, r := range Rules {
		for _, bad := range []string{"/", "/etc", "/proc", "/sys", "/home", "/root", "/usr", "/usr/share", "/var"} {
			if r.Path == bad {
				t.Errorf("rule allows all of %s", bad)
			}
		}
		if r.Path != "/work" && r.Path != "/tmp" && r.Path != "/dev/null" && r.Access&writes != 0 {
			t.Errorf("%s may be written", r.Path)
		}
		if r.Access&writes != 0 && r.Access&fsExecute != 0 {
			t.Errorf("%s may be written and executed", r.Path)
		}
		if r.Access&(fsMakeChar|fsMakeBlock) != 0 {
			t.Errorf("%s may make devices", r.Path)
		}
		if r.Access&^fsAll != 0 {
			t.Errorf("%s has an unknown right", r.Path)
		}
	}
	if minABI < 6 {
		t.Error("the scopes need ABI 6")
	}
}
