package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestConfigureVMOutput(t *testing.T) {
	stateDir := t.TempDir()
	scaler := &tartScaler{cfg: config{StateDir: stateDir}}
	runner := &runnerVM{name: "xtool-runner-test", vmCommand: &exec.Cmd{}}
	if err := scaler.configureVMOutput(runner); err != nil {
		t.Fatal(err)
	}
	if runner.vmLog != nil || runner.vmCommand.Stdout != nil || runner.vmCommand.Stderr != nil {
		t.Fatal("VM output should be discarded by default")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "logs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default VM output created a logs directory: %v", err)
	}

	scaler.cfg.TartVMLogs = true
	runner = &runnerVM{name: "xtool-runner-test", vmCommand: &exec.Cmd{}}
	if err := scaler.configureVMOutput(runner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.vmLog.Close() })
	if runner.vmCommand.Stdout != runner.vmLog || runner.vmCommand.Stderr != runner.vmLog {
		t.Fatal("enabled VM output should use the per-VM log file")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "logs", runner.name+".vm.log")); err != nil {
		t.Fatalf("enabled VM output did not create a log file: %v", err)
	}
}

func TestRunnerAlive(t *testing.T) {
	for _, test := range []struct {
		name      string
		exitCode  string
		response  string
		wantAlive bool
		wantErr   bool
	}{
		{name: "listener present", exitCode: "0", response: "alive", wantAlive: true},
		{name: "listener absent", exitCode: "0", response: "absent"},
		{name: "probe failure", exitCode: "2", wantErr: true},
		{name: "unexpected response", exitCode: "0", response: "other", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tart := filepath.Join(t.TempDir(), "tart")
			script := "#!/bin/sh\n[ \"$1\" = exec ] && [ \"$3\" = /bin/sh ] && [ \"$4\" = -c ] || exit 2\necho " + test.response + "\nexit " + test.exitCode + "\n"
			if err := os.WriteFile(tart, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			alive, err := runnerAlive(context.Background(), config{TartBin: tart}, "runner-vm")
			if alive != test.wantAlive || (err != nil) != test.wantErr {
				t.Fatalf("runnerAlive() = (%t, %v), want alive=%t error=%t", alive, err, test.wantAlive, test.wantErr)
			}
		})
	}
}

func TestRunnerHealth(t *testing.T) {
	var health runnerHealth
	for range 2 {
		if health.observe(false, nil) {
			t.Fatal("runner declared unhealthy before three confirmed absences")
		}
	}
	if !health.observe(false, nil) {
		t.Fatal("runner not declared unhealthy after three confirmed absences")
	}
	if health.observe(true, nil) || health.absences != 0 {
		t.Fatal("healthy probe did not reset absence count")
	}
	for range 11 {
		if health.observe(false, errors.New("temporary RPC failure")) {
			t.Fatal("runner declared unhealthy before twelve probe errors")
		}
	}
	if !health.observe(false, errors.New("temporary RPC failure")) {
		t.Fatal("runner not declared unhealthy after twelve probe errors")
	}
}
