package sageagents

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCopilotAuthStateUsesSageAuthStore(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	stateDir := t.TempDir()
	if err := SaveGitHubOAuthToken(stateDir, "gho_test"); err != nil {
		t.Fatalf("SaveGitHubOAuthToken() error = %v", err)
	}

	status := CopilotAuthState(stateDir)
	if !status.Connected {
		t.Fatalf("expected connected auth state, got %+v", status)
	}
	if !status.OAuthStored {
		t.Fatalf("expected OAuthStored=true, got %+v", status)
	}
	if status.TokenSource != "sage-auth-store" {
		t.Fatalf("expected sage auth source, got %q", status.TokenSource)
	}
}

func TestCopilotAuthStateUsesEnvFallback(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "gho_env")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	status := CopilotAuthState(t.TempDir())
	if !status.Connected {
		t.Fatalf("expected connected auth state, got %+v", status)
	}
	if !status.EnvTokenAvailable {
		t.Fatalf("expected EnvTokenAvailable=true, got %+v", status)
	}
	if status.TokenSource != "environment" {
		t.Fatalf("expected environment source, got %q", status.TokenSource)
	}
}

func TestCopilotAuthStateUsesValidCachedTokenFirst(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	stateDir := t.TempDir()
	writeCachedToken(copilotTokenPath(stateDir), CopilotRuntimeAuth{
		Token:     "copilot-token",
		ExpiresAt: time.Now().Add(time.Hour),
		BaseURL:   defaultCopilotAPIBaseURL,
	})

	status := CopilotAuthState(stateDir)
	if !status.CachedTokenValid {
		t.Fatalf("expected valid cached token, got %+v", status)
	}
	if status.TokenSource != "cache" {
		t.Fatalf("expected cache source, got %q", status.TokenSource)
	}
}

func TestDeleteGitHubOAuthTokenRemovesAuthAndCache(t *testing.T) {
	stateDir := t.TempDir()
	if err := SaveGitHubOAuthToken(stateDir, "gho_test"); err != nil {
		t.Fatalf("SaveGitHubOAuthToken() error = %v", err)
	}
	writeCachedToken(copilotTokenPath(stateDir), CopilotRuntimeAuth{
		Token:     "copilot-token",
		ExpiresAt: time.Now().Add(time.Hour),
		BaseURL:   defaultCopilotAPIBaseURL,
	})

	if err := DeleteGitHubOAuthToken(stateDir); err != nil {
		t.Fatalf("DeleteGitHubOAuthToken() error = %v", err)
	}
	for _, filePath := range []string{
		filepath.Join(stateDir, "auth", "github-copilot.json"),
		filepath.Join(stateDir, "credentials", "github-copilot.token.json"),
	} {
		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err = %v", filePath, err)
		}
	}
}

func TestDeriveCopilotAPIBaseURLFromToken(t *testing.T) {
	token := "tid=abc;proxy-ep=https://proxy.individual.githubcopilot.com;exp=1"
	got := deriveCopilotAPIBaseURLFromToken(token)
	want := "https://api.individual.githubcopilot.com"
	if got != want {
		t.Fatalf("deriveCopilotAPIBaseURLFromToken() = %q, want %q", got, want)
	}
}

func TestResolveGitHubCopilotClientIDDefaultsToCopilotClient(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "")

	got := resolveGitHubCopilotClientID("")
	if got != defaultGitHubCopilotClientID {
		t.Fatalf("resolveGitHubCopilotClientID() = %q, want default", got)
	}
}

func TestResolveGitHubCopilotClientIDAllowsOverride(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "env-client")

	if got := resolveGitHubCopilotClientID(""); got != "env-client" {
		t.Fatalf("resolveGitHubCopilotClientID(env) = %q, want env-client", got)
	}
	if got := resolveGitHubCopilotClientID("body-client"); got != "body-client" {
		t.Fatalf("resolveGitHubCopilotClientID(body) = %q, want body-client", got)
	}
}

func TestCopilotAgentFallsBackToSageStateDirEnv(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	stateDir := t.TempDir()
	t.Setenv("SAGE_STATE_DIR", stateDir)

	agent := &CopilotAgent{}
	_, err := agent.getToken()
	if err == nil {
		t.Fatal("expected missing auth error")
	}
	if !strings.Contains(err.Error(), stateDir) {
		t.Fatalf("expected error to include SAGE_STATE_DIR %q, got %v", stateDir, err)
	}
	if strings.Contains(err.Error(), "set SAGE_STATE_DIR or OAuthToken") {
		t.Fatalf("expected agent to try SAGE_STATE_DIR before no-source fallback, got %v", err)
	}
}

func TestRefreshCopilotTokenFallsBackToLegacyEndpoint(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, fmt.Sprintf("%s %s", r.URL.Path, r.Header.Get("Authorization")))
		switch {
		case r.URL.Path == "/copilot_internal/v2/token":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case r.URL.Path == "/copilot_internal/token" && r.Header.Get("Authorization") == "token gho_test":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"tid=abc;proxy-ep=https://proxy.individual.githubcopilot.com;exp=1","expires_at":4102444800}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"bad auth"}`))
		}
	}))
	defer server.Close()

	t.Setenv("SAGE_GITHUB_API_BASE_URL", server.URL)
	auth, err := refreshCopilotToken("gho_test")
	if err != nil {
		t.Fatalf("refreshCopilotToken() error = %v", err)
	}
	if auth.Token == "" {
		t.Fatal("expected token from fallback endpoint")
	}
	if auth.BaseURL != "https://api.individual.githubcopilot.com" {
		t.Fatalf("unexpected base url: %q", auth.BaseURL)
	}
	if len(requests) < 4 {
		t.Fatalf("expected fallback attempts over endpoint + auth variants, got %d (%v)", len(requests), requests)
	}
}

func TestRefreshCopilotTokenReturnsAttemptSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	t.Setenv("SAGE_GITHUB_API_BASE_URL", server.URL)
	_, err := refreshCopilotToken("gho_test")
	if err == nil {
		t.Fatal("expected refreshCopilotToken() to fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "failed after") || !strings.Contains(msg, "/copilot_internal/v2/token") || !strings.Contains(msg, "/copilot_internal/token") {
		t.Fatalf("expected endpoint attempt summary in error, got %q", msg)
	}
}
