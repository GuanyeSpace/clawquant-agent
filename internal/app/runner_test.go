package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/GuanyeSpace/clawquant-agent/internal/buildinfo"
)

func TestRunnerShowsVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	runner := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Info: buildinfo.Info{
			Version:   "v0.1.0",
			Commit:    "abc1234",
			BuildTime: "2026-03-16T00:00:00Z",
		},
	}

	if err := runner.Run([]string{"-version"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	got := strings.TrimSpace(stdout.String())
	want := "version=v0.1.0 commit=abc1234 build_time=2026-03-16T00:00:00Z"

	if got != want {
		t.Fatalf("unexpected version output: got %q want %q", got, want)
	}
}

func TestRunnerShowsDefaultMessage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	runner := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Info:   buildinfo.Current(),
	}

	if err := runner.Run(nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	got := strings.TrimSpace(stdout.String())
	want := "clawquant-agent initialized. Add your strategy/runtime wiring under internal/."

	if got != want {
		t.Fatalf("unexpected default output: got %q want %q", got, want)
	}
}
