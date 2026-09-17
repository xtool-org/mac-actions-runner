package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
