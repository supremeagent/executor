package executor

import (
	"os"
	"sort"
	"strings"
)

// BuildCommandEnv builds command environment variables from the host environment
// and applies overrides from left to right.
func BuildCommandEnv(overrides ...map[string]string) []string {
	envMap := make(map[string]string)
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}

		// Remove Anthropic environment variables
		if strings.HasPrefix(parts[0], "ANTHROPIC_") {
			continue
		}

		envMap[parts[0]] = parts[1]
	}

	for _, override := range overrides {
		for key, value := range override {
			if strings.TrimSpace(key) == "" {
				continue
			}
			envMap[key] = value
		}
	}

	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+envMap[key])
	}

	return result
}
