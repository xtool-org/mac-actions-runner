package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type processLock struct {
	file *os.File
}

func acquireControllerLock(cfg config) (*processLock, error) {
	controllersDir := filepath.Join(cfg.StateDir, "controllers")
	if err := os.MkdirAll(controllersDir, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	path := filepath.Join(controllersDir, "controller-"+stateKey(cfg.ScaleSetName)+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open controller lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another controller is already running for scale set %s", cfg.ScaleSetName)
		}
		return nil, fmt.Errorf("lock controller state: %w", err)
	}
	return &processLock{file: file}, nil
}

func (l *processLock) Close() error {
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	return errors.Join(unlockErr, closeErr)
}

func stateKey(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}
