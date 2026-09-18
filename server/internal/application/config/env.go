package config

import (
	"os"
	"strings"
)

func expandEnv(content string) string {
	return os.Expand(content, func(key string) string {
		if val, ok := os.LookupEnv(key); ok {
			return strings.ReplaceAll(val, `\`, `\\`)
		}
		return ""
	})
}
