package main

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestRenderLaunchAgentPlist(t *testing.T) {
	plist := renderLaunchAgentPlist(launchAgentConfig{
		Executable:        "/usr/local/bin/tartscaleset",
		StandardOutPath:   "/tmp/stdout.log",
		StandardErrorPath: "/tmp/stderr.log",
		Environment:       map[string]string{"RUNNER_LABELS": "one,<two>", "RUNNER_STATE_DIR": "/Users/test & runner/state"},
	})

	decoder := xml.NewDecoder(strings.NewReader(string(plist)))
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
	}
	if strings.Contains(string(plist), "test & runner") || strings.Contains(string(plist), "one,<two>") {
		t.Fatal("plist contains unescaped values")
	}
	if strings.Contains(string(plist), "WorkingDirectory") {
		t.Fatal("plist should not set a working directory")
	}
}
