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
		WorkingDirectory:  "/Users/test & runner",
		StandardOutPath:   "/tmp/stdout.log",
		StandardErrorPath: "/tmp/stderr.log",
		Environment:       map[string]string{"RUNNER_LABELS": "one,<two>"},
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
}

func TestConfiguredLaunchEnvironmentExcludesUnrecognizedVariables(t *testing.T) {
	t.Setenv("APP_ID", "1234")
	t.Setenv("UNRELATED_SECRET", "do-not-copy")

	environment := configuredLaunchEnvironment()
	if environment["APP_ID"] != "1234" {
		t.Fatalf("APP_ID = %q, want 1234", environment["APP_ID"])
	}
	if _, ok := environment["UNRELATED_SECRET"]; ok {
		t.Fatal("unrecognized environment variable was captured")
	}
}
