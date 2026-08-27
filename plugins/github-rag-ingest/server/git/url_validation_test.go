package git

import (
	"strings"
	"testing"
)

func TestValidateRepoURL_RejectsDisallowedSchemes(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"plain http", "http://example.com/org/repo.git"},
		{"git protocol", "git://example.com/org/repo.git"},
		{"file scheme", "file:///etc/passwd"},
		{"local path", "/var/lib/repos/repo.git"},
		{"relative path", "../repo"},
		{"ftp scheme", "ftp://example.com/repo.git"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRepoURL(tc.url); err == nil {
				t.Errorf("expected %q to be rejected", tc.url)
			}
		})
	}
}

func TestValidateRepoURL_RejectsInternalHosts(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"loopback https", "https://127.0.0.1/org/repo.git"},
		{"loopback with port", "https://127.0.0.1:8080/internal/api"},
		{"localhost", "https://localhost/org/repo.git"},
		{"private 10.x", "https://10.0.0.5/org/repo.git"},
		{"private 192.168.x", "https://192.168.1.10/org/repo.git"},
		{"private 172.16.x", "https://172.16.0.1/org/repo.git"},
		{"cloud metadata", "https://169.254.169.254/latest/meta-data/"},
		{"ipv6 loopback", "https://[::1]/org/repo.git"},
		{"ssh to internal", "ssh://git@10.0.0.5/org/repo.git"},
		{"scp-like to internal", "git@192.168.1.10:org/repo.git"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRepoURL(tc.url)
			if err == nil {
				t.Errorf("expected %q to be rejected as internal", tc.url)
				return
			}
			if !strings.Contains(err.Error(), "internal") && !strings.Contains(err.Error(), "scheme") {
				t.Errorf("error for %q should mention internal network, got: %v", tc.url, err)
			}
		})
	}
}

func TestValidateRepoURL_AllowsPublicRepoURLs(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"https public IP", "https://8.8.8.8/org/repo.git"},
		{"ssh public IP", "ssh://git@8.8.8.8/org/repo.git"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRepoURL(tc.url); err != nil {
				t.Errorf("expected %q to be allowed, got: %v", tc.url, err)
			}
		})
	}
}

func TestValidateRepoURL_DevelopmentBypass(t *testing.T) {
	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "true")

	// Internal hosts allowed with the documented development bypass...
	if err := ValidateRepoURL("https://127.0.0.1/org/repo.git"); err != nil {
		t.Errorf("expected bypass to allow internal host, got: %v", err)
	}
	// ...but scheme validation still applies.
	if err := ValidateRepoURL("file:///etc/passwd"); err == nil {
		t.Error("expected file:// to be rejected even with bypass enabled")
	}
}
