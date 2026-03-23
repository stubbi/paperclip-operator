package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveDigest_Anonymous(t *testing.T) {
	expectedDigest := "sha256:abc123def456"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/org/repo/manifests/latest" {
			w.Header().Set("Docker-Content-Digest", expectedDigest)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	// Strip https:// to get the host
	host := strings.TrimPrefix(srv.URL, "https://")

	digest, err := client.ResolveDigest(context.Background(), host+"/org/repo", "latest", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest != expectedDigest {
		t.Errorf("expected digest %q, got %q", expectedDigest, digest)
	}
}

func TestResolveDigest_BearerAuth(t *testing.T) {
	expectedDigest := "sha256:bearer123"

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/org/repo/manifests/v1.0":
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				w.Header().Set("Www-Authenticate",
					fmt.Sprintf(`Bearer realm="%s/token",service="test-registry",scope="repository:org/repo:pull"`,
						"https://"+r.Host))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Docker-Content-Digest", expectedDigest)
			w.WriteHeader(http.StatusOK)
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-token"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	host := strings.TrimPrefix(srv.URL, "https://")

	digest, err := client.ResolveDigest(context.Background(), host+"/org/repo", "v1.0", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest != expectedDigest {
		t.Errorf("expected digest %q, got %q", expectedDigest, digest)
	}
}

func TestResolveDigest_WithCredentials(t *testing.T) {
	expectedDigest := "sha256:private456"
	var receivedBasicAuth string

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/private/repo/manifests/latest":
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				w.Header().Set("Www-Authenticate",
					fmt.Sprintf(`Bearer realm="%s/token",service="test"`, "https://"+r.Host))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Docker-Content-Digest", expectedDigest)
			w.WriteHeader(http.StatusOK)
		case "/token":
			receivedBasicAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "authed-token"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	dockerConfig := DockerConfig{
		Auths: map[string]DockerAuth{
			host: {Auth: base64.StdEncoding.EncodeToString([]byte("user:pass"))},
		},
	}
	configJSON, _ := json.Marshal(dockerConfig)

	client := NewClient(srv.Client())
	digest, err := client.ResolveDigest(context.Background(), host+"/private/repo", "latest", configJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest != expectedDigest {
		t.Errorf("expected digest %q, got %q", expectedDigest, digest)
	}
	if receivedBasicAuth == "" {
		t.Error("expected basic auth to be forwarded to token endpoint")
	}
}

func TestResolveDigest_NotFound(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	host := strings.TrimPrefix(srv.URL, "https://")

	_, err := client.ResolveDigest(context.Background(), host+"/org/repo", "missing", nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestResolveDigest_NoDigestHeader(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // 200 but no digest header
	}))
	defer srv.Close()

	client := NewClient(srv.Client())
	host := strings.TrimPrefix(srv.URL, "https://")

	_, err := client.ResolveDigest(context.Background(), host+"/org/repo", "latest", nil)
	if err == nil {
		t.Fatal("expected error for missing digest header")
	}
}

func TestParseRepository(t *testing.T) {
	tests := []struct {
		input        string
		expectedHost string
		expectedName string
	}{
		{"ghcr.io/org/repo", "ghcr.io", "org/repo"},
		{"docker.io/library/nginx", "docker.io", "library/nginx"},
		{"nginx", "registry-1.docker.io", "library/nginx"},
		{"myorg/myrepo", "registry-1.docker.io", "myorg/myrepo"},
		{"registry.example.com:5000/my/image", "registry.example.com:5000", "my/image"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, name := parseRepository(tt.input)
			if host != tt.expectedHost {
				t.Errorf("expected host %q, got %q", tt.expectedHost, host)
			}
			if name != tt.expectedName {
				t.Errorf("expected name %q, got %q", tt.expectedName, name)
			}
		})
	}
}

func TestParseBearerChallenge(t *testing.T) {
	tests := []struct {
		header  string
		realm   string
		service string
		scope   string
	}{
		{
			`Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:org/repo:pull"`,
			"https://ghcr.io/token", "ghcr.io", "repository:org/repo:pull",
		},
		{
			`Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`,
			"https://auth.docker.io/token", "registry.docker.io", "",
		},
		{`Basic realm="test"`, "", "", ""},
		{"", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			realm, service, scope := parseBearerChallenge(tt.header)
			if realm != tt.realm {
				t.Errorf("realm: expected %q, got %q", tt.realm, realm)
			}
			if service != tt.service {
				t.Errorf("service: expected %q, got %q", tt.service, service)
			}
			if scope != tt.scope {
				t.Errorf("scope: expected %q, got %q", tt.scope, scope)
			}
		})
	}
}
