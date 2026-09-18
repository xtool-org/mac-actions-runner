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

func TestResolveRuntimePathsMakesXcodePathAbsolute(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	cfg := config{StateDir: directory, AppPrivateKeyFile: "auto", XcodeAppPath: "Xcode.app"}

	if err := cfg.resolveRuntimePaths(); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(directory, "Xcode.app"); cfg.XcodeAppPath != want {
		t.Fatalf("xcodeAppPath = %q, want %q", cfg.XcodeAppPath, want)
	}
}

func TestLaunchEnvironmentExcludesUnrecognizedVariables(t *testing.T) {
	t.Setenv("APP_ID", "1234")
	t.Setenv("PATH", "/unexpected/shell/path")
	t.Setenv("UNRELATED_SECRET", "do-not-copy")

	environment := (config{StateDir: "/state"}).launchEnvironment()
	if environment["APP_ID"] != "1234" {
		t.Fatalf("APP_ID = %q, want 1234", environment["APP_ID"])
	}
	if _, ok := environment["UNRELATED_SECRET"]; ok {
		t.Fatal("unrecognized environment variable was captured")
	}
	if _, ok := environment["PATH"]; ok {
		t.Fatal("shell PATH was captured")
	}
}

func TestLaunchEnvironmentMakesPathsAbsolute(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.MkdirAll(filepath.Join(directory, "tools"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "tools", "tart"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "Xcode.app", "Contents", "Developer"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNNER_STATE_DIR", "./state")
	t.Setenv("APP_PRIVATE_KEY_FILE", "./key.pem")
	t.Setenv("TART_BIN", "./tools/tart")
	t.Setenv("SOFTNET_BIN", "./tools/softnet")
	t.Setenv("XCODE_APP_PATH", "./Xcode.app")
	t.Setenv("TART_NETWORK_MODE", "shared")

	cfg, err := loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	environment := cfg.launchEnvironment()
	for name, want := range map[string]string{
		"RUNNER_STATE_DIR":     filepath.Join(directory, "state"),
		"APP_PRIVATE_KEY_FILE": filepath.Join(directory, "key.pem"),
		"TART_BIN":             filepath.Join(directory, "tools", "tart"),
		"SOFTNET_BIN":          filepath.Join(directory, "tools", "softnet"),
		"XCODE_APP_PATH":       filepath.Join(directory, "Xcode.app"),
	} {
		if got := environment[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestLaunchEnvironmentPreservesAutomaticPaths(t *testing.T) {
	t.Setenv("APP_PRIVATE_KEY_FILE", "auto")
	t.Setenv("TART_BIN", "auto")
	t.Setenv("SOFTNET_BIN", "auto")
	t.Setenv("XCODE_APP_PATH", "none")

	environment := (config{}).launchEnvironment()
	for name, want := range map[string]string{
		"APP_PRIVATE_KEY_FILE": "auto",
		"TART_BIN":             "auto",
		"SOFTNET_BIN":          "auto",
		"XCODE_APP_PATH":       "none",
	} {
		if got := environment[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	t.Setenv("SOFTNET_BIN", "")
	t.Setenv("XCODE_APP_PATH", "")
	environment = (config{}).launchEnvironment()
	if environment["SOFTNET_BIN"] != "" || environment["XCODE_APP_PATH"] != "" {
		t.Fatal("empty optional paths should remain empty")
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("abc'def")
	want := `'abc'\''def'`
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}
