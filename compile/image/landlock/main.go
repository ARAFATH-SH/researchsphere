//go:build linux

// rs-landlock is the compile sandbox's file wall (docs/compile.md, NFR-4). It runs first in
// every compile container, restricts itself with Landlock and then executes the command it
// is given, which inherits the restriction with all its children: TeX, latexmk, BibTeX, the
// shell and tar can then read only the TeX Live tree, the runtime's programs and libraries,
// /work and /tmp, and write only /work and /tmp. TeX Live 2026 made openin_any a no-op, so no
// TeX setting can do this (docs/decisions.md).
//
// It fails closed: when the kernel lacks Landlock ABI minABI, or any step fails, it prints
// "rs-landlock: sandbox unavailable: ..." and exits 78 before anything else runs.
//
// Usage: rs-landlock -- COMMAND [ARG...]
//
//	rs-landlock --probe   restrict, check that /etc/passwd cannot be opened and /work can be
//	                      written, print "landlock abi=N ok"
//
// Standard library only; it is built into the image (compile/image/Dockerfile).
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// minABI is the Landlock ABI the sandbox requires (docs/deploy.md names the kernel).
const minABI = 8

// ExitUnavailable is the exit status of a sandbox that cannot enforce its rules; the worker
// treats it as fatal (compile/internal/sandbox).
const ExitUnavailable = 78

// Marker starts every refusal message; the worker looks for it.
const Marker = "rs-landlock: sandbox unavailable: "

// The Landlock system calls have the same numbers on every architecture.
const (
	sysCreateRuleset = 444
	sysAddRule       = 445
	sysRestrictSelf  = 446

	createRulesetVersion = 1 << 0
	rulePathBeneath      = 1
	prSetNoNewPrivs      = 38
	oPath                = 0x200000 // O_PATH (syscall does not name it)
)

// Filesystem access rights (include/uapi/linux/landlock.h).
const (
	fsExecute    uint64 = 1 << 0
	fsWriteFile  uint64 = 1 << 1
	fsReadFile   uint64 = 1 << 2
	fsReadDir    uint64 = 1 << 3
	fsRemoveDir  uint64 = 1 << 4
	fsRemoveFile uint64 = 1 << 5
	fsMakeChar   uint64 = 1 << 6
	fsMakeDir    uint64 = 1 << 7
	fsMakeReg    uint64 = 1 << 8
	fsMakeSock   uint64 = 1 << 9
	fsMakeFifo   uint64 = 1 << 10
	fsMakeBlock  uint64 = 1 << 11
	fsMakeSym    uint64 = 1 << 12
	fsRefer      uint64 = 1 << 13 // ABI 2
	fsTruncate   uint64 = 1 << 14 // ABI 3
	fsIoctlDev   uint64 = 1 << 15 // ABI 5

	netBindTCP    uint64 = 1 << 0 // ABI 4
	netConnectTCP uint64 = 1 << 1 // ABI 4

	scopeAbstractUnixSocket uint64 = 1 << 0 // ABI 6
	scopeSignal             uint64 = 1 << 1 // ABI 6

	// fsAll is every filesystem right the sandbox handles: anything not allowed below is
	// refused.
	fsAll = fsExecute | fsWriteFile | fsReadFile | fsReadDir | fsRemoveDir | fsRemoveFile |
		fsMakeChar | fsMakeDir | fsMakeReg | fsMakeSock | fsMakeFifo | fsMakeBlock | fsMakeSym |
		fsRefer | fsTruncate | fsIoctlDev
	// fileRights are the rights Landlock accepts on a rule for a single file.
	fileRights = fsExecute | fsWriteFile | fsReadFile | fsTruncate | fsIoctlDev

	readExec = fsExecute | fsReadFile | fsReadDir
	// scratch is a writable folder: everything but devices and executing. Nothing is ever
	// executed from /work or /tmp (both are noexec mounts too); biber runs from its copy
	// unpacked into the image (RS_BIBER_CACHE).
	scratch = fsAll &^ (fsMakeChar | fsMakeBlock | fsExecute)
)

// Rule allows access below one path.
type Rule struct {
	Path   string
	Access uint64
}

// Rules is the whole allow-list. Every path must exist in the image: a missing one is a
// broken image, and the sandbox refuses to run.
var Rules = []Rule{
	{"/usr/bin", readExec},
	{"/usr/lib", readExec},
	{"/usr/lib64", readExec},
	{"/usr/share/perl", readExec},
	{"/usr/share/perl5", readExec},
	{"/usr/local/bin", readExec},
	{"/usr/local/texlive", readExec},        // the whole TeX Live tree, texmf-var and texmf-config included
	{"/usr/local/share/rs-biber", readExec}, // biber unpacked at build time (PAR_GLOBAL_TEMP)
	{"/etc/ld.so.cache", fsReadFile},
	{"/etc/rs-compile", fsReadFile | fsReadDir}, // the fixed latexmkrc
	{"/dev/null", fsReadFile | fsWriteFile | fsTruncate | fsIoctlDev},
	{"/dev/urandom", fsReadFile}, // Perl (biber, latexmk) seeds from it
	{"/work", scratch},
	{"/tmp", scratch},
}

