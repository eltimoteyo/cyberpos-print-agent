package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClientConfig holds the configuration for the gateway WebSocket client.
type WSClientConfig struct {
	GatewayWSURL string
	AgentID      string
	Token        string
	Version      string
	Hostname     string
	Capabilities []string
	DataDir      string
}

// WSJobPayload mirrors the gateway message for a print job delivered over WS.
type WSJobPayload struct {
	JobID        string         `json:"job_id"`
	SaleID       string         `json:"sale_id,omitempty"`
	PrinterName  string         `json:"printer_name,omitempty"`
	Title        string         `json:"title"`
	Lines        []string       `json:"lines"`
	Footer       []string       `json:"footer,omitempty"`
	OpenDrawer   bool           `json:"open_drawer"`
	CutPaper     bool           `json:"cut_paper"`
	APIBaseURL   string         `json:"api_base_url"`
	BearerToken  string         `json:"bearer_token"`
	TargetFormat string         `json:"target_format,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
}

// WSJobResultPayload is sent back to the gateway after processing a job.
type WSJobResultPayload struct {
	JobID        string         `json:"job_id"`
	Status       string         `json:"status"`
	ErrorMessage string         `json:"error_message,omitempty"`
	Result       map[string]any `json:"result,omitempty"`
	PrintedAt    *int64         `json:"printed_at,omitempty"`
}

// WSMessage is the envelope used by the gateway WebSocket channel.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// WSClient maintains a persistent WebSocket connection to the api-gateway.
type WSClient struct {
	cfg      WSClientConfig
	srv      *Server
	conn     *websocket.Conn
	mu       sync.Mutex
	stopCh   <-chan struct{}
	send     chan []byte
	agentID  string
}

// NewWSClient creates a gateway WebSocket client.
func NewWSClient(cfg WSClientConfig, srv *Server) *WSClient {
	return &WSClient{
		cfg:     cfg,
		srv:     srv,
		send:    make(chan []byte, 256),
		agentID: cfg.AgentID,
	}
}

// Run starts the connection/reconnection loop until stopCh is closed.
func (c *WSClient) Run(stopCh <-chan struct{}) {
	c.stopCh = stopCh

	// Register this client as the WS reporter on the server so printed/failed
	// results are forwarded to the gateway.
	c.srv.SetWSReporter(c.reportResult)

	// Ensure we have a stable agent ID and expose it via /status
	c.agentID = c.ensureAgentID()
	c.srv.SetAgentID(c.agentID)

	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-stopCh:
			c.closeConnection()
			return
		default:
		}

		if err := c.connect(); err != nil {
			log.Printf("[ws] connection failed: %v; retrying in %v", err, backoff)
			select {
			case <-stopCh:
				return
			case <-time.After(backoff):
				backoff = nextBackoff(backoff, maxBackoff)
				continue
			}
		}

		backoff = 1 * time.Second
		log.Printf("[ws] connected to gateway as agent %s", c.agentID)

		// Reset on disconnect
		done := make(chan struct{})
		go c.writePump(done)

		if err := c.readPump(); err != nil {
			log.Printf("[ws] connection lost: %v", err)
		}
		close(done)
		c.closeConnection()

		select {
		case <-stopCh:
			return
		case <-time.After(backoff):
			backoff = nextBackoff(backoff, maxBackoff)
		}
	}
}

func (c *WSClient) connect() error {
	u, err := url.Parse(c.cfg.GatewayWSURL)
	if err != nil {
		return fmt.Errorf("invalid gateway ws url: %w", err)
	}

	q := u.Query()
	q.Set("token", c.cfg.Token)
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Send register message
	hostname := c.cfg.Hostname
	if hostname == "" {
		hostname, _ = os.Hostname()
	}

	reg := WSMessage{
		Type: "register",
		Payload: mustRawJSON(map[string]any{
			"agent_id":     c.agentID,
			"hostname":     hostname,
			"version":      c.cfg.Version,
			"capabilities": normalizeStringSlice(c.cfg.Capabilities),
			"printers":     c.srv.printerInfos(),
		}),
	}

	if err := c.conn.WriteJSON(reg); err != nil {
		_ = conn.Close()
		return fmt.Errorf("register failed: %w", err)
	}

	return nil
}

func (c *WSClient) closeConnection() {
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()
}

func (c *WSClient) readPump() error {
	for {
		var msg WSMessage
		if err := c.conn.ReadJSON(&msg); err != nil {
			return err
		}

		switch strings.ToLower(strings.TrimSpace(msg.Type)) {
		case "job":
			c.handleJob(msg.Payload)
		case "ping":
			c.send <- mustJSONBytes(WSMessage{Type: "pong"})
		case "registered":
			log.Printf("[ws] registered with gateway")
		case "error":
			log.Printf("[ws] gateway error: %s", string(msg.Payload))
		default:
			log.Printf("[ws] unknown message type: %s", msg.Type)
		}
	}
}

func (c *WSClient) writePump(done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case msg := <-c.send:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("[ws] write error: %v", err)
				return
			}
		case <-ticker.C:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *WSClient) handleJob(raw json.RawMessage) {
	var payload WSJobPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("[ws] invalid job payload: %v", err)
		return
	}

	format := strings.ToLower(strings.TrimSpace(payload.TargetFormat))
	if format == "" {
		format = "ticket"
	}

	switch format {
	case "test":
		printerName := payload.PrinterName
		if printerName == "" {
			if pn, ok := payload.Payload["printer_name"].(string); ok {
				printerName = pn
			}
		}
		go c.srv.RunTestJob(payload.JobID, printerName)
		return

	case "ticket":
		// handled below

	default:
		log.Printf("[ws] job format %q not yet supported by this agent", format)
		c.reportResult(WSJobResultPayload{
			JobID:        payload.JobID,
			Status:       "failed",
			ErrorMessage: fmt.Sprintf("formato %s aún no soportado", format),
		})
		return
	}

	// Convert to the internal ticket format and enqueue on the shared server queue.
	req := printTicketRequest{
		JobID:       payload.JobID,
		SaleID:      payload.SaleID,
		PrinterName: payload.PrinterName,
		Title:       payload.Title,
		Lines:       payload.Lines,
		Footer:      payload.Footer,
		OpenDrawer:  payload.OpenDrawer,
		CutPaper:    payload.CutPaper,
		APIBaseURL:  payload.APIBaseURL,
		BearerToken: payload.BearerToken,
	}

	if err := c.srv.EnqueueRemoteJob(req); err != nil {
		log.Printf("[ws] failed to enqueue remote job %s: %v", payload.JobID, err)
		c.reportResult(WSJobResultPayload{
			JobID:        payload.JobID,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
	}
}

func (c *WSClient) reportResult(result WSJobResultPayload) error {
	msg := WSMessage{
		Type:    "job_result",
		Payload: mustRawJSON(result),
	}

	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("not connected to gateway")
	}

	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(msg)
}

// ensureAgentID loads or generates and persists a unique agent ID.
func (c *WSClient) ensureAgentID() string {
	if c.cfg.AgentID != "" {
		return c.cfg.AgentID
	}

	dataDir := c.cfg.DataDir
	if dataDir == "" {
		dataDir = defaultDataDir()
	}
	_ = os.MkdirAll(dataDir, 0o755)

	idPath := filepath.Join(dataDir, "agent.json")
	if data, err := os.ReadFile(idPath); err == nil {
		var meta struct {
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal(data, &meta); err == nil && strings.TrimSpace(meta.AgentID) != "" {
			return meta.AgentID
		}
	}

	id := generateAgentID()
	data, _ := json.MarshalIndent(map[string]string{"agent_id": id}, "", "  ")
	_ = os.WriteFile(idPath, data, 0o644)
	return id
}

func defaultDataDir() string {
	if pd := os.Getenv("PROGRAMDATA"); pd != "" {
		return filepath.Join(pd, "CyberERP", "PrintAgent")
	}
	baseDir, _ := os.UserConfigDir()
	return filepath.Join(baseDir, "cybererp", "print-agent")
}

func generateAgentID() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "agent"
	}
	timestamp := time.Now().UnixNano()
	rnd := rand.Intn(10000)
	return fmt.Sprintf("%s-%d-%04d", hostname, timestamp, rnd)
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func normalizeStringSlice(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func mustJSONBytes(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func mustRawJSON(v interface{}) json.RawMessage {
	return json.RawMessage(mustJSONBytes(v))
}
