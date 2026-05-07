package sageagents

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrGenerateIdentityLoadsRawPrivateKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	keyFile := filepath.Join(t.TempDir(), "agent.key")
	if err := os.WriteFile(keyFile, priv, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var agent BaseAgent
	if err := agent.LoadOrGenerateIdentity(keyFile); err != nil {
		t.Fatalf("LoadOrGenerateIdentity: %v", err)
	}

	if string(agent.PrivateKey) != string(priv) {
		t.Fatalf("loaded private key does not match raw file contents")
	}
}

func TestLoadOrGenerateIdentityLoadsBase64URLPrivateKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	keyFile := filepath.Join(t.TempDir(), "agent.key")
	encoded := base64.RawURLEncoding.EncodeToString(priv)
	if err := os.WriteFile(keyFile, []byte(encoded+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var agent BaseAgent
	if err := agent.LoadOrGenerateIdentity(keyFile); err != nil {
		t.Fatalf("LoadOrGenerateIdentity: %v", err)
	}

	if string(agent.PrivateKey) != string(priv) {
		t.Fatalf("loaded private key does not match decoded base64url contents")
	}
}

func TestLoadOrGenerateIdentityRejectsInvalidExistingFile(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "agent.key")
	if err := os.WriteFile(keyFile, []byte("not-a-key"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var agent BaseAgent
	err := agent.LoadOrGenerateIdentity(keyFile)
	if err == nil {
		t.Fatalf("expected invalid key file to return an error")
	}
	if !strings.Contains(err.Error(), "not a valid Ed25519 private key") {
		t.Fatalf("unexpected error: %v", err)
	}
}
