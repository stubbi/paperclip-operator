package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DockerConfig represents the structure of a .dockerconfigjson secret.
type DockerConfig struct {
	Auths map[string]DockerAuth `json:"auths"`
}

// DockerAuth holds credentials for a single registry.
type DockerAuth struct {
	Auth string `json:"auth"` // base64(user:pass)
}

// Client queries OCI registries for manifest digests.
type Client struct {
	HTTP *http.Client
}

// NewClient creates a registry client. If httpClient is nil, a default with 30s timeout is used.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{HTTP: httpClient}
}

// ResolveDigest queries the registry for the current digest of repo:tag.
// dockerConfigJSON is the raw .dockerconfigjson bytes (may be nil for public repos).
func (c *Client) ResolveDigest(ctx context.Context, repo, tag string, dockerConfigJSON []byte) (string, error) {
	host, name := parseRepository(repo)
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, name, tag)

	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
	}, ", "))

	var username, password string
	if dockerConfigJSON != nil {
		username, password = extractCredentials(dockerConfigJSON, host)
	}

	resp, err := c.doWithAuth(ctx, req, host, name, username, password)
	if err != nil {
		return "", fmt.Errorf("querying registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry returned HTTP %d for %s:%s", resp.StatusCode, repo, tag)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("no Docker-Content-Digest header for %s:%s", repo, tag)
	}
	return digest, nil
}

func (c *Client) doWithAuth(ctx context.Context, req *http.Request, host, name, username, password string) (*http.Response, error) {
	// Try direct request first (works for public repos or if credentials are sufficient as basic auth)
	resp, err := c.HTTP.Do(req) //nolint:gosec // URL is constructed from user-provided registry config
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	_ = resp.Body.Close()

	// Parse WWW-Authenticate for bearer token exchange
	wwwAuth := resp.Header.Get("Www-Authenticate")
	realm, service, scope := parseBearerChallenge(wwwAuth)
	if realm == "" {
		return nil, fmt.Errorf("401 with no bearer challenge for %s", host)
	}

	// If no scope was provided, derive it from the image name
	if scope == "" {
		scope = fmt.Sprintf("repository:%s:pull", name)
	}

	token, err := c.fetchToken(ctx, realm, service, scope, username, password)
	if err != nil {
		return nil, fmt.Errorf("fetching bearer token: %w", err)
	}

	// Retry with bearer token
	retryReq := req.Clone(ctx)
	retryReq.Header.Set("Authorization", "Bearer "+token)
	return c.HTTP.Do(retryReq) //nolint:gosec // URL is constructed from user-provided registry config, not untrusted input
}

func (c *Client) fetchToken(ctx context.Context, realm, service, scope, username, password string) (string, error) {
	tokenURL := realm + "?"
	if service != "" {
		tokenURL += "service=" + service + "&"
	}
	tokenURL += "scope=" + scope

	req, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil) //nolint:gosec // realm URL from registry WWW-Authenticate header
	if err != nil {
		return "", err
	}
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := c.HTTP.Do(req) //nolint:gosec // token URL from registry WWW-Authenticate header
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned HTTP %d", resp.StatusCode)
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	token := tokenResp.Token
	if token == "" {
		token = tokenResp.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("empty token from %s", realm)
	}
	return token, nil
}

// parseRepository splits "ghcr.io/org/repo" into host and name.
// Handles Docker Hub shorthand (no host = docker.io, library/ prefix for single-segment names).
func parseRepository(repo string) (host, name string) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 1 || (!strings.Contains(parts[0], ".") && !strings.Contains(parts[0], ":")) {
		// No host prefix — Docker Hub
		host = "registry-1.docker.io"
		name = repo
		if !strings.Contains(name, "/") {
			name = "library/" + name
		}
		return
	}
	host = parts[0]
	name = parts[1]
	return
}

// parseBearerChallenge extracts realm, service, and scope from a WWW-Authenticate header.
// Example: Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:org/repo:pull"
func parseBearerChallenge(header string) (realm, service, scope string) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", "", ""
	}
	params := header[7:]
	for _, part := range splitChallengeParams(params) {
		k, v := splitKeyValue(part)
		switch k {
		case "realm":
			realm = v
		case "service":
			service = v
		case "scope":
			scope = v
		}
	}
	return
}

func splitChallengeParams(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	for _, ch := range s {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ',' && !inQuote:
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

func splitKeyValue(s string) (string, string) {
	idx := strings.IndexByte(s, '=')
	if idx < 0 {
		return s, ""
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:])
}

func extractCredentials(dockerConfigJSON []byte, host string) (string, string) {
	var cfg DockerConfig
	if err := json.Unmarshal(dockerConfigJSON, &cfg); err != nil {
		return "", ""
	}
	// Try exact match first, then with https:// prefix
	auth, ok := cfg.Auths[host]
	if !ok {
		auth, ok = cfg.Auths["https://"+host]
	}
	if !ok {
		return "", ""
	}
	decoded, err := base64.StdEncoding.DecodeString(auth.Auth)
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
