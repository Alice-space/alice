package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Alice-space/alice/internal/buildinfo"
)

func TestRootCmdVersionFlagPrintsVersion(t *testing.T) {
	originalVersion := buildinfo.Version
	buildinfo.Version = "v9.9.9"
	defer func() {
		buildinfo.Version = originalVersion
	}()

	cmd := newRootCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version flag failed: %v", err)
	}

	if got := strings.TrimSpace(stdout.String()); got != "v9.9.9" {
		t.Fatalf("unexpected version output: %q", got)
	}
}
