package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestParseConfig_RedisPassword(t *testing.T) {
	nested := ParseConfig(map[string]string{"config": `{"storage_backend":"redis","redis_url":"redis://host:6379","redis_password":"p1"}`})
	if nested.RedisPassword != "p1" {
		t.Errorf("nested redis_password = %q, want p1", nested.RedisPassword)
	}
	flat := ParseConfig(map[string]string{"storage_backend": "redis", "redis_url": "redis://host:6379", "redis_password": "p2"})
	if flat.RedisPassword != "p2" {
		t.Errorf("flat redis_password = %q, want p2", flat.RedisPassword)
	}
}

// redis_password overrides a password in redis_url; without it the URL's
// password is used.
func TestRedisOptionsPassword(t *testing.T) {
	opt, err := redisOptions("redis://:from-url@localhost:6379/0", "from-field")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Password != "from-field" {
		t.Errorf("password = %q, want from-field", opt.Password)
	}

	opt, err = redisOptions("redis://:from-url@localhost:6379/0", "")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Password != "from-url" {
		t.Errorf("password = %q, want from-url", opt.Password)
	}
}

// An unparseable URL is reported without the URL, which may carry a password
// (the error is logged when the plugin falls back to KV).
func TestRedisOptionsErrorOmitsURL(t *testing.T) {
	for _, u := range []string{
		"redis://:s3cr3t-pass@localhost:badport/0",
		"redis://user:s3cr3t-pass@local host:6379",
		"ftp://:s3cr3t-pass@localhost:6379",
	} {
		_, err := redisOptions(u, "")
		if err == nil {
			t.Errorf("redisOptions(%q) succeeded, want an error", u)
			continue
		}
		if strings.Contains(err.Error(), "s3cr3t-pass") {
			t.Errorf("error leaks the password: %v", err)
		}
	}
}

func TestResolveRedisPassword(t *testing.T) {
	t.Setenv("RL_TEST_REDIS_PASSWORD", "env-pass")

	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "", want: ""},
		{in: "plain", want: "plain"},
		{in: "$ENV/RL_TEST_REDIS_PASSWORD", want: "env-pass"},
		{in: "$ENV/RL_TEST_REDIS_PASSWORD_UNSET", wantErr: true},
		// A reference AI Studio did not resolve (an older Studio, a missing
		// secret, or an edge gateway, which receives the reference) is never
		// sent to Redis as the password.
		{in: "$SECRET/REDIS_PASSWORD", wantErr: true},
	}
	for _, c := range cases {
		got, err := resolveRedisPassword(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("resolveRedisPassword(%q) = %q, want an error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("resolveRedisPassword(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

var secretFieldName = regexp.MustCompile(`(?i)(password|secret|token|api_?key|private_?key|passphrase)`)

// Every secret-looking string field in the config schema is marked
// format:password, so the plugin config form masks it.
func TestConfigSchemaMasksSecretFields(t *testing.T) {
	raw, err := os.ReadFile("config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Properties["redis_password"]; !ok {
		t.Fatal("config schema has no redis_password field")
	}
	for name, prop := range schema.Properties {
		if prop["type"] == "string" && secretFieldName.MatchString(name) && prop["format"] != "password" {
			t.Errorf("%s is not format:password", name)
		}
	}
}
