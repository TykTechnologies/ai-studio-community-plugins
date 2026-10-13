package secrets

import (
	"fmt"
	"os"
	"strings"
)

// ResolveConfigRef resolves a secret-bearing config value:
//
//   - $SECRET/NAME is resolved by AI Studio 2.3 and later before the plugin
//     starts, so one still present here means an older AI Studio or a secret
//     that does not exist. The plugin cannot read AI Studio secrets itself,
//     so this is an error.
//   - $ENV/NAME is never resolved by AI Studio (its environment holds
//     platform secrets); NAME is read from the plugin's own environment. On
//     AI Studio 2.3 and later that only holds the default allowlist and the
//     variables listed in PLUGIN_ENV_PASSTHROUGH.
//
// Any other value is returned unchanged.
func ResolveConfigRef(field, value string) (string, error) {
	switch {
	case strings.HasPrefix(value, "$ENV/"):
		name := strings.TrimPrefix(value, "$ENV/")
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			return "", fmt.Errorf("%s: environment variable %s referenced by %s is not set (on AI Studio 2.3+, pass it to plugins with PLUGIN_ENV_PASSTHROUGH)", field, name, value)
		}
		return v, nil
	case strings.HasPrefix(value, "$SECRET/"):
		return "", fmt.Errorf("%s: secret reference %s was not resolved; it needs AI Studio 2.3 or later and an existing secret of that name", field, value)
	default:
		return value, nil
	}
}
