package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"print-agent-go/internal/agent"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "0.1.0"

func main() {
	// Subcommands handled before env loading so they can run without agent.env.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			selfInstall(os.Args[2:])
			return
		case "uninstall":
			selfUninstall()
			return
		}
	}

	// Load optional agent.env from the executable directory so Windows services
	// and scripted installs can configure the process without registry tweaks.
	loadAgentEnvFile()

	if runAsService() {
		runService("CyberERPPrintAgent", false)
		return
	}

	// Console / scheduled-task mode (backwards compatibility)
	runAgent(nil)
}

// loadAgentEnvFile loads key=value pairs from agent.env located next to the
// executable. Values already present in the environment are not overwritten.
func loadAgentEnvFile() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	envPath := filepath.Join(filepath.Dir(exePath), "agent.env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}

func runAgent(stopCh <-chan struct{}) {
	cfg := buildServerConfig()

	srv, err := agent.NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to initialize print agent: %v", err)
	}

	// Start WebSocket client if configured
	wsCfg := buildWSClientConfig()
	if wsCfg.GatewayWSURL != "" {
		wsClient := agent.NewWSClient(wsCfg, srv)
		go wsClient.Run(stopCh)
	}

	log.Printf("print-agent listening on http://%s", cfg.Address)

	server := &http.Server{
		Addr:    cfg.Address,
		Handler: srv.Handler(),
	}

	// Graceful shutdown when stopCh is closed (service mode)
	if stopCh != nil {
		go func() {
			<-stopCh
			log.Println("print-agent stopping...")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		}()
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func buildServerConfig() agent.ServerConfig {
	mandatoryOrigins := []string{
		"https://cyberposapp.createam.cloud",
		"http://localhost:4200",
		"http://localhost:4201",
		"http://localhost:5173",
		"http://localhost:5174",
		"http://localhost:3000",
	}

	return agent.ServerConfig{
		Address:         getEnv("PRINT_AGENT_ADDR", "127.0.0.1:12345"),
		Version:         getEnv("PRINT_AGENT_VERSION", Version),
		AllowedOrigins:  withMandatoryOrigins(getStringSliceEnv("PRINT_AGENT_ALLOWED_ORIGINS", ""), mandatoryOrigins),
		PairingToken:    getEnv("PRINT_AGENT_PAIRING_TOKEN", ""),
		SigningSecret:   getEnv("PRINT_AGENT_SIGNING_SECRET", ""),
		MaxClockSkewSec: getIntEnv("PRINT_AGENT_MAX_CLOCK_SKEW_SEC", 300),
		RateLimitPerMin: getIntEnv("PRINT_AGENT_RATE_LIMIT_PER_MIN", 120),
		QueueMaxSize:    getIntEnv("PRINT_AGENT_QUEUE_SIZE", 200),
		MaxRetries:      getIntEnv("PRINT_AGENT_MAX_RETRIES", 3),
		DataDir:         getEnv("PRINT_AGENT_DATA_DIR", ""),
	}
}

func buildWSClientConfig() agent.WSClientConfig {
	return agent.WSClientConfig{
		GatewayWSURL: getEnv("PRINT_AGENT_GATEWAY_WS_URL", ""),
		AgentID:      getEnv("PRINT_AGENT_ID", ""),
		Token:        getEnv("PRINT_AGENT_TOKEN", ""),
		Version:      getEnv("PRINT_AGENT_VERSION", "0.1.0"),
		Hostname:     getEnv("PRINT_AGENT_HOSTNAME", ""),
		Capabilities: getStringSliceEnv("PRINT_AGENT_CAPABILITIES", "escpos"),
		DataDir:      getEnv("PRINT_AGENT_DATA_DIR", ""),
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

func withMandatoryOrigins(current []string, mandatory []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(current)+len(mandatory))

	appendUnique := func(v string) {
		normalized := strings.TrimSpace(strings.ToLower(v))
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, strings.TrimSpace(v))
	}

	for _, v := range current {
		appendUnique(v)
	}
	for _, v := range mandatory {
		appendUnique(v)
	}

	return out
}
