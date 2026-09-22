package llmprovider

import (
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const defaultTimeoutSeconds = 300

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func resolveTimeoutSeconds(requestSeconds int, storedSeconds int, providerType domain.LLMProviderType) int {
	if requestSeconds > 0 {
		return requestSeconds
	}
	if storedSeconds > 0 {
		return storedSeconds
	}
	if def, ok := domain.LLMProviderDefinitionFor(providerType); ok && def.DefaultTimeoutSeconds > 0 {
		return def.DefaultTimeoutSeconds
	}
	return defaultTimeoutSeconds
}

func ResolveTimeoutDuration(seconds int, providerType domain.LLMProviderType, globalDefault time.Duration) time.Duration {
	resolved := resolveTimeoutDuration(seconds, providerType, globalDefault)
	if globalDefault > 0 && resolved < globalDefault {
		return globalDefault
	}
	return resolved
}

func resolveTimeoutDuration(seconds int, providerType domain.LLMProviderType, globalDefault time.Duration) time.Duration {
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if def, ok := domain.LLMProviderDefinitionFor(providerType); ok && def.DefaultTimeoutSeconds > 0 {
		return time.Duration(def.DefaultTimeoutSeconds) * time.Second
	}
	if globalDefault > 0 {
		return globalDefault
	}
	return defaultTimeoutSeconds * time.Second
}
