package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	DBURL             string
	Port              string
	SeedAdminEmail    string
	SeedAdminPassword string
}

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}

	cfg := Config{
		DBURL:             strings.TrimSpace(os.Getenv("DATABASE_URL")),
		Port:              envOrDefault("PORT", "8080"),
		SeedAdminEmail:    envOrDefault("SEED_ADMIN_EMAIL", "admin@fitness.local"),
		SeedAdminPassword: envOrDefault("SEED_ADMIN_PASSWORD", "Admin123!"),
	}

	if cfg.DBURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func loadDotEnv() error {
	file, err := os.Open(".env")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}

		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}

	return scanner.Err()
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}
