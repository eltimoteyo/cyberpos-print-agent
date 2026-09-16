package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSConnectSendsTokenOnlyInAuthorizationHeader(t *testing.T) {
	for _, query := range []string{"?mode=agent", "?mode=agent&token=old-secret"} {
		t.Run(query, func(t *testing.T) {
			type handshake struct {
				authorization string
				hasQueryToken bool
				mode          string
			}
			requests := make(chan handshake, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- handshake{r.Header.Get("Authorization"), r.URL.Query().Has("token"), r.URL.Query().Get("mode")}
				// Basta observar el handshake: no enumerar impresoras reales del equipo.
				http.Error(w, "test handshake", http.StatusForbidden)
			}))
			defer server.Close()
			client := NewWSClient(WSClientConfig{
				GatewayWSURL: "ws" + strings.TrimPrefix(server.URL, "http") + query,
				Token:        "test-agent-token",
			}, &Server{})
			if err := client.connect(); err == nil {
				t.Fatal("el servidor simulado debía cerrar el handshake")
			}
			select {
			case request := <-requests:
				if request.authorization != "Bearer test-agent-token" || request.hasQueryToken || request.mode != "agent" {
					t.Fatalf("handshake incorrecto: authorization correcta=%t, token en URL=%t, mode=%q", request.authorization == "Bearer test-agent-token", request.hasQueryToken, request.mode)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("el handshake no llegó al servidor")
			}
		})
	}
}

func TestWSRegistrationAdoptsIdentityAndSerializesResults(t *testing.T) {
	const resultCount = 100
	messages := make(chan WSMessage, resultCount+1)
	serverErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		for _, msg := range []WSMessage{
			{Type: "registered", Payload: mustRawJSON(map[string]string{"agent_id": "agent-canonical"})},
			{Type: "ping"},
		} {
			if err := conn.WriteJSON(msg); err != nil {
				serverErrors <- err
				return
			}
		}
		for i := 0; i < resultCount+1; i++ {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				serverErrors <- err
				return
			}
			messages <- msg
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	local := &Server{}
	dataDir := t.TempDir()
	client := NewWSClient(WSClientConfig{AgentID: "local-old-id", DataDir: dataDir}, local)
	client.conn = conn
	done, writerDone, readerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(writerDone)
		client.writePump(done)
	}()
	go func() {
		defer close(readerDone)
		_ = client.readPump()
	}()
	t.Cleanup(func() {
		close(done)
		client.closeConnection()
		for _, finished := range []<-chan struct{}{writerDone, readerDone} {
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Error("el ciclo websocket no terminó")
			}
		}
	})

	receive := func() WSMessage {
		t.Helper()
		select {
		case msg := <-messages:
			return msg
		case err := <-serverErrors:
			t.Fatalf("gateway simulado: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("sin respuesta websocket")
		}
		return WSMessage{}
	}
	// El pong confirma que readPump terminó de procesar y persistir el ack previo.
	if got := receive().Type; got != "pong" {
		t.Fatalf("tipo recibido = %q; esperado pong", got)
	}
	if got := client.currentAgentID(); got != "agent-canonical" {
		t.Fatalf("identidad actual = %q", got)
	}
	recorder := httptest.NewRecorder()
	local.handleStatus(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))
	var status struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.AgentID != "agent-canonical" {
		t.Fatalf("/status conserva la identidad anterior: %q", status.AgentID)
	}
	restarted := NewWSClient(WSClientConfig{DataDir: dataDir}, &Server{})
	if got := restarted.ensureAgentID(); got != "agent-canonical" {
		t.Fatalf("identidad después de reiniciar = %q", got)
	}

	// Los trabajos terminan en paralelo, junto con los mensajes de control.
	var workers sync.WaitGroup
	for i := 0; i < resultCount; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			if err := client.reportResult(WSJobResultPayload{JobID: fmt.Sprint(i), Status: "printed"}); err != nil {
				t.Errorf("reportResult: %v", err)
			}
		}(i)
	}
	workers.Wait()
	seen := make(map[string]bool)
	for i := 0; i < resultCount; i++ {
		msg := receive()
		if msg.Type != "job_result" {
			t.Fatalf("tipo recibido = %q; esperado job_result", msg.Type)
		}
		var result WSJobResultPayload
		if err := json.Unmarshal(msg.Payload, &result); err != nil {
			t.Fatal(err)
		}
		if seen[result.JobID] || result.Status != "printed" {
			t.Fatalf("resultado duplicado o corrupto: %+v", result)
		}
		seen[result.JobID] = true
	}
}

func TestAcceptRegistrationRejectsMissingOrInvalidIdentity(t *testing.T) {
	for _, payload := range []string{`{`, `{}`, `{"agent_id":"  "}`} {
		t.Run(payload, func(t *testing.T) {
			client := NewWSClient(WSClientConfig{AgentID: "original", DataDir: t.TempDir()}, &Server{})
			if err := client.acceptRegistration(json.RawMessage(payload)); err == nil {
				t.Fatal("se esperaba rechazo del registro inválido")
			}
			if client.currentAgentID() != "original" {
				t.Fatal("un ack inválido cambió la identidad")
			}
		})
	}
}

func TestAcceptRegistrationKeepsAuthorizedIdentityOnPersistenceFailure(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dataPath, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	local := &Server{}
	client := NewWSClient(WSClientConfig{AgentID: "old", DataDir: dataPath}, local)
	if err := client.acceptRegistration(json.RawMessage(`{"agent_id":"authorized"}`)); err == nil {
		t.Fatal("se esperaba informar el error de persistencia")
	}
	if client.currentAgentID() != "authorized" || local.agentID != "authorized" {
		t.Fatal("el fallo de disco no debe restaurar la identidad anterior durante la sesión")
	}
}

func TestReportResultRejectsDisconnectedAndFullQueue(t *testing.T) {
	client := NewWSClient(WSClientConfig{}, &Server{})
	if err := client.reportResult(WSJobResultPayload{JobID: "job"}); err == nil {
		t.Fatal("se aceptó un resultado sin conexión")
	}
	client.conn = &websocket.Conn{}
	client.send = make(chan []byte, 1)
	if err := client.reportResult(WSJobResultPayload{JobID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := client.reportResult(WSJobResultPayload{JobID: "second"}); err == nil {
		t.Fatal("se debe informar una cola llena sin bloquear al trabajador")
	}
}
