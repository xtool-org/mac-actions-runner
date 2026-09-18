package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const launchAgentLabel = "sh.xtool.tartscaleset"

type launchAgentConfig struct {
	Executable        string
	StandardOutPath   string
	StandardErrorPath string
}

func registerLaunchAgent(ctx context.Context) error {
	if err := requireLaunchAgentUser(); err != nil {
		return err
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if info, err := os.Stat(cfg.AppPrivateKeyFile); err != nil {
		return fmt.Errorf("inspect GitHub App private key: %w", err)
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("GitHub App private key is not a regular file: %s", cfg.AppPrivateKeyFile)
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find tartscaleset executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve tartscaleset executable: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	launchAgentsDir := launchAgentsDirectory(homeDir)
	logDir := filepath.Join(cfg.StateDir, "logs")
	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	plistPath := filepath.Join(launchAgentsDir, launchAgentLabel+".plist")
	plist := renderLaunchAgentPlist(launchAgentConfig{
		Executable:        executable,
		StandardOutPath:   filepath.Join(logDir, "launchd.log"),
		StandardErrorPath: filepath.Join(logDir, "launchd.error.log"),
	})
	if err := writeFileAtomically(plistPath, plist, 0o644); err != nil {
		return fmt.Errorf("write LaunchAgent: %w", err)
	}

	domainTarget := fmt.Sprintf("gui/%d", os.Getuid())
	serviceTarget := domainTarget + "/" + launchAgentLabel
	loaded, err := launchAgentLoaded(ctx, serviceTarget)
	if err != nil {
		return err
	}
	if loaded {
		if err := runLaunchctl(ctx, "bootout", "--wait", serviceTarget); err != nil {
			return err
		}
	}
	lock, err := acquireControllerLock(cfg)
	if err != nil {
		return fmt.Errorf("cannot start LaunchAgent: %w", err)
	}
	if err := lock.Close(); err != nil {
		return fmt.Errorf("release controller lock: %w", err)
	}
	if err := runLaunchctl(ctx, "enable", serviceTarget); err != nil {
		return err
	}
	if err := runLaunchctl(ctx, "bootstrap", domainTarget, plistPath); err != nil {
		return err
	}

	fmt.Println("Registered and started", launchAgentLabel)
	fmt.Println("LaunchAgent:", plistPath)
	fmt.Println("Logs:", logDir)
	return nil
}

func unregisterLaunchAgent(ctx context.Context) error {
	if err := requireLaunchAgentUser(); err != nil {
		return err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	domainTarget := fmt.Sprintf("gui/%d", os.Getuid())
	serviceTarget := domainTarget + "/" + launchAgentLabel
	loaded, err := launchAgentLoaded(ctx, serviceTarget)
	if err != nil {
		return err
	}
	if loaded {
		if err := runLaunchctl(ctx, "bootout", "--wait", serviceTarget); err != nil {
			return err
		}
	}

	plistPath := filepath.Join(launchAgentsDirectory(homeDir), launchAgentLabel+".plist")
	if err := os.Remove(plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove LaunchAgent: %w", err)
	}
	fmt.Println("Unregistered", launchAgentLabel)
	fmt.Println("Runner state and logs were left in place.")
	return nil
}

func requireLaunchAgentUser() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("LaunchAgent management is only supported on macOS")
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("manage the LaunchAgent as your normal user, not with sudo")
	}
	return nil
}

func launchAgentsDirectory(homeDir string) string {
	return filepath.Join(homeDir, "Library", "LaunchAgents")
}

func renderLaunchAgentPlist(cfg launchAgentConfig) []byte {
	var plist strings.Builder
	plist.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	plist.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	plist.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writePlistString(&plist, "  ", "Label", launchAgentLabel)
	plist.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	writePlistValue(&plist, "    ", "string", cfg.Executable)
	plist.WriteString("  </array>\n")
	writePlistString(&plist, "  ", "StandardOutPath", cfg.StandardOutPath)
	writePlistString(&plist, "  ", "StandardErrorPath", cfg.StandardErrorPath)
	plist.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	plist.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	plist.WriteString("  <key>ProcessType</key>\n  <string>Background</string>\n")
	plist.WriteString("  <key>ThrottleInterval</key>\n  <integer>10</integer>\n")
	plist.WriteString("</dict>\n</plist>\n")
	return []byte(plist.String())
}

func writePlistString(plist *strings.Builder, indent, key, value string) {
	writePlistValue(plist, indent, "key", key)
	writePlistValue(plist, indent, "string", value)
}

func writePlistValue(plist *strings.Builder, indent, element, value string) {
	plist.WriteString(indent)
	plist.WriteByte('<')
	plist.WriteString(element)
	plist.WriteByte('>')
	_ = xml.EscapeText(plist, []byte(value))
	plist.WriteString("</")
	plist.WriteString(element)
	plist.WriteString(">\n")
}

func writeFileAtomically(path string, contents []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func launchAgentLoaded(ctx context.Context, serviceTarget string) (bool, error) {
	command := exec.CommandContext(ctx, "/bin/launchctl", "print", serviceTarget)
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return false, nil
		}
		return false, fmt.Errorf("inspect LaunchAgent: %w", err)
	}
	return true, nil
}

func runLaunchctl(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "/bin/launchctl", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, message)
		}
		return fmt.Errorf("launchctl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
