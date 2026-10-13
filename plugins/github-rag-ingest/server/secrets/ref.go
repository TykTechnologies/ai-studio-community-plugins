package secrets

import (
	"fmt"
	"os"
	"strings"
)

// ResolveConfigRef resolves a secret-bearing config value. AI Studio 2.3
// resolves $SECRET/NAME and $ENV/NAME references in plugin config before the
// plugin starts, so a value that still looks like a reference means:
//
//   - $ENV/NAME: an older AI Studio; NAME is read from the plugin's
//     environment (on 2.3, plugins only see host variables listed in
//     PLUGIN_ENV_PASSTHROUGH).
//   - $SECRET/NAME: an older AI Studio, or a secret that does not exist;
//     the plugin cannot read AI Studio secrets itself, so this is an error.
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
