package config

import "github.com/makifbaysal/tasktrooper/server/internal/domain"

func expandEnv(content string) string {
	return domain.EnvExpand(content)
}
