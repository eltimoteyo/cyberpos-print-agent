package agent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"print-agent-go/internal/config"
)

type ServerConfig struct {
	Address         string
	Version         string
	AllowedOrigins  []string
	PairingToken    string
	SigningSecret   string
	MaxClockSkewSec int
	QueueMaxSize    int
	MaxRetries      int
	RateLimitPerMin int
}

type Server struct {
	cfg        ServerConfig
	mux        *http.ServeMux
	store      *config.Store
	mu         sync.RWMutex
	stat       agentStatus
	ticketQ    chan printTicketRequest
	processing map[string]struct{}
	processed  map[string]processedPrintJob
	rateMu     sync.Mutex
	rate       map[string]rateCounter
}

type rateCounter struct {
	WindowStart int64
	Count       int
}

type agentStatus struct {
	Connected         bool   `json:"connected"`
	Version           string `json:"version"`
	LastJobID         string `json:"lastJobId,omitempty"`
	LastSaleID        string `json:"lastSaleId,omitempty"`
	LastPrinterName   string `json:"lastPrinterName,omitempty"`
	LastPrintStatus   string `json:"lastPrintStatus,omitempty"`
	LastError         string `json:"lastError,omitempty"`
	LastPrintAt       string `json:"lastPrintAt,omitempty"`
	SuccessfulPrints  int    `json:"successfulPrints"`
	FailedPrints      int    `json:"failedPrints"`
	LastUpdatedAt     string `json:"lastUpdatedAt"`
	ConfiguredPrinter string `json:"configuredPrinter,omitempty"`
	QueueDepth        int    `json:"queueDepth"`
	QueueCapacity     int    `json:"queueCapacity"`
}

type processedPrintJob struct {
	Status      string
	PrinterName string
	Error       string
	UpdatedAt   string
}

func NewServer(cfg ServerConfig) (*Server, error) {
	store, err := config.NewStore()
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:        normalizeServerConfig(cfg),
		mux:        http.NewServeMux(),
		store:      store,
		ticketQ:    make(chan printTicketRequest, normalizeServerConfig(cfg).QueueMaxSize),
		processing: make(map[string]struct{}),
		processed:  make(map[string]processedPrintJob),
		rate:       make(map[string]rateCounter),
		stat: agentStatus{
			Connected:       true,
			Version:         cfg.Version,
			LastPrintStatus: "idle",
			LastUpdatedAt:   time.Now().UTC().Format(time.RFC3339),
			QueueCapacity:   normalizeServerConfig(cfg).QueueMaxSize,
		},
	}

	if saved, ok, err := store.Load(); err == nil && ok {
		s.stat.ConfiguredPrinter = saved.PrinterName
	}

	s.registerRoutes()
	go s.runTicketWorker()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.withLogging(s.mux)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /printers", s.handlePrinters)
	s.mux.HandleFunc("POST /config", s.handleConfigUpsert)
	s.mux.HandleFunc("GET /config", s.handleConfigGet)
	s.mux.HandleFunc("POST /print/test", s.handlePrintTest)
	s.mux.HandleFunc("POST /print/ticket", s.handlePrintTicket)
	s.mux.HandleFunc("GET /status", s.handleStatus)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "print-agent",
		"version": s.cfg.Version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handlePrinters(w http.ResponseWriter, r *http.Request) {
	printers, err := listPrinters()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list printers: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"printers": printers,
	})
}

func (s *Server) handleConfigUpsert(w http.ResponseWriter, r *http.Request) {
	if !s.requirePairingToken(w, r) {
		return
	}

	var payload config.PrinterConfig
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.Save(payload); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "config saved",
		"config":  payload,
	})

	s.mu.Lock()
	s.stat.ConfiguredPrinter = payload.PrinterName
	s.stat.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.mu.Unlock()
}

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	cfg, ok, err := s.store.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load config")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "config not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config": cfg,
	})
}

type testPrintRequest struct {
	PrinterName string `json:"printerName"`
}

