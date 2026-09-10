package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/actions/scaleset"
	"github.com/caarlos0/env/v11"
)

type config struct {
	AppID             string   `env:"APP_ID" envDefault:"4891121"`
	AppPrivateKeyFile string   `env:"APP_PRIVATE_KEY_FILE" envDefault:"./runner-private-key.pem"`
	GitHubAPIURL      string   `env:"GITHUB_API_URL" envDefault:"https://api.github.com"`
	GitHubRunnerURL   string   `env:"GITHUB_RUNNER_URL" envDefault:"auto"`
	OrgName           string   `env:"ORG_NAME" envDefault:"xtool-org"`
	RunnerGroup       string   `env:"RUNNER_GROUP" envDefault:"xtool-runners"`
	RunnerLabels      []string `env:"RUNNER_LABELS" envDefault:"xtool-runner,ubuntu-on-macos" envSeparator:","`
	RunnerNamePrefix  string   `env:"RUNNER_NAME_PREFIX" envDefault:"xtool-runner"`
	ScaleSetName      string   `env:"RUNNER_SCALE_SET_NAME" envDefault:"xtool-runner"`
	MinRunners        int      `env:"RUNNER_MIN_COUNT" envDefault:"1"`
	MaxRunners        int      `env:"RUNNER_MAX_COUNT" envDefault:"1"`
	TartBin           string   `env:"TART_BIN" envDefault:"auto"`
	TartSourceImage   string   `env:"TART_SOURCE_IMAGE" envDefault:"ghcr.io/cirruslabs/ubuntu:latest"`
	TartBaseVM        string   `env:"TART_BASE_VM" envDefault:"xtool-runner-base"`
	TartCPU           int      `env:"TART_CPU" envDefault:"4"`
	TartMemoryMB      int      `env:"TART_MEMORY_MB" envDefault:"8192"`
	TartDiskGB        int      `env:"TART_DISK_GB" envDefault:"50"`
	TartNetworkMode   string   `env:"TART_NETWORK_MODE" envDefault:"softnet"`
	TartSoftnetAllow  string   `env:"TART_SOFTNET_ALLOW"`
	SoftnetBin        string   `env:"SOFTNET_BIN" envDefault:"auto"`
	XcodeAppPath      string   `env:"XCODE_APP_PATH" envDefault:"auto"`
	StateDir          string   `env:"RUNNER_STATE_DIR" envDefault:".state"`
	VMReadyTimeoutSec int      `env:"VM_READY_TIMEOUT_SECONDS" envDefault:"180"`
}

func parseConfig() (config, error) {
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return config{}, fmt.Errorf("parse environment: %w", err)
	}
	cfg.RunnerLabels = trimStrings(cfg.RunnerLabels)
	return cfg, nil
}

func loadConfig(ctx context.Context) (config, error) {
	cfg, err := parseConfig()
	if err != nil {
		return config{}, err
	}
	if err := cfg.resolveStateDir(); err != nil {
		return config{}, err
	}
	tools := toolManager{stateDir: cfg.StateDir}
	if cfg.TartBin == "auto" {
		cfg.TartBin, err = tools.ensureTart(ctx)
	} else {
		cfg.TartBin, err = filepath.Abs(cfg.TartBin)
	}
	if err != nil {
		return config{}, err
	}
	if cfg.TartNetworkMode == "softnet" {
		if cfg.SoftnetBin == "auto" {
			cfg.SoftnetBin, err = tools.ensureSoftnet(ctx)
		} else {
			cfg.SoftnetBin, err = filepath.Abs(cfg.SoftnetBin)
		}
		if err != nil {
			return config{}, err
		}
		if err := requirePrivilegedSoftnet(cfg.SoftnetBin); err != nil {
			return config{}, err
		}
	}
	if err := cfg.resolveRuntimePaths(); err != nil {
		return config{}, err
	}
	if err := cfg.validate(); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func trimStrings(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			trimmed = append(trimmed, value)
		}
	}
	return trimmed
}

func (c *config) resolveStateDir() error {
	stateDir, err := filepath.Abs(c.StateDir)
	if err != nil {
		return fmt.Errorf("resolve RUNNER_STATE_DIR: %w", err)
	}
	c.StateDir = stateDir
	return nil
}

