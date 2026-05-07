package sageagents

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type copilotTokenFile struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
	UpdatedAt int64  `json:"updatedAt"`
	BaseURL   string `json:"baseUrl,omitempty"`
}

type sageGitHubAuthFile struct {
	Provider  string `json:"provider"`
	Token     string `json:"token"`
	TokenType string `json:"tokenType,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

// CopilotAuthStatus is the dashboard-safe provider state summary. It reports
// whether Sage can call Copilot without exposing raw OAuth or API tokens.
type CopilotAuthStatus struct {
	Connected          bool   `json:"connected"`
	TokenSource        string `json:"tokenSource,omitempty"`
	CachedTokenValid   bool   `json:"cachedTokenValid"`
	CachedTokenExpires int64  `json:"cachedTokenExpires,omitempty"`
	OAuthStored        bool   `json:"oauthStored"`
	EnvTokenAvailable  bool   `json:"envTokenAvailable"`
	Error              string `json:"error,omitempty"`
}

// GitHubDeviceLoginStart is returned when the dashboard starts GitHub's device
// flow. The browser displays UserCode and VerificationURI to the user.
type GitHubDeviceLoginStart struct {
	DeviceCode      string `json:"deviceCode"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}

type githubDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type CopilotRuntimeAuth struct {
	Token     string
	ExpiresAt time.Time
	BaseURL   string
}

const defaultCopilotAPIBaseURL = "https://api.individual.githubcopilot.com"
const defaultGitHubCopilotClientID = "Iv1.b507a08c87ecfe98"
const defaultGitHubAPIBaseURL = "https://api.github.com"

// GetCopilotToken returns a valid Copilot API bearer token from Sage-owned state.
func GetCopilotToken(stateDir string) (string, error) {
	auth, err := GetCopilotRuntimeAuth(stateDir)
	if err != nil {
		return "", err
	}
	return auth.Token, nil
}

// GetCopilotRuntimeAuth returns a Copilot runtime token plus the API base URL
// advertised by GitHub's Copilot token exchange.
func GetCopilotRuntimeAuth(stateDir string) (CopilotRuntimeAuth, error) {
	tokenPath := copilotTokenPath(stateDir)
	if auth, ok := readCachedCopilotRuntimeAuth(tokenPath); ok {
		return auth, nil
	}

	oauthToken, err := getGitHubOAuthToken(stateDir)
	if err != nil {
		return CopilotRuntimeAuth{}, fmt.Errorf("no valid cached token and GitHub auth unavailable: %w", err)
	}
	auth, err := refreshCopilotToken(oauthToken)
	if err != nil {
		return CopilotRuntimeAuth{}, fmt.Errorf("Copilot token refresh failed: %w", err)
	}

	writeCachedToken(tokenPath, auth)
	return auth, nil
}

func CopilotAuthState(stateDir string) CopilotAuthStatus {
	status := CopilotAuthStatus{}
	if tf, ok := readCopilotTokenFile(copilotTokenPath(stateDir)); ok {
		status.CachedTokenExpires = tf.ExpiresAt
		status.CachedTokenValid = tf.Token != "" && time.Until(time.UnixMilli(tf.ExpiresAt)) > 5*time.Minute
	}
	if token, ok := readSageGitHubAuthToken(stateDir); ok && token != "" {
		status.OAuthStored = true
	}
	if envGitHubToken() != "" {
		status.EnvTokenAvailable = true
	}
	switch {
	case status.CachedTokenValid:
		status.Connected = true
		status.TokenSource = "cache"
	case status.OAuthStored:
		status.Connected = true
		status.TokenSource = "sage-auth-store"
	case status.EnvTokenAvailable:
		status.Connected = true
		status.TokenSource = "environment"
	default:
		status.Error = "no Sage GitHub auth token, env token, or valid Copilot cache found"
	}
	return status
}