func (s *Server) handlePrintTest(w http.ResponseWriter, r *http.Request) {
	if !s.requirePairingToken(w, r) {
		return
	}

	var payload testPrintRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	printerName := strings.TrimSpace(payload.PrinterName)
	if printerName == "" {
		cfg, ok, err := s.store.Load()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load config")
			return
		}
		if !ok {
			writeError(w, http.StatusBadRequest, "printerName is required when local config is missing")
			return
		}
		printerName = strings.TrimSpace(cfg.PrinterName)
		if printerName == "" {
			writeError(w, http.StatusBadRequest, "configured printerName is empty")
			return
		}
	}

	if err := sendTestPrint(printerName); err != nil {
		s.mu.Lock()
		s.stat.LastPrinterName = printerName
		s.stat.LastPrintStatus = "failed"
		s.stat.LastError = err.Error()
		s.stat.FailedPrints++
		s.stat.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339)
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("test print failed: %v", err))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	s.stat.LastPrinterName = printerName
	s.stat.LastPrintStatus = "success"
	s.stat.LastError = ""
	s.stat.LastPrintAt = now
	s.stat.SuccessfulPrints++
	s.stat.LastUpdatedAt = now
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"message":     "test print sent",
		"printerName": printerName,
	})
}

type printTicketRequest struct {
	JobID       string   `json:"jobId"`
	SaleID      string   `json:"saleId"`
	PrinterName string   `json:"printerName"`
	Title       string   `json:"title"`
	Lines       []string `json:"lines"`
	Footer      []string `json:"footer"`
	OpenDrawer  bool     `json:"openDrawer"`
	CutPaper    bool     `json:"cutPaper"`
	APIBaseURL  string   `json:"apiBaseUrl"`
	BearerToken string   `json:"bearerToken"`
}

func (s *Server) handlePrintTicket(w http.ResponseWriter, r *http.Request) {
	if !s.requirePairingToken(w, r) {
		return
	}
	if !s.requireSignedRequest(w, r) {
		return
	}

	var payload printTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	if len(payload.Lines) == 0 {
		writeError(w, http.StatusBadRequest, "lines must contain at least one line")
		return
	}

	printerName := strings.TrimSpace(payload.PrinterName)
	if printerName == "" {
		cfg, ok, err := s.store.Load()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load config")
			return
		}
		if !ok || strings.TrimSpace(cfg.PrinterName) == "" {
			writeError(w, http.StatusBadRequest, "printerName is required when local config is missing")
			return
		}
		printerName = strings.TrimSpace(cfg.PrinterName)
	}

	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = "CyberERP Ticket"
	}

	payload.PrinterName = printerName
	payload.Title = title
	payload.JobID = strings.TrimSpace(payload.JobID)
	payload.SaleID = strings.TrimSpace(payload.SaleID)

	if payload.JobID != "" {
		s.mu.RLock()
		if done, found := s.processed[payload.JobID]; found {
			s.mu.RUnlock()
			writeJSON(w, http.StatusOK, map[string]any{
				"message":     "ticket already processed",
				"jobId":       payload.JobID,
				"saleId":      payload.SaleID,
				"printerName": done.PrinterName,
				"status":      done.Status,
				"error":       done.Error,
				"duplicate":   true,
			})
			return
		}
		if _, inProgress := s.processing[payload.JobID]; inProgress {
			s.mu.RUnlock()
			writeJSON(w, http.StatusAccepted, map[string]any{
				"message":   "ticket already queued",
				"jobId":     payload.JobID,
				"saleId":    payload.SaleID,
				"duplicate": true,
			})
			return
		}
		s.mu.RUnlock()
	}

	s.mu.Lock()
	if payload.JobID != "" {
		s.processing[payload.JobID] = struct{}{}
	}
	s.stat.LastJobID = payload.JobID
	s.stat.LastSaleID = payload.SaleID
	s.stat.LastPrinterName = payload.PrinterName
	s.stat.LastPrintStatus = "queued"
	s.stat.LastError = ""
	s.stat.QueueDepth = len(s.ticketQ) + 1
	s.stat.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.mu.Unlock()

	select {
	case s.ticketQ <- payload:
		writeJSON(w, http.StatusAccepted, map[string]any{
			"message":     "ticket queued",
			"printerName": payload.PrinterName,
			"jobId":       payload.JobID,
			"saleId":      payload.SaleID,
			"openDrawer":  payload.OpenDrawer,
			"cutPaper":    payload.CutPaper,
		})
	default:
		s.mu.Lock()
		if payload.JobID != "" {
			delete(s.processing, payload.JobID)
		}
		s.stat.LastPrintStatus = "failed"
		s.stat.LastError = "print queue is full"
		s.stat.FailedPrints++
		s.stat.QueueDepth = len(s.ticketQ)
		s.stat.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339)
		s.mu.Unlock()

		writeError(w, http.StatusServiceUnavailable, "print queue is full")
	}
}