func (c *config) resolveRuntimePaths() error {
	if c.GitHubRunnerURL == "auto" {
		c.GitHubRunnerURL = "https://github.com/" + c.OrgName
	}
	if c.XcodeAppPath == "none" {
		c.XcodeAppPath = ""
	}
	if c.XcodeAppPath == "auto" {
		output, err := exec.Command("/usr/bin/xcode-select", "-p").Output()
		if err != nil {
			return fmt.Errorf("derive XCODE_APP_PATH with xcode-select -p: %w", err)
		}
		developerDir := strings.TrimSpace(string(output))
		const suffix = "/Contents/Developer"
		if !strings.HasSuffix(developerDir, suffix) {
			return fmt.Errorf("xcode-select does not point inside an Xcode.app: %s", developerDir)
		}
		c.XcodeAppPath = strings.TrimSuffix(developerDir, suffix)
	}
	var err error
	if c.AppPrivateKeyFile, err = filepath.Abs(c.AppPrivateKeyFile); err != nil {
		return fmt.Errorf("resolve APP_PRIVATE_KEY_FILE: %w", err)
	}
	return nil
}

func (c config) validate() error {
	required := map[string]string{
		"APP_ID": c.AppID, "APP_PRIVATE_KEY_FILE": c.AppPrivateKeyFile,
		"GITHUB_API_URL": c.GitHubAPIURL, "GITHUB_RUNNER_URL": c.GitHubRunnerURL,
		"ORG_NAME": c.OrgName, "RUNNER_GROUP": c.RunnerGroup,
		"RUNNER_NAME_PREFIX": c.RunnerNamePrefix, "RUNNER_SCALE_SET_NAME": c.ScaleSetName,
		"TART_BASE_VM": c.TartBaseVM, "TART_BIN": c.TartBin,
		"TART_NETWORK_MODE": c.TartNetworkMode, "TART_SOURCE_IMAGE": c.TartSourceImage,
	}
	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if _, err := url.ParseRequestURI(c.GitHubAPIURL); err != nil {
		return fmt.Errorf("invalid GITHUB_API_URL: %w", err)
	}
	if _, err := url.ParseRequestURI(c.GitHubRunnerURL); err != nil {
		return fmt.Errorf("invalid GITHUB_RUNNER_URL: %w", err)
	}
	if c.MinRunners < 0 || c.MaxRunners < c.MinRunners {
		return fmt.Errorf("runner counts must satisfy 0 <= RUNNER_MIN_COUNT <= RUNNER_MAX_COUNT")
	}
	if c.VMReadyTimeoutSec <= 0 || c.TartCPU <= 0 || c.TartMemoryMB <= 0 || c.TartDiskGB <= 0 {
		return fmt.Errorf("VM timeout, CPU, memory, and disk settings must be greater than 0")
	}
	if strings.ContainsAny(c.RunnerNamePrefix, "/:") {
		return fmt.Errorf("RUNNER_NAME_PREFIX cannot contain '/' or ':'")
	}
	if c.TartNetworkMode != "shared" && c.TartNetworkMode != "softnet" && c.TartNetworkMode != "host" {
		return fmt.Errorf("unsupported TART_NETWORK_MODE: %s", c.TartNetworkMode)
	}
	if info, err := os.Stat(c.TartBin); err != nil {
		return fmt.Errorf("inspect TART_BIN: %w", err)
	} else if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("TART_BIN is not executable: %s", c.TartBin)
	}
	if c.XcodeAppPath != "" {
		if strings.ContainsRune(c.XcodeAppPath, ':') {
			return fmt.Errorf("XCODE_APP_PATH cannot contain a colon")
		}
		developerDir := filepath.Join(c.XcodeAppPath, "Contents", "Developer")
		if info, err := os.Stat(developerDir); err != nil {
			return fmt.Errorf("inspect XCODE_APP_PATH: %w", err)
		} else if !info.IsDir() {
			return fmt.Errorf("XCODE_APP_PATH has no Contents/Developer directory: %s", c.XcodeAppPath)
		}
	}
	if len(c.RunnerLabels) == 0 {
		return fmt.Errorf("RUNNER_LABELS must contain at least one label")
	}
	return nil
}

func (c config) scaleSetLabels() []scaleset.Label {
	labels := make([]scaleset.Label, 0, len(c.RunnerLabels))
	for _, label := range c.RunnerLabels {
		labels = append(labels, scaleset.Label{Name: label})
	}
	return labels
}

func (c config) vmReadyTimeout() time.Duration {
	return time.Duration(c.VMReadyTimeoutSec) * time.Second
}
