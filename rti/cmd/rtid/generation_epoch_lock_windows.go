//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockFederationGenerationEpoch(path string) (func() error, error) {
	// The file is only a stable rendezvous path. Lock ownership lives in the
	// kernel and is released automatically if the process exits.
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(
		windows.Handle(lockFile.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		1,
		0,
		overlapped,
	); err != nil {
		_ = lockFile.Close()
		return nil, err
	}
	return func() error {
		unlockErr := windows.UnlockFileEx(
			windows.Handle(lockFile.Fd()),
			0,
			1,
			0,
			overlapped,
		)
		return errors.Join(unlockErr, lockFile.Close())
	}, nil
}

func replaceFederationGenerationEpoch(tmpPath, path, _ string) error {
	from, err := windows.UTF16PtrFromString(tmpPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		from,
		to,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
