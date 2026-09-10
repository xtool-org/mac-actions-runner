package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTrimStrings(t *testing.T) {
	got := trimStrings([]string{"xtool-runner", " ubuntu-on-macos ", "", "ARM64"})
	want := []string{"xtool-runner", "ubuntu-on-macos", "ARM64"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trimStrings() = %#v, want %#v", got, want)
	}
}

func TestLoadConfigFromEnvironment(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	tart := filepath.Join(directory, "tart")
	if err := os.WriteFile(tart, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TART_BIN", tart)
	t.Setenv("XCODE_APP_PATH", "none")
	t.Setenv("APP_PRIVATE_KEY_FILE", "key.pem")
	t.Setenv("RUNNER_STATE_DIR", ".state")
	t.Setenv("TART_NETWORK_MODE", "shared")
	t.Setenv("RUNNER_MIN_COUNT", "2")
	t.Setenv("RUNNER_MAX_COUNT", "4")

	cfg, err := loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinRunners != 2 || cfg.MaxRunners != 4 {
		t.Fatalf("runner counts = %d..%d, want 2..4", cfg.MinRunners, cfg.MaxRunners)
	}
	if cfg.XcodeAppPath != "" {
		t.Fatalf("xcodeAppPath = %q, want disabled", cfg.XcodeAppPath)
	}
	if cfg.vmReadyTimeout() != 180*time.Second {
		t.Fatalf("vmReadyTimeout = %s, want 180s", cfg.vmReadyTimeout())
	}
	if cfg.AppPrivateKeyFile != filepath.Join(directory, "key.pem") {
		t.Fatalf("appPrivateKeyFile = %q", cfg.AppPrivateKeyFile)
	}
}

func TestResolveStateDirExpandsHome(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cfg := config{StateDir: "~/.config/tartscaleset"}

	if err := cfg.resolveStateDir(); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(homeDir, ".config", "tartscaleset")
	if cfg.StateDir != want {
		t.Fatalf("stateDir = %q, want %q", cfg.StateDir, want)
	}
}

func TestResolveRuntimePathsUsesStateDirForAutoPrivateKey(t *testing.T) {
	stateDir := t.TempDir()
	cfg := config{
		AppPrivateKeyFile: "auto",
		StateDir:          stateDir,
		XcodeAppPath:      "none",
	}

	if err := cfg.resolveRuntimePaths(); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stateDir, "private-key.pem")
	if cfg.AppPrivateKeyFile != want {
		t.Fatalf("appPrivateKeyFile = %q, want %q", cfg.AppPrivateKeyFile, want)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("abc'def")
	want := `'abc'\''def'`
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}
