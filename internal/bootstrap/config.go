package bootstrap

import "credential-service/internal/config"

func loadConfig() (*config.Config, error) {
	return config.Load()
}
