package git

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	cryptossh "golang.org/x/crypto/ssh"
)

// The known_hosts_path plugin option beats GITHUB_RAG_KNOWN_HOSTS, which is
// used when the option is unset. On AI Studio 2.3 the option is the only
// source that needs no PLUGIN_ENV_PASSTHROUGH.
func TestKnownHostsPrecedence(t *testing.T) {
	configHost, envHost := generateHostKey(t), generateHostKey(t)
	dir := t.TempDir()
	write := func(name string, key cryptossh.Signer) string {
		p := filepath.Join(dir, name)
		line := "git.example.com " + string(cryptossh.MarshalAuthorizedKey(key.PublicKey()))
		if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	configFile, envFile := write("config_known_hosts", configHost), write("env_known_hosts", envHost)
	t.Setenv("GITHUB_RAG_KNOWN_HOSTS", envFile)
	t.Setenv("SSH_KNOWN_HOSTS", filepath.Join(dir, "missing"))

	addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 22}
	for _, tc := range []struct {
		name       string
		configured []string
		trusted    cryptossh.Signer
		untrusted  cryptossh.Signer
	}{
		{"config beats env", []string{configFile}, configHost, envHost},
		{"env when config unset", nil, envHost, configHost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			callback, err := knownHostsCallback(tc.configured)
			if err != nil {
				t.Fatal(err)
			}
			if err := callback("git.example.com:22", addr, tc.trusted.PublicKey()); err != nil {
				t.Errorf("expected host key accepted: %v", err)
			}
			if err := callback("git.example.com:22", addr, tc.untrusted.PublicKey()); err == nil {
				t.Error("host key from the other source was accepted")
			}
		})
	}

	// GetAuthMethod passes the configured paths through
	if _, err := GetAuthMethod(sshSecret(t), filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing configured known_hosts file did not fail closed")
	}
}
