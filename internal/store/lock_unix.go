//go:build !windows && !js

package store

import (
	"os"
	"syscall"
)

// lock takes an exclusive advisory lock on a sentinel file, serialising mutations
// across processes.
//
// This is the load-bearing primitive of the whole store. Two agents in separate
// terminals will call Claim concurrently; without this both read "free" and both
// write "held", which is exactly the silent-overwrite bug Tearoom exists to
// prevent. flock is used rather than a lockfile-with-PID because it is released
// by the kernel when the process dies — an agent that crashes mid-claim must not
// wedge the workspace forever.
func (f *File) lock() (func(), error) {
	fh, err := os.OpenFile(f.path("lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(fh.Fd()), syscall.LOCK_EX); err != nil {
		fh.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(fh.Fd()), syscall.LOCK_UN)
		fh.Close()
	}, nil
}