// rulesetAttr is struct landlock_ruleset_attr (ABI 6 layout).
type rulesetAttr struct {
	handledFS  uint64
	handledNet uint64
	scoped     uint64
}

func abi() (int, error) {
	v, _, e := syscall.Syscall(sysCreateRuleset, 0, 0, createRulesetVersion)
	if e != 0 {
		return 0, fmt.Errorf("landlock is not available (%w); the kernel needs Landlock ABI %d, enabled in its lsm= list", e, minABI)
	}
	return int(v), nil
}

// restrict applies rules to the calling thread; the caller locks it and executes from it.
func restrict(rules []Rule) (int, error) {
	v, err := abi()
	if err != nil {
		return 0, err
	}
	if v < minABI {
		return v, fmt.Errorf("the kernel has Landlock ABI %d, the sandbox needs %d", v, minABI)
	}
	attr := rulesetAttr{
		handledFS:  fsAll,
		handledNet: netBindTCP | netConnectTCP,
		scoped:     scopeAbstractUnixSocket | scopeSignal,
	}
	fd, _, e := syscall.Syscall(sysCreateRuleset, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0) //nolint:gosec // the kernel's struct, passed as the system call requires
	if e != 0 {
		return v, fmt.Errorf("create the ruleset: %w", e)
	}
	ruleset := int(fd)
	defer func() { _ = syscall.Close(ruleset) }()
	for _, r := range rules {
		if err := addRule(ruleset, r); err != nil {
			return v, err
		}
	}
	if _, _, e := syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); e != 0 {
		return v, fmt.Errorf("no_new_privs: %w", e)
	}
	if _, _, e := syscall.Syscall(sysRestrictSelf, uintptr(ruleset), 0, 0); e != 0 {
		return v, fmt.Errorf("restrict: %w", e)
	}
	return v, nil
}

func addRule(ruleset int, r Rule) error {
	fd, err := syscall.Open(r.Path, oPath|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", r.Path, err)
	}
	defer func() { _ = syscall.Close(fd) }()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat %s: %w", r.Path, err)
	}
	access := r.Access
	if st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		access &= fileRights
	}
	attr := pathBeneath(access, fd)
	if _, _, e := syscall.Syscall6(sysAddRule, uintptr(ruleset), rulePathBeneath, uintptr(unsafe.Pointer(&attr[0])), 0, 0, 0); e != 0 { //nolint:gosec // the kernel's struct, passed as the system call requires
		return fmt.Errorf("allow %s: %w", r.Path, e)
	}
	return nil
}

// pathBeneath is struct landlock_path_beneath_attr, which is packed: u64 then s32, 12 bytes.
func pathBeneath(access uint64, fd int) [12]byte {
	var b [12]byte
	binary.NativeEndian.PutUint64(b[:8], access)
	binary.NativeEndian.PutUint32(b[8:], uint32(int32(fd))) //nolint:gosec // a file descriptor fits in the kernel's s32
	return b
}

// probe checks the restriction it is under.
func probe(v int) error {
	if f, err := os.Open("/etc/passwd"); err == nil {
		_ = f.Close()
		return errors.New("/etc/passwd can still be opened")
	} else if !errors.Is(err, syscall.EACCES) {
		return fmt.Errorf("/etc/passwd: want permission denied, got %w", err)
	}
	f, err := os.CreateTemp("/work", ".rs-probe-")
	if err != nil {
		return fmt.Errorf("/work is not writable: %w", err)
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	fmt.Printf("landlock abi=%d ok\n", v)
	return nil
}

// parse returns the command to run, or probe = true.
func parse(args []string) (cmd []string, isProbe bool, err error) {
	switch {
	case len(args) == 1 && args[0] == "--probe":
		return nil, true, nil
	case len(args) >= 2 && args[0] == "--":
		return args[1:], false, nil
	}
	return nil, false, errors.New("usage: rs-landlock -- COMMAND [ARG...] | --probe")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, Marker+err.Error())
	os.Exit(ExitUnavailable)
}

func main() {
	cmd, isProbe, err := parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64)
	}
	// Landlock restricts the calling thread: stay on it until exec replaces the process.
	runtime.LockOSThread()
	v, err := restrict(Rules)
	if err != nil {
		fail(err)
	}
	if isProbe {
		if err := probe(v); err != nil {
			fail(err)
		}
		return
	}
	// The command is the image's own entrypoint (compile/image/Dockerfile), never a user's.
	if err := syscall.Exec(cmd[0], cmd, os.Environ()); err != nil { //nolint:gosec // see above
		fail(fmt.Errorf("exec %s: %w", cmd[0], err))
	}
}
