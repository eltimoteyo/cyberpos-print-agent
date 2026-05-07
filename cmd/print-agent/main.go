package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"print-agent-go/internal/agent"
)

func main() {
	cfg := agent.ServerConfig{
		Address:        getEnv("PRINT_AGENT_ADDR", "127.0.0.1:12345"),
		Version:        getEnv("PRINT_AGENT_VERSION", "0.1.0"),
		AllowedOrigins: getStringSliceEnv("PRINT_AGENT_ALLOWED_ORIGINS", "https://cyberposapp.createam.cloud,http://localhost:4200,http://localhost:4201,http://localhost:5173,http://localhost:5174,http://localhost:3000"),
		PairingToken:   getEnv("PRINT_AGENT_PAIRING_TOKEN", ""),
		SigningSecret:  getEnv("PRINT_AGENT_SIGNING_SECRET", ""),
		MaxClockSkewSec: getIntEnv("PRINT_AGENT_MAX_CLOCK_SKEW_SEC", 300),
		RateLimitPerMin: getIntEnv("PRINT_AGENT_RATE_LIMIT_PER_MIN", 120),
		QueueMaxSize:   getIntEnv("PRINT_AGENT_QUEUE_SIZE", 200),
		MaxRetries:     getIntEnv("PRINT_AGENT_MAX_RETRIES", 3),
	}

	srv, err := agent.NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to initialize print agent: %v", err)
	}

	log.Printf("print-agent listening on http://%s", cfg.Address)
	if err := http.ListenAndServe(cfg.Address, srv.Handler()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getEnv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return v
}

func getStringSliceEnv(key, fallback string) []string {
	raw := strings.TrimSpace(getEnv(key, fallback))
	if raw == "" {
		return []string{}
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}

	return out
}
