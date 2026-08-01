//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockFederationGenerationEpoch(path string) (func() error, error) {
	// The file is only a stable rendezvous path. Lock ownership lives in the
	// kernel and is released automatically if the process exits.
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		_ = lockFile.Close()
		return nil, err
	}
	return func() error {
		unlockErr := unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		return errors.Join(unlockErr, lockFile.Close())
	}, nil
}

func replaceFederationGenerationEpoch(tmpPath, path, parentDir string) error {
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	dir, err := os.Open(parentDir)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}
