//go:build !linux && !darwin

package repository

import "os"

func lockDirExclusive(f *os.File) error   { return nil }
func unlockDirExclusive(f *os.File) error { return nil }
