package domain

import (
	"os"
	"strings"
)

func EnvExpand(content string) string {
	return os.Expand(content, func(key string) string {
		if val, ok := os.LookupEnv(key); ok {
			return val
		}
		return ""
	})
}

func EnvExpandMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = EnvExpand(v)
	}
	return out
}

func EnvExpandSlice(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = EnvExpand(v)
	}
	return out
}

func EnvExpandString(s string) string {
	if s == "" {
		return s
	}
	return strings.TrimSpace(EnvExpand(s))
}
