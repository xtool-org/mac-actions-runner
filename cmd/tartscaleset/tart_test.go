package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTartCommandUsesDeterministicPath(t *testing.T) {
	t.Setenv("PATH", "/unexpected/shell/path")
	cfg := config{
		TartBin:    "/tools/tart",
		SoftnetBin: "/state/tools/softnet/softnet",
	}

	command := newTartCommand(context.Background(), cfg, "list")
	want := filepath.Dir(cfg.SoftnetBin) + string(os.PathListSeparator) + macOSSystemPath
	if got := environmentValue(command.Env, "PATH"); got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
	if got := environmentValue(command.Env, "SOFTNET_BIN"); got != cfg.SoftnetBin {
		t.Fatalf("SOFTNET_BIN = %q, want %q", got, cfg.SoftnetBin)
	}
}

func TestNewTartCommandUsesSystemPathWithoutSoftnet(t *testing.T) {
	t.Setenv("PATH", "/unexpected/shell/path")
	command := newTartCommand(context.Background(), config{TartBin: "/tools/tart"}, "list")

	if got := environmentValue(command.Env, "PATH"); got != macOSSystemPath {
		t.Fatalf("PATH = %q, want %q", got, macOSSystemPath)
	}
}

func environmentValue(environment []string, name string) string {
	prefix := name + "="
	for _, item := range environment {
		if value, ok := strings.CutPrefix(item, prefix); ok {
			return value
		}
	}
	return ""
}
