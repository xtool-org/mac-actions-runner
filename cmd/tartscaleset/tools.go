package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	tartVersion    = "2.32.1"
	tartURL        = "https://github.com/openai/tart/releases/download/2.32.1/tart.tar.gz"
	softnetVersion = "0.19.0"
	softnetURL     = "https://github.com/openai/softnet/releases/download/0.19.0/softnet.tar.gz"
	softnetSHA256  = "1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"
)

type toolManager struct {
	stateDir string
}

func (m toolManager) ensureTart(ctx context.Context) (string, error) {
	return m.install(ctx, "Tart", "tart", tartVersion, tartURL, "", filepath.Join("tart.app", "Contents", "MacOS", "tart"))
}

func (m toolManager) ensureSoftnet(ctx context.Context) (string, error) {
	return m.install(ctx, "Softnet", "softnet", softnetVersion, softnetURL, softnetSHA256, "softnet")
}

func (m toolManager) install(ctx context.Context, displayName, name, version, downloadURL, checksum, executable string) (string, error) {
	root := filepath.Join(m.stateDir, "tools", name)
	installDir := filepath.Join(root, version)
	executablePath := filepath.Join(installDir, executable)
	if isExecutable(executablePath) {
		return executablePath, nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create %s tool directory: %w", name, err)
	}
	temporaryDir, err := os.MkdirTemp(root, ".install-"+version+"-")
	if err != nil {
		return "", fmt.Errorf("create temporary %s directory: %w", name, err)
	}
	defer os.RemoveAll(temporaryDir)

	archive := filepath.Join(temporaryDir, name+".tar.gz")
	fmt.Fprintf(os.Stderr, "Downloading %s %s.\n", displayName, version)
	if err := downloadFile(ctx, downloadURL, archive, checksum); err != nil {
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	stagedDir := filepath.Join(temporaryDir, "extracted")
	if err := extractTarGzip(archive, stagedDir); err != nil {
		return "", fmt.Errorf("extract %s: %w", name, err)
	}
	if !isExecutable(filepath.Join(stagedDir, executable)) {
		return "", fmt.Errorf("%s archive did not contain executable %s", displayName, executable)
	}
	if err := os.RemoveAll(installDir); err != nil {
		return "", fmt.Errorf("replace incomplete %s installation: %w", name, err)
	}
	if err := os.Rename(stagedDir, installDir); err != nil {
		return "", fmt.Errorf("install %s: %w", name, err)
	}
	return executablePath, nil
}

func downloadFile(ctx context.Context, sourceURL, destination, expectedSHA256 string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "xtool-tart-scale-set")
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", response.Status)
	}

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), response.Body)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if expectedSHA256 != "" {
		actual := hex.EncodeToString(hash.Sum(nil))
		if actual != expectedSHA256 {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, actual)
		}
	}
	return nil
}

func extractTarGzip(archive, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}

	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := archiveTarget(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode).Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, reader)
			if err := errors.Join(copyErr, output.Close()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry %q (type %d)", header.Name, header.Typeflag)
		}
	}
}

func archiveTarget(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return filepath.Join(root, clean), nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func setup(ctx context.Context) error {
	cfg, err := parseConfig()
	if err != nil {
		return err
	}
	if err := cfg.resolveStateDir(); err != nil {
		return err
	}
	if cfg.SoftnetBin == "auto" {
		cfg.SoftnetBin, err = (toolManager{stateDir: cfg.StateDir}).ensureSoftnet(ctx)
	} else {
		cfg.SoftnetBin, err = filepath.Abs(cfg.SoftnetBin)
	}
	if err != nil {
		return err
	}
	if requirePrivilegedSoftnet(cfg.SoftnetBin) == nil {
		fmt.Println("Softnet is already configured:", cfg.SoftnetBin)
		return nil
	}

	fmt.Println("Configuring Softnet with the root ownership and setuid bit required by Tart.")
	commands := [][]string{{"/usr/sbin/chown", "root:wheel", cfg.SoftnetBin}, {"/bin/chmod", "u+s", cfg.SoftnetBin}}
	for _, arguments := range commands {
		name, args := arguments[0], arguments[1:]
		if os.Geteuid() != 0 {
			args = append([]string{name}, args...)
			name = "/usr/bin/sudo"
		}
		command := exec.CommandContext(ctx, name, args...)
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("configure Softnet: %w", err)
		}
	}
	if err := requirePrivilegedSoftnet(cfg.SoftnetBin); err != nil {
		return err
	}
	fmt.Println("Softnet is configured:", cfg.SoftnetBin)
	return nil
}

func requirePrivilegedSoftnet(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect Softnet: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode()&os.ModeSetuid == 0 {
		return fmt.Errorf("Softnet needs its one-time privilege setup; run: tartscaleset setup")
	}
	return nil
}
