package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
)

const controllerVersion = "1"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch {
	case len(os.Args) == 1:
		err = run(ctx)
	case len(os.Args) == 2 && os.Args[1] == "setup":
		err = setup(ctx)
	case len(os.Args) == 2 && os.Args[1] == "register":
		err = registerLaunchAgent(ctx)
	case len(os.Args) == 2 && os.Args[1] == "unregister":
		err = unregisterLaunchAgent(ctx)
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [setup|register|unregister]\n", os.Args[0])
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	lock, err := acquireControllerLock(cfg)
	if err != nil {
		return err
	}
	defer lock.Close()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	privateKey, err := os.ReadFile(cfg.AppPrivateKeyFile)
	if err != nil {
		return fmt.Errorf("read GitHub App private key: %w", err)
	}
	if err := prepareBaseImage(ctx, cfg, logger.WithGroup("image")); err != nil {
		return fmt.Errorf("prepare base image: %w", err)
	}
	installationID, err := discoverInstallationID(ctx, cfg, privateKey)
	if err != nil {
		return err
	}
	client, err := scaleset.NewClientWithGitHubApp(scaleset.ClientWithGitHubAppConfig{
		GitHubConfigURL: cfg.GitHubRunnerURL,
		GitHubAppAuth: scaleset.GitHubAppAuth{
			ClientID:       cfg.AppID,
			InstallationID: installationID,
			PrivateKey:     string(privateKey),
		},
		SystemInfo: systemInfo(0),
	})
	if err != nil {
		return fmt.Errorf("create scale-set client: %w", err)
	}

	runnerGroupID := 1
	if cfg.RunnerGroup != scaleset.DefaultRunnerGroup {
		group, err := client.GetRunnerGroupByName(ctx, cfg.RunnerGroup)
		if err != nil {
			return fmt.Errorf("find runner group %s: %w", cfg.RunnerGroup, err)
		}
		runnerGroupID = group.ID
	}

	desiredScaleSet := &scaleset.RunnerScaleSet{
		Name:          cfg.ScaleSetName,
		RunnerGroupID: runnerGroupID,
		Labels:        cfg.scaleSetLabels(),
		RunnerSetting: scaleset.RunnerSetting{DisableUpdate: true},
	}
	scaleSet, err := client.GetRunnerScaleSet(ctx, runnerGroupID, cfg.ScaleSetName)
	if err != nil {
		return fmt.Errorf("find existing runner scale set: %w", err)
	}
	if scaleSet == nil {
		scaleSet, err = client.CreateRunnerScaleSet(ctx, desiredScaleSet)
		if err != nil {
			return fmt.Errorf("create runner scale set: %w", err)
		}
		logger.Info("Created runner scale set", "name", scaleSet.Name, "id", scaleSet.ID)
	} else {
		scaleSet, err = client.UpdateRunnerScaleSet(ctx, scaleSet.ID, desiredScaleSet)
		if err != nil {
			return fmt.Errorf("update existing runner scale set: %w", err)
		}
		logger.Info("Reusing runner scale set", "name", scaleSet.Name, "id", scaleSet.ID)
	}
	client.SetSystemInfo(systemInfo(scaleSet.ID))
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		logger.Info("Deleting runner scale set", "name", scaleSet.Name, "id", scaleSet.ID)
		if err := client.DeleteRunnerScaleSet(cleanupCtx, scaleSet.ID); err != nil {
			logger.Error("Failed to delete runner scale set", "error", err)
		}
	}()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "xtool-tart-controller"
	}
	sessionClient, err := client.MessageSessionClient(ctx, scaleSet.ID, fmt.Sprintf("%s-%d", hostname, os.Getpid()))
	if err != nil {
		return fmt.Errorf("create scale-set message session: %w", err)
	}
	defer sessionClient.Close(context.Background())

	scaler, err := newTartScaler(cfg, client, scaleSet.ID, logger.WithGroup("tart"))
	if err != nil {
		return fmt.Errorf("initialize Tart scaler: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		scaler.shutdown(cleanupCtx)
	}()

	scaleSetListener, err := listener.New(sessionClient, listener.Config{
		ScaleSetID: scaleSet.ID,
		MaxRunners: cfg.MaxRunners,
		Logger:     logger.WithGroup("listener"),
	})
	if err != nil {
		return fmt.Errorf("create scale-set listener: %w", err)
	}
	logger.Info(
		"Listening for jobs",
		"scaleSet", cfg.ScaleSetName,
		"minRunners", cfg.MinRunners,
		"maxRunners", cfg.MaxRunners,
	)
	if err := scaleSetListener.Run(ctx, scaler); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("scale-set listener: %w", err)
	}
	return nil
}

func systemInfo(scaleSetID int) scaleset.SystemInfo {
	return scaleset.SystemInfo{
		System:     "xtool-tart-runner",
		Subsystem:  "tart-scale-set",
		Version:    controllerVersion,
		CommitSHA:  "unknown",
		ScaleSetID: scaleSetID,
	}
}
