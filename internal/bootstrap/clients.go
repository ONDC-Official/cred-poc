package bootstrap

import (
	"credential-service/internal/config"
	"credential-service/internal/service/client"
)

func setupClients(cfg *config.Config) *client.DigioClient {
	return client.NewDigioClient(client.DigioConfig{
		BaseURL: cfg.Digio.BaseURL,
		Token:   cfg.Digio.Token,
		Timeout: cfg.Digio.Timeout,
	})
}
