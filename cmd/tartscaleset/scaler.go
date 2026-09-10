package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	resources "github.com/xtool-org/xtool-runner"
)

type tartScaler struct {
	cfg            config
	runners        runnerState
	scaleSetID     int
	scalesetClient *scaleset.Client
	runnerScript   string
	logger         *slog.Logger
}

type runnerVM struct {
	name       string
	busy       bool
	vmCommand  *exec.Cmd
	vmDone     <-chan error
	runnerDone <-chan error
	vmLog      *os.File
}

func newTartScaler(cfg config, client *scaleset.Client, scaleSetID int, logger *slog.Logger) (*tartScaler, error) {
	scaler := &tartScaler{
		cfg:            cfg,
		runners:        newRunnerState(),
		scaleSetID:     scaleSetID,
		scalesetClient: client,
		runnerScript:   resources.RunRunnerScript,
		logger:         logger,
	}
	if err := scaler.cleanupOrphanedVMs(context.Background()); err != nil {
		return nil, err
	}
	return scaler, nil
}

func (s *tartScaler) HandleDesiredRunnerCount(ctx context.Context, count int) (int, error) {
	currentCount := s.runners.count()
	targetCount := min(s.cfg.MaxRunners, s.cfg.MinRunners+count)
	if targetCount <= currentCount {
		return currentCount, nil
	}

	s.logger.Info("Scaling up runners", "current", currentCount, "target", targetCount)
	for range targetCount - currentCount {
		if _, err := s.startRunner(ctx); err != nil {
			return s.runners.count(), fmt.Errorf("start Tart runner: %w", err)
		}
	}
	return s.runners.count(), nil
}

func (s *tartScaler) HandleJobStarted(_ context.Context, job *scaleset.JobStarted) error {
	s.logger.Info("Job started", "runner", job.RunnerName, "jobId", job.JobID)
	if !s.runners.markBusy(job.RunnerName) {
		s.logger.Warn("Job started on an untracked runner", "runner", job.RunnerName)
	}
	return nil
}

func (s *tartScaler) HandleJobCompleted(ctx context.Context, job *scaleset.JobCompleted) error {
	s.logger.Info("Job completed", "runner", job.RunnerName, "jobId", job.JobID, "result", job.Result)
	return s.removeRunner(ctx, job.RunnerName)
}

func (s *tartScaler) startRunner(ctx context.Context) (_ string, returnedErr error) {
	name, err := s.newRunnerName()
	if err != nil {
		return "", err
	}
	marker := filepath.Join(s.runnerMarkerDir(), name)
	if err := os.MkdirAll(s.runnerMarkerDir(), 0o700); err != nil {
		return "", fmt.Errorf("create runner state directory: %w", err)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		return "", fmt.Errorf("record runner VM: %w", err)
	}

	runner := &runnerVM{name: name}
	defer func() {
		if returnedErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.destroyRunner(cleanupCtx, runner); err != nil {
				s.logger.Error("Failed to clean up runner after start failure", "runner", name, "error", err)
			}
		}
	}()

	s.logger.Info("Creating disposable VM", "runner", name, "base", s.cfg.TartBaseVM)
	if err := runTart(ctx, s.cfg, "clone", s.cfg.TartBaseVM, name); err != nil {
		return "", err
	}
	if err := runTart(ctx, s.cfg, "set", name, "--random-mac"); err != nil {
		return "", err
	}

	logDir := filepath.Join(s.cfg.StateDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return "", fmt.Errorf("create log directory: %w", err)
	}
	logPath := filepath.Join(logDir, name+".vm.log")
	runner.vmLog, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("open VM log: %w", err)
	}
	vmArgs := tartRunArguments(s.cfg, name)
	runner.vmCommand = newTartCommand(context.Background(), s.cfg, vmArgs...)
	runner.vmCommand.Stdout = runner.vmLog
	runner.vmCommand.Stderr = runner.vmLog
	if err := runner.vmCommand.Start(); err != nil {
		return "", fmt.Errorf("start Tart VM: %w", err)
	}
	runner.vmDone = waitForCommand(runner.vmCommand)

	if err := waitForTartGuest(ctx, s.cfg, name); err != nil {
		return "", err
	}
	if err := s.mountXcode(ctx, name); err != nil {
		return "", err
	}

	jit, err := s.scalesetClient.GenerateJitRunnerConfig(
		ctx,
		&scaleset.RunnerScaleSetJitRunnerSetting{Name: name, WorkFolder: "_work"},
		s.scaleSetID,
	)
	if err != nil {
		return "", fmt.Errorf("generate JIT runner config: %w", err)
	}

	runnerCommand := newTartCommand(context.Background(), s.cfg, "exec", "-i", name, "/bin/bash", "-s")
	runnerCommand.Stdin = strings.NewReader(
		"RUNNER_JIT_CONFIG=" + shellQuote(jit.EncodedJITConfig) + "\n" + s.runnerScript,
	)
	runnerCommand.Stdout = os.Stdout
	runnerCommand.Stderr = os.Stderr
	if err := runnerCommand.Start(); err != nil {
		return "", fmt.Errorf("start runner in Tart VM: %w", err)
	}
	runner.runnerDone = waitForCommand(runnerCommand)
	s.runners.add(runner)

	go func() {
		err := <-runner.runnerDone
		if err != nil {
			s.logger.Warn("Runner process exited", "runner", name, "error", err)
		} else {
			s.logger.Info("Runner process exited", "runner", name)
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.removeRunner(cleanupCtx, name); err != nil {
			s.logger.Error("Failed to remove exited runner", "runner", name, "error", err)
		}
	}()

	s.logger.Info("Runner is starting", "runner", name)
	return name, nil
}

