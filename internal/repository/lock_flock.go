//go:build linux || darwin

package repository

import (
	"os"
	"syscall"
)

// lockDirExclusive acquires a blocking exclusive flock on the given lock file.
// It serializes directory mutations across processes and FileStore instances.
func lockDirExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// unlockDirExclusive releases a previously acquired exclusive flock.
func unlockDirExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
