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
	t.Setenv("HOME", directory)
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

func TestConfigFileAndProcessEnvironmentPrecedence(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configDir := filepath.Join(homeDir, ".config", "tartscaleset")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := "APP_ID=from-file\nRUNNER_MAX_COUNT=2\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.env"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ID", "from-process")
	unsetEnvForTest(t, "RUNNER_MAX_COUNT")

	cfg, err := parseConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppID != "from-process" || cfg.MaxRunners != 2 {
		t.Fatalf("configuration precedence = APP_ID %q, RUNNER_MAX_COUNT %d", cfg.AppID, cfg.MaxRunners)
	}
}

func TestLoadConfigFromConfigFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configDir := filepath.Join(homeDir, ".config", "tartscaleset")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tart := filepath.Join(configDir, "tart")
	if err := os.WriteFile(tart, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TART_NETWORK_MODE", "RUNNER_STATE_DIR", "APP_PRIVATE_KEY_FILE", "TART_BIN", "XCODE_APP_PATH"} {
		unsetEnvForTest(t, name)
	}
	stateDir := filepath.Join(configDir, "state")
	key := filepath.Join(configDir, "key.pem")
	contents := "TART_NETWORK_MODE=shared\nRUNNER_STATE_DIR=" + stateDir + "\nAPP_PRIVATE_KEY_FILE=" + key + "\nTART_BIN=" + tart + "\nXCODE_APP_PATH=none\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.env"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != stateDir || cfg.AppPrivateKeyFile != key || cfg.TartBin != tart || cfg.XcodeAppPath != "" {
		t.Fatalf("config file values not applied: %+v", cfg)
	}
}

func TestConfigFileIsOptionalButMalformedFileFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configPath, err := configFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseConfig(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("APP_ID=\"unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseConfig(); err == nil {
		t.Fatal("malformed config file was accepted")
	}
}

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("abc'def")
	want := `'abc'\''def'`
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}