func (s *tartScaler) removeRunner(ctx context.Context, name string) error {
	runner, ok := s.runners.take(name)
	if !ok {
		return nil
	}
	s.logger.Info("Destroying disposable VM", "runner", name)
	return s.destroyRunner(ctx, runner)
}

func (s *tartScaler) destroyRunner(ctx context.Context, runner *runnerVM) error {
	var cleanupErrors []error
	if exists, err := tartVMExists(ctx, s.cfg, runner.name); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	} else if exists {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		if err := runTart(stopCtx, s.cfg, "stop", runner.name, "--timeout", "10"); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
		cancel()
	}

	if runner.vmDone != nil {
		select {
		case <-runner.vmDone:
		case <-time.After(15 * time.Second):
			if runner.vmCommand != nil && runner.vmCommand.Process != nil {
				_ = runner.vmCommand.Process.Kill()
			}
		}
	}
	if runner.vmLog != nil {
		_ = runner.vmLog.Close()
	}

	if exists, err := tartVMExists(ctx, s.cfg, runner.name); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	} else if exists {
		if err := runTart(ctx, s.cfg, "delete", runner.name); err != nil {
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	if err := os.Remove(filepath.Join(s.runnerMarkerDir(), runner.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("remove runner state marker: %w", err))
	}
	return errors.Join(cleanupErrors...)
}

func (s *tartScaler) shutdown(ctx context.Context) {
	s.logger.Info("Shutting down Tart runners")
	for _, runner := range s.runners.takeAll() {
		s.logger.Info("Destroying runner during shutdown", "runner", runner.name, "busy", runner.busy)
		if err := s.destroyRunner(ctx, runner); err != nil {
			s.logger.Error("Failed to destroy runner", "runner", runner.name, "error", err)
		}
	}
}

func (s *tartScaler) cleanupOrphanedVMs(ctx context.Context) error {
	entries, err := os.ReadDir(s.runnerMarkerDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read runner state directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		s.logger.Info("Cleaning orphaned runner VM", "runner", name)
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		err := s.destroyRunner(cleanupCtx, &runnerVM{name: name})
		cancel()
		if err != nil {
			return fmt.Errorf("clean orphaned runner %s: %w", name, err)
		}
	}
	return nil
}

func (s *tartScaler) mountXcode(ctx context.Context, name string) error {
	if s.cfg.XcodeAppPath == "" {
		return nil
	}
	script := `set -e
sudo -n install -d -o root -g root /usr/local/share/Xcode.app
sudo -n mount -t virtiofs -o ro xcode /usr/local/share/Xcode.app
test -d /usr/local/share/Xcode.app/Contents/Developer`
	if err := runTart(ctx, s.cfg, "exec", name, "/bin/bash", "-c", script); err != nil {
		return fmt.Errorf("mount Xcode in runner VM: %w", err)
	}
	return nil
}

func (s *tartScaler) newRunnerName() (string, error) {
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate runner name: %w", err)
	}
	return fmt.Sprintf("%s-%d-%s", s.cfg.RunnerNamePrefix, time.Now().Unix(), hex.EncodeToString(random)), nil
}

func (s *tartScaler) runnerMarkerDir() string {
	return filepath.Join(s.cfg.StateDir, "runners", stateKey(s.cfg.ScaleSetName))
}

func waitForCommand(command *exec.Cmd) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
		close(done)
	}()
	return done
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

var _ listener.Scaler = (*tartScaler)(nil)