func (s *Server) runTicketWorker() {
	for payload := range s.ticketQ {
		s.processTicket(payload)
	}
}

func (s *Server) processTicket(payload printTicketRequest) {
	var lastErr error
	for attempt := 1; attempt <= s.cfg.MaxRetries; attempt++ {
		if err := sendTicketPrint(payload.PrinterName, payload.Title, payload.Lines, payload.Footer, payload.OpenDrawer, payload.CutPaper); err != nil {
			lastErr = err
			if attempt < s.cfg.MaxRetries {
				time.Sleep(time.Duration(attempt) * 400 * time.Millisecond)
			}
			continue
		}

		now := time.Now().UTC().Format(time.RFC3339)
		reportErr := reportPrintJobResult(payload.APIBaseURL, payload.BearerToken, payload.JobID, "printed", map[string]any{
			"saleId":      payload.SaleID,
			"printerName": payload.PrinterName,
			"printedAt":   now,
		})
		if reportErr != nil {
			logStructured("warn", "print_job_report_failed", map[string]any{
				"job_id":  payload.JobID,
				"sale_id": payload.SaleID,
				"error":   reportErr.Error(),
			})
		}

		s.mu.Lock()
		if payload.JobID != "" {
			delete(s.processing, payload.JobID)
			s.processed[payload.JobID] = processedPrintJob{
				Status:      "printed",
				PrinterName: payload.PrinterName,
				UpdatedAt:   now,
			}
			s.trimProcessedLocked()
		}
		s.stat.LastJobID = payload.JobID
		s.stat.LastSaleID = payload.SaleID
		s.stat.LastPrinterName = payload.PrinterName
		s.stat.LastPrintStatus = "success"
		s.stat.LastError = ""
		s.stat.LastPrintAt = now
		s.stat.SuccessfulPrints++
		s.stat.QueueDepth = len(s.ticketQ)
		s.stat.LastUpdatedAt = now
		s.mu.Unlock()
		return
	}

	errMessage := "ticket print failed"
	if lastErr != nil {
		errMessage = lastErr.Error()
	}
	reportErr := reportPrintJobResult(payload.APIBaseURL, payload.BearerToken, payload.JobID, "failed", map[string]any{
		"saleId":      payload.SaleID,
		"printerName": payload.PrinterName,
		"error":       errMessage,
	})
	if reportErr != nil {
		logStructured("warn", "print_job_report_failed", map[string]any{
			"job_id":  payload.JobID,
			"sale_id": payload.SaleID,
			"error":   reportErr.Error(),
		})
	}

	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	if payload.JobID != "" {
		delete(s.processing, payload.JobID)
		s.processed[payload.JobID] = processedPrintJob{
			Status:      "failed",
			PrinterName: payload.PrinterName,
			Error:       errMessage,
			UpdatedAt:   now,
		}
		s.trimProcessedLocked()
	}
	s.stat.LastJobID = payload.JobID
	s.stat.LastSaleID = payload.SaleID
	s.stat.LastPrinterName = payload.PrinterName
	s.stat.LastPrintStatus = "failed"
	s.stat.LastError = errMessage
	s.stat.FailedPrints++
	s.stat.QueueDepth = len(s.ticketQ)
	s.stat.LastUpdatedAt = now
	s.mu.Unlock()
}

