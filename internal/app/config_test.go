package app

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseArgsRequiresCredentials(t *testing.T) {
	var stderr bytes.Buffer

	_, err := ParseArgs([]string{"--token", "abc"}, &stderr)
	if !errors.Is(err, ErrMissingRequiredFlag) {
		t.Fatalf("expected ErrMissingRequiredFlag, got %v", err)
	}
}

func TestParseArgsParsesConfig(t *testing.T) {
	var stderr bytes.Buffer

	result, err := ParseArgs([]string{
		"--token", "token",
		"--secret", "secret",
		"--server", "ws://localhost:8080",
		"--data-dir", "./state",
	}, &stderr)
	if err != nil {
		t.Fatalf("ParseArgs returned error: %v", err)
	}

	if result.Config.Token != "token" {
		t.Fatalf("unexpected token: %q", result.Config.Token)
	}

	if result.Config.Secret != "secret" {
		t.Fatalf("unexpected secret: %q", result.Config.Secret)
	}

	if result.Config.Server != "ws://localhost:8080" {
		t.Fatalf("unexpected server: %q", result.Config.Server)
	}

	if result.Config.DataDir != "./state" {
		t.Fatalf("unexpected data-dir: %q", result.Config.DataDir)
	}
}
