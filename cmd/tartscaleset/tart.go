package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func runTart(ctx context.Context, cfg config, args ...string) error {
	command := exec.CommandContext(ctx, cfg.TartBin, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("tart %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func tartVMExists(ctx context.Context, cfg config, name string) (bool, error) {
	command := exec.CommandContext(ctx, cfg.TartBin, "list", "--source", "local", "--quiet")
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
		command := exec.CommandContext(probeCtx, cfg.TartBin, "exec", name, "/usr/bin/true")
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