func SaveGitHubOAuthToken(stateDir, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token is required")
	}
	auth := sageGitHubAuthFile{
		Provider:  "github-copilot",
		Token:     token,
		TokenType: "oauth",
		UpdatedAt: time.Now().UnixMilli(),
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	path := gitHubAuthPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func DeleteGitHubOAuthToken(stateDir string) error {
	_ = os.Remove(copilotTokenPath(stateDir))
	err := os.Remove(gitHubAuthPath(stateDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func StartGitHubDeviceLogin(clientID string) (GitHubDeviceLoginStart, error) {
	clientID = resolveGitHubCopilotClientID(clientID)
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", "read:user")
	req, err := http.NewRequest(http.MethodPost, "https://github.com/login/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		return GitHubDeviceLoginStart{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return GitHubDeviceLoginStart{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return GitHubDeviceLoginStart{}, fmt.Errorf("device login returned %d: %s", resp.StatusCode, body)
	}
	var result githubDeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return GitHubDeviceLoginStart{}, err
	}
	return GitHubDeviceLoginStart{
		DeviceCode:      result.DeviceCode,
		UserCode:        result.UserCode,
		VerificationURI: result.VerificationURI,
		ExpiresIn:       result.ExpiresIn,
		Interval:        result.Interval,
	}, nil
}

func CompleteGitHubDeviceLogin(stateDir, clientID, deviceCode string) error {
	clientID = resolveGitHubCopilotClientID(clientID)
	deviceCode = strings.TrimSpace(deviceCode)
	if deviceCode == "" {
		return fmt.Errorf("deviceCode is required")
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	req, err := http.NewRequest(http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("device completion returned %d: %s", resp.StatusCode, body)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("%s: %s", result.Error, result.Description)
	}
	if result.AccessToken == "" {
		return fmt.Errorf("GitHub returned no access token")
	}
	return SaveGitHubOAuthToken(stateDir, result.AccessToken)
}

func getGitHubOAuthToken(stateDir string) (string, error) {
	if token, ok := readSageGitHubAuthToken(stateDir); ok {
		return token, nil
	}
	if token := envGitHubToken(); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("github-copilot token not found under %s/auth or supported env vars", stateDir)
}

func readSageGitHubAuthToken(stateDir string) (string, bool) {
	data, err := os.ReadFile(gitHubAuthPath(stateDir))
	if err != nil {
		return "", false
	}
	var auth sageGitHubAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return "", false
	}
	token := strings.TrimSpace(auth.Token)
	return token, token != ""
}

func refreshCopilotToken(oauthToken string) (CopilotRuntimeAuth, error) {
	oauthToken = strings.TrimSpace(oauthToken)
	if oauthToken == "" {
		return CopilotRuntimeAuth{}, fmt.Errorf("oauth token is empty")
	}

	type refreshAttempt struct {
		endpoint   string
		authScheme string
		statusCode int
		body       string
		err        error
	}

	attempts := make([]refreshAttempt, 0, 4)
	for _, endpoint := range copilotTokenRefreshEndpoints() {
		for _, authScheme := range []string{"Bearer", "token"} {
			req, err := http.NewRequest(http.MethodGet, endpoint, nil)
			if err != nil {
				attempts = append(attempts, refreshAttempt{endpoint: endpoint, authScheme: authScheme, err: err})
				continue
			}
			setCopilotRefreshHeaders(req, authScheme, oauthToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				attempts = append(attempts, refreshAttempt{endpoint: endpoint, authScheme: authScheme, err: err})
				continue
			}

			bodyBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			body := trimForError(bodyBytes)

			if resp.StatusCode != http.StatusOK {
				attempts = append(attempts, refreshAttempt{
					endpoint:   endpoint,
					authScheme: authScheme,
					statusCode: resp.StatusCode,
					body:       body,
				})
				continue
			}

			var result struct {
				Token     string `json:"token"`
				ExpiresAt int64  `json:"expires_at"`
			}
			if err := json.Unmarshal(bodyBytes, &result); err != nil {
				attempts = append(attempts, refreshAttempt{endpoint: endpoint, authScheme: authScheme, err: fmt.Errorf("parse token response: %w", err)})
				continue
			}
			if result.Token == "" {
				attempts = append(attempts, refreshAttempt{endpoint: endpoint, authScheme: authScheme, err: fmt.Errorf("empty token in response")})
				continue
			}

			expiresAt := time.Unix(result.ExpiresAt, 0)
			if result.ExpiresAt > 10_000_000_000 {
				expiresAt = time.UnixMilli(result.ExpiresAt)
			}
			return CopilotRuntimeAuth{
				Token:     result.Token,
				ExpiresAt: expiresAt,
				BaseURL:   deriveCopilotAPIBaseURLFromToken(result.Token),
			}, nil
		}
	}

	if len(attempts) == 0 {
		return CopilotRuntimeAuth{}, fmt.Errorf("token refresh failed without attempts")
	}

	parts := make([]string, 0, len(attempts))
	for _, item := range attempts {
		part := fmt.Sprintf("%s (%s)", item.endpoint, item.authScheme)
		if item.err != nil {
			part += ": " + item.err.Error()
		} else {
			part += fmt.Sprintf(": status %d", item.statusCode)
			if item.body != "" {
				part += ": " + item.body
			}
		}
		parts = append(parts, part)
	}
	return CopilotRuntimeAuth{}, fmt.Errorf("token refresh failed after %d attempts; %s", len(attempts), strings.Join(parts, " | "))
}

func copilotTokenRefreshEndpoints() []string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SAGE_GITHUB_API_BASE_URL")), "/")
	if base == "" {
		base = defaultGitHubAPIBaseURL
	}
	return []string{
		base + "/copilot_internal/v2/token",
		base + "/copilot_internal/token",
	}
}

func setCopilotRefreshHeaders(req *http.Request, authScheme, token string) {
	req.Header.Set("Authorization", authScheme+" "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sage-nexus/1.0")
	req.Header.Set("Editor-Version", "vscode/1.85.0")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.12.0")
	req.Header.Set("Openai-Intent", "conversation-panel")
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

func trimForError(body []byte) string {
	value := strings.TrimSpace(string(body))
	const maxLen = 400
	if len(value) > maxLen {
		return value[:maxLen] + "..."
	}
	return value
}

func writeCachedToken(path string, auth CopilotRuntimeAuth) {
	tf := copilotTokenFile{
		Token:     auth.Token,
		ExpiresAt: auth.ExpiresAt.UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
		BaseURL:   normalizeCopilotAPIBaseURL(auth.BaseURL),
	}
	data, _ := json.MarshalIndent(tf, "", "  ")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

func readCachedCopilotRuntimeAuth(path string) (CopilotRuntimeAuth, bool) {
	tf, ok := readCopilotTokenFile(path)
	if !ok || tf.Token == "" {
		return CopilotRuntimeAuth{}, false
	}
	if time.Until(time.UnixMilli(tf.ExpiresAt)) <= 5*time.Minute {
		return CopilotRuntimeAuth{}, false
	}
	return CopilotRuntimeAuth{
		Token:     tf.Token,
		ExpiresAt: time.UnixMilli(tf.ExpiresAt),
		BaseURL:   normalizeCopilotAPIBaseURL(firstNonEmpty(tf.BaseURL, deriveCopilotAPIBaseURLFromToken(tf.Token))),
	}, true
}

func readCopilotTokenFile(path string) (copilotTokenFile, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return copilotTokenFile{}, false
	}
	var tf copilotTokenFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return copilotTokenFile{}, false
	}
	return tf, true
}

func copilotTokenPath(stateDir string) string {
	return filepath.Join(stateDir, "credentials", "github-copilot.token.json")
}

func gitHubAuthPath(stateDir string) string {
	return filepath.Join(stateDir, "auth", "github-copilot.json")
}

func envGitHubToken() string {
	for _, key := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func deriveCopilotAPIBaseURLFromToken(token string) string {
	for _, part := range strings.Split(token, ";") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(strings.ToLower(part), "proxy-ep=") {
			continue
		}
		host := strings.TrimSpace(strings.TrimPrefix(part, "proxy-ep="))
		host = strings.TrimPrefix(host, "https://")
		host = strings.TrimPrefix(host, "http://")
		host = strings.TrimPrefix(host, "proxy.")
		if host == "" {
			continue
		}
		return normalizeCopilotAPIBaseURL("https://api." + host)
	}
	return defaultCopilotAPIBaseURL
}

func normalizeCopilotAPIBaseURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return defaultCopilotAPIBaseURL
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveGitHubCopilotClientID(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	if value = strings.TrimSpace(os.Getenv("GITHUB_CLIENT_ID")); value != "" {
		return value
	}
	return defaultGitHubCopilotClientID
}
