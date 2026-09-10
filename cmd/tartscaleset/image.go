package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	resources "github.com/xtool-org/xtool-runner"
)

const baseImageVersion = "1"

func prepareBaseImage(ctx context.Context, cfg config, logger *slog.Logger) (returnedErr error) {
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	versionFile := filepath.Join(cfg.StateDir, cfg.TartBaseVM+".image-version")
	installedVersion, err := readImageVersion(versionFile)
	if err != nil {
		return err
	}
	baseExists, err := tartVMExists(ctx, cfg, cfg.TartBaseVM)
	if err != nil {
		return err
	}
	if baseExists && installedVersion == baseImageVersion {
		logger.Info("Base VM is current", "name", cfg.TartBaseVM, "imageVersion", baseImageVersion)
		return nil
	}

	if baseExists {
		if installedVersion == "" {
			logger.Info("Base VM has no image version; rebuilding", "name", cfg.TartBaseVM, "imageVersion", baseImageVersion)
		} else {
			logger.Info("Base VM is outdated; rebuilding", "name", cfg.TartBaseVM, "installedVersion", installedVersion, "imageVersion", baseImageVersion)
		}
	} else {
		logger.Info("Base VM does not exist; building", "name", cfg.TartBaseVM, "imageVersion", baseImageVersion)
	}

	stagingVM := fmt.Sprintf("%s-preparing-%d", cfg.TartBaseVM, os.Getpid())
	var vmCommand *exec.Cmd
	var vmDone <-chan error
	published := false
	defer func() {
		if published {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if vmCommand != nil {
			_ = runTart(cleanupCtx, cfg, "stop", stagingVM, "--timeout", "10")
			waitForVMProcess(vmCommand, vmDone, 15*time.Second)
		}
		exists, err := tartVMExists(cleanupCtx, cfg, stagingVM)
		if err == nil && exists {
			_ = runTart(cleanupCtx, cfg, "delete", stagingVM)
		}
	}()

	logger.Info("Cloning source image", "source", cfg.TartSourceImage, "name", stagingVM)
	if err := runTart(ctx, cfg, "clone", cfg.TartSourceImage, stagingVM); err != nil {
		return err
	}
	if err := runTart(
		ctx,
		cfg,
		"set",
		stagingVM,
		"--cpu", strconv.Itoa(cfg.TartCPU),
		"--memory", strconv.Itoa(cfg.TartMemoryMB),
		"--disk-size", strconv.Itoa(cfg.TartDiskGB),
	); err != nil {
		return err
	}

	logger.Info("Booting base VM for provisioning", "name", stagingVM)
	vmCommand = newTartCommand(context.Background(), cfg, tartRunArguments(cfg, stagingVM)...)
	vmCommand.Stdout = os.Stdout
	vmCommand.Stderr = os.Stderr
	if err := vmCommand.Start(); err != nil {
		return fmt.Errorf("start Tart base VM: %w", err)
	}
	vmDone = waitForCommand(vmCommand)
	if err := waitForTartGuest(ctx, cfg, stagingVM); err != nil {
		return err
	}

	logger.Info("Installing GitHub runner, Docker, and Docker Compose", "name", stagingVM)
	provisionCommand := newTartCommand(ctx, cfg, "exec", "-i", stagingVM, "/bin/bash", "-s")
	provisionCommand.Stdin = strings.NewReader(resources.ProvisionGuestScript)
	provisionCommand.Stdout = os.Stdout
	provisionCommand.Stderr = os.Stderr
	if err := provisionCommand.Run(); err != nil {
		return fmt.Errorf("provision Tart base VM: %w", err)
	}
	if err := runTart(ctx, cfg, "exec", stagingVM, "/bin/sync"); err != nil {
		return err
	}
	if err := runTart(ctx, cfg, "stop", stagingVM, "--timeout", "30"); err != nil {
		return err
	}
	waitForVMProcess(vmCommand, vmDone, 15*time.Second)
	vmCommand = nil
	vmDone = nil

	if baseExists {
		if err := runTart(ctx, cfg, "delete", cfg.TartBaseVM); err != nil {
			return err
		}
	}
	if err := runTart(ctx, cfg, "rename", stagingVM, cfg.TartBaseVM); err != nil {
		return err
	}
	if err := os.WriteFile(versionFile, []byte(baseImageVersion+"\n"), 0o600); err != nil {
		return fmt.Errorf("record base image version: %w", err)
	}
	published = true
	logger.Info("Base VM is ready", "name", cfg.TartBaseVM, "imageVersion", baseImageVersion)
	return nil
}

func readImageVersion(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read base image version: %w", err)
	}
	return strings.TrimSpace(string(contents)), nil
}

func waitForVMProcess(command *exec.Cmd, done <-chan error, timeout time.Duration) {
	if command == nil || done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		<-done
	}
}
