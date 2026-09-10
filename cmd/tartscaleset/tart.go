package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const macOSSystemPath = "/usr/bin:/bin:/usr/sbin:/sbin"

func runTart(ctx context.Context, cfg config, args ...string) error {
	command := newTartCommand(ctx, cfg, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("tart %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func tartVMExists(ctx context.Context, cfg config, name string) (bool, error) {
	command := newTartCommand(ctx, cfg, "list", "--source", "local", "--quiet")
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("list Tart VMs: %w", err)
	}
	for vmName := range strings.Lines(string(output)) {
		if strings.TrimSpace(vmName) == name {
			return true, nil
		}
	}
	return false, nil
}

func waitForTartGuest(ctx context.Context, cfg config, name string) error {
	deadline := time.NewTimer(cfg.vmReadyTimeout())
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		command := newTartCommand(probeCtx, cfg, "exec", name, "/usr/bin/true")
		err := command.Run()
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("VM %s did not become ready within %s", name, cfg.vmReadyTimeout())
		case <-ticker.C:
		}
	}
}

func newTartCommand(ctx context.Context, cfg config, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, cfg.TartBin, args...)
	path := macOSSystemPath
	overrides := map[string]string{"PATH": path}
	if cfg.SoftnetBin != "" {
		path = filepath.Dir(cfg.SoftnetBin) + string(os.PathListSeparator) + path
		overrides["PATH"] = path
		overrides["SOFTNET_BIN"] = cfg.SoftnetBin
	}
	command.Env = environmentWithOverrides(overrides)
	return command
}

func environmentWithOverrides(overrides map[string]string) []string {
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := overrides[key]; !replaced {
			environment = append(environment, item)
		}
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func tartRunArguments(cfg config, name string) []string {
	args := []string{"run", "--no-graphics"}
	switch cfg.TartNetworkMode {
	case "softnet":
		args = append(args, "--net-softnet")
		if cfg.TartSoftnetAllow != "" {
			args = append(args, "--net-softnet-allow="+cfg.TartSoftnetAllow)
		}
	case "host":
		args = append(args, "--net-host")
	}
	if cfg.XcodeAppPath != "" {
		args = append(args, "--dir", cfg.XcodeAppPath+":ro,tag=xcode")
	}
	return append(args, name)
}
