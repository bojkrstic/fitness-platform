package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	DBURL             string
	Port              string
	SeedAdminEmail    string
	SeedAdminPassword string
	WebRTCICEServers  []ICEServer
}

type ICEServer struct {
	URLs       any    `json:"urls"`
	Username   string `json:"username,omitempty"`
	Credential string `json:"credential,omitempty"`
}

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}

	iceServers, err := loadICEServers()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DBURL:             strings.TrimSpace(os.Getenv("DATABASE_URL")),
		Port:              envOrDefault("PORT", "8080"),
		SeedAdminEmail:    envOrDefault("SEED_ADMIN_EMAIL", "admin@fitness.local"),
		SeedAdminPassword: envOrDefault("SEED_ADMIN_PASSWORD", "Admin123!"),
		WebRTCICEServers:  iceServers,
	}

	if cfg.DBURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func loadICEServers() ([]ICEServer, error) {
	raw := strings.TrimSpace(os.Getenv("WEBRTC_ICE_SERVERS"))
	if raw == "" {
		return defaultICEServers()
	}

	var servers []ICEServer
	if err := json.Unmarshal([]byte(raw), &servers); err != nil {
		return nil, fmt.Errorf("WEBRTC_ICE_SERVERS must be a JSON array: %w", err)
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("WEBRTC_ICE_SERVERS must contain at least one ICE server")
	}
	for i, server := range servers {
		switch urls := server.URLs.(type) {
		case string:
			if strings.TrimSpace(urls) == "" {
				return nil, fmt.Errorf("WEBRTC_ICE_SERVERS[%d].urls is required", i)
			}
		case []any:
			if len(urls) == 0 {
				return nil, fmt.Errorf("WEBRTC_ICE_SERVERS[%d].urls is required", i)
			}
			for j, urlValue := range urls {
				urlString, ok := urlValue.(string)
				if !ok || strings.TrimSpace(urlString) == "" {
					return nil, fmt.Errorf("WEBRTC_ICE_SERVERS[%d].urls[%d] must be a non-empty string", i, j)
				}
			}
		default:
			return nil, fmt.Errorf("WEBRTC_ICE_SERVERS[%d].urls must be a string or array of strings", i)
		}
	}

	return servers, nil
}

func defaultICEServers() ([]ICEServer, error) {
	servers := []ICEServer{{URLs: "stun:stun.l.google.com:19302"}}

	turnHost := strings.TrimSpace(os.Getenv("WEBRTC_TURN_HOST"))
	if turnHost == "" {
		return servers, nil
	}

	turnUsername := strings.TrimSpace(os.Getenv("WEBRTC_TURN_USERNAME"))
	turnPassword := strings.TrimSpace(os.Getenv("WEBRTC_TURN_PASSWORD"))
	if turnUsername == "" || turnPassword == "" {
		return nil, fmt.Errorf("WEBRTC_TURN_USERNAME and WEBRTC_TURN_PASSWORD are required when WEBRTC_TURN_HOST is set")
	}

	turnPort := envOrDefault("WEBRTC_TURN_PORT", "3478")
	servers = append(servers, ICEServer{
		URLs: []string{
			fmt.Sprintf("turn:%s:%s?transport=udp", turnHost, turnPort),
			fmt.Sprintf("turn:%s:%s?transport=tcp", turnHost, turnPort),
		},
		Username:   turnUsername,
		Credential: turnPassword,
	})

	return servers, nil
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