func (s *Server) trimProcessedLocked() {
	const maxProcessedEntries = 1000
	if len(s.processed) <= maxProcessedEntries {
		return
	}

	type entry struct {
		jobID     string
		updatedAt string
	}

	items := make([]entry, 0, len(s.processed))
	for jobID, data := range s.processed {
		items = append(items, entry{jobID: jobID, updatedAt: data.UpdatedAt})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].updatedAt < items[j].updatedAt
	})

	toDelete := len(s.processed) - maxProcessedEntries
	for i := 0; i < toDelete; i++ {
		delete(s.processed, items[i].jobID)
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	stat := s.stat
	stat.QueueDepth = len(s.ticketQ)
	stat.QueueCapacity = cap(s.ticketQ)
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status": stat,
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		ip := clientIP(r.RemoteAddr)

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && !s.isOriginAllowed(origin) {
			logStructured("warn", "request_rejected_origin", map[string]any{
				"method":    r.Method,
				"path":      r.URL.Path,
				"remote_ip": ip,
				"origin":    origin,
			})
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}

		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Agent-Token,X-Agent-Timestamp,X-Agent-Signature")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			logStructured("info", "http_request", map[string]any{
				"method":      r.Method,
				"path":        r.URL.Path,
				"remote_ip":   ip,
				"status":      http.StatusNoContent,
				"duration_ms": time.Since(started).Milliseconds(),
			})
			return
		}

		if s.cfg.RateLimitPerMin > 0 && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete) {
			key := ip + "|" + r.Method + "|" + r.URL.Path
			allowed, remaining := s.allowRequest(key)
			if !allowed {
				w.Header().Set("Retry-After", "60")
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				logStructured("warn", "request_rate_limited", map[string]any{
					"method":        r.Method,
					"path":          r.URL.Path,
					"remote_ip":     ip,
					"limit_per_min": s.cfg.RateLimitPerMin,
					"remaining":     remaining,
				})
				return
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(s.cfg.RateLimitPerMin))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		}

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		logStructured("info", "http_request", map[string]any{
			"method":      r.Method,
			"path":        r.URL.Path,
			"remote_ip":   ip,
			"status":      rec.status,
			"duration_ms": time.Since(started).Milliseconds(),
		})
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) allowRequest(key string) (bool, int) {
	now := time.Now().Unix()
	windowStart := now - (now % 60)

	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	entry, ok := s.rate[key]
	if !ok || entry.WindowStart != windowStart {
		s.rate[key] = rateCounter{WindowStart: windowStart, Count: 1}
		return true, s.cfg.RateLimitPerMin - 1
	}

	if entry.Count >= s.cfg.RateLimitPerMin {
		return false, 0
	}

	entry.Count++
	s.rate[key] = entry
	return true, s.cfg.RateLimitPerMin - entry.Count
}

func clientIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}

func (s *Server) requirePairingToken(w http.ResponseWriter, r *http.Request) bool {
	if strings.TrimSpace(s.cfg.PairingToken) == "" {
		return true
	}

	provided := strings.TrimSpace(r.Header.Get("X-Agent-Token"))
	if provided == "" || provided != s.cfg.PairingToken {
		writeError(w, http.StatusUnauthorized, "invalid or missing pairing token")
		return false
	}

	return true
}

func (s *Server) isOriginAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}

	for _, allowed := range s.cfg.AllowedOrigins {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}

func normalizeServerConfig(cfg ServerConfig) ServerConfig {
	if cfg.MaxClockSkewSec <= 0 {
		cfg.MaxClockSkewSec = 300
	}
	if cfg.RateLimitPerMin <= 0 {
		cfg.RateLimitPerMin = 120
	}
	if cfg.QueueMaxSize <= 0 {
		cfg.QueueMaxSize = 200
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if len(cfg.AllowedOrigins) == 0 {
		cfg.AllowedOrigins = []string{
			"https://cyberposapp.createam.cloud",
			"http://localhost:4200",
			"http://localhost:4201",
			"http://localhost:5173",
			"http://localhost:5174",
			"http://localhost:3000",
		}
	}
	return cfg
}

func logStructured(level, event string, fields map[string]any) {
	payload := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339),
		"level": level,
		"event": event,
	}
	for k, v := range fields {
		payload[k] = v
	}

	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("{\"level\":\"error\",\"event\":\"log_marshal_failed\",\"error\":%q}", err.Error())
		return
	}
	log.Print(string(b))
}

func (s *Server) requireSignedRequest(w http.ResponseWriter, r *http.Request) bool {
	secret := strings.TrimSpace(s.cfg.SigningSecret)
	if secret == "" {
		return true
	}

	tsRaw := strings.TrimSpace(r.Header.Get("X-Agent-Timestamp"))
	sig := strings.TrimSpace(r.Header.Get("X-Agent-Signature"))
	if tsRaw == "" || sig == "" {
		writeError(w, http.StatusUnauthorized, "missing signature headers")
		return false
	}

	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid signature timestamp")
		return false
	}

	now := time.Now().Unix()
	delta := now - ts
	if delta < 0 {
		delta = -delta
	}
	if delta > int64(s.cfg.MaxClockSkewSec) {
		writeError(w, http.StatusUnauthorized, "signature timestamp out of range")
		return false
	}

	expected := computeRequestSignature(secret, tsRaw)
	provided := strings.ToLower(sig)
	if !hmac.Equal([]byte(expected), []byte(provided)) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return false
	}

	return true
}

func computeRequestSignature(secret, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	return hex.EncodeToString(mac.Sum(nil))
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]any{"error": message})
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
