package connection

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/GuanyeSpace/clawquant-agent/internal/command"
	clawcrypto "github.com/GuanyeSpace/clawquant-agent/internal/crypto"
	"github.com/GuanyeSpace/clawquant-agent/internal/storage"
)

func TestManagerConnectDispatchesCommand(t *testing.T) {
	upgrader := websocket.Upgrader{}
	statusCh := make(chan map[string]string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/agent" {
			http.NotFound(w, r)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		var auth authMessage
		if err := conn.ReadJSON(&auth); err != nil {
			t.Errorf("read auth failed: %v", err)
			return
		}

		if auth.Type != "auth" {
			t.Errorf("unexpected auth type: %s", auth.Type)
			return
		}

		if auth.Secret != "test-secret" {
			t.Errorf("unexpected auth secret: %q", auth.Secret)
			return
		}

		if !clawcrypto.Verify(auth.Token, "test-secret", auth.Signature, auth.Timestamp) {
			t.Errorf("invalid auth signature")
			return
		}

		if err := conn.WriteJSON(authResult{
			Type:    "auth_result",
			Success: true,
			AgentID: "agent-123",
		}); err != nil {
			t.Errorf("write auth result failed: %v", err)
			return
		}

		if err := conn.WriteJSON(command.CreateBotCommand{
			Type:  "create_bot",
			BotID: "bot-1",
		}); err != nil {
			t.Errorf("write create_bot failed: %v", err)
			return
		}

		var status map[string]string
		if err := conn.ReadJSON(&status); err != nil {
			t.Errorf("read bot_status failed: %v", err)
			return
		}

		statusCh <- status
	}))
	defer server.Close()

	logger := log.New(testWriter{t}, "", 0)
	dispatcher := command.NewDispatcher(logger)
	manager := NewManager("test-token", "test-secret", wsURL(server.URL), dispatcher, staticCounter(0), nil, logger)
	dispatcher.SetSender(manager)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := manager.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer manager.Close()

	select {
	case status := <-statusCh:
		if status["type"] != "bot_status" {
			t.Fatalf("unexpected message type: %q", status["type"])
		}

		if status["bot_id"] != "bot-1" {
			t.Fatalf("unexpected bot_id: %q", status["bot_id"])
		}

		if status["status"] != "running" {
			t.Fatalf("unexpected status: %q", status["status"])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for bot status")
	}
}

func TestManagerConnectRejectsFailedAuth(t *testing.T) {
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		var auth authMessage
		if err := conn.ReadJSON(&auth); err != nil {
			t.Errorf("read auth failed: %v", err)
			return
		}

		if auth.Secret != "test-secret" {
			t.Errorf("unexpected auth secret: %q", auth.Secret)
			return
		}

		_ = conn.WriteJSON(authResult{
			Type:    "auth_result",
			Success: false,
			Message: "bad credentials",
		})
	}))
	defer server.Close()

	manager := NewManager("test-token", "test-secret", wsURL(server.URL), noopHandler{}, staticCounter(0), nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := manager.Connect(ctx); err == nil || !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("expected auth failure, got %v", err)
	}
}

type staticCounter int

func (c staticCounter) BotsRunning() int {
	return int(c)
}

type noopHandler struct{}

func (noopHandler) HandleCreateBot(command.CreateBotCommand) error   { return nil }
func (noopHandler) HandleStopBot(command.StopBotCommand) error       { return nil }
func (noopHandler) HandleRestartBot(command.RestartBotCommand) error { return nil }

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

func wsURL(serverURL string) string {
	return "ws" + strings.TrimPrefix(serverURL, "http")
}

func TestBuildAgentEndpoint(t *testing.T) {
	got, err := buildAgentEndpoint("wss://platform.example.com/base")
	if err != nil {
		t.Fatalf("buildAgentEndpoint returned error: %v", err)
	}

	if got != "wss://platform.example.com/base/ws/agent" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
}

func TestDispatchIgnoresUnknownType(t *testing.T) {
	manager := NewManager("token", "secret", "ws://localhost:8080", noopHandler{}, staticCounter(0), nil, nil)
	payload, err := json.Marshal(map[string]string{"type": "unknown"})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if err := manager.dispatch(payload, "unknown"); err != nil {
		t.Fatalf("dispatch returned error: %v", err)
	}
}

func TestManagerSendLogPersistsAndSyncs(t *testing.T) {
	upgrader := websocket.Upgrader{}
	logCh := make(chan logMessage, 4)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		var auth authMessage
		if err := conn.ReadJSON(&auth); err != nil {
			t.Errorf("read auth failed: %v", err)
			return
		}

		if auth.Secret != "test-secret" {
			t.Errorf("unexpected auth secret: %q", auth.Secret)
			return
		}

		if err := conn.WriteJSON(authResult{Type: "auth_result", Success: true, AgentID: "agent-1"}); err != nil {
			t.Errorf("write auth_result failed: %v", err)
			return
		}

		for {
			var msg map[string]json.RawMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			var msgType string
			if err := json.Unmarshal(msg["type"], &msgType); err != nil {
				t.Errorf("unmarshal type failed: %v", err)
				return
			}

			if msgType != "log" {
				continue
			}

			var entry logMessage
			if err := json.Unmarshal(mustMarshal(t, msg), &entry); err != nil {
				t.Errorf("unmarshal log failed: %v", err)
				return
			}

			logCh <- entry
		}
	}))
	defer server.Close()

	store, _, err := storage.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer store.Close()

	manager := NewManager("test-token", "test-secret", wsURL(server.URL), noopHandler{}, staticCounter(0), store, log.New(testWriter{t}, "", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := manager.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer manager.Close()

	if err := manager.SendLog("bot-1", "info", "hello", map[string]string{"source": "test"}); err != nil {
		t.Fatalf("SendLog returned error: %v", err)
	}

	select {
	case msg := <-logCh:
		if msg.BotID != "bot-1" || msg.Level != "info" || msg.Message != "hello" {
			t.Fatalf("unexpected log message: %+v", msg)
		}

		if string(msg.Data) != `{"source":"test"}` {
			t.Fatalf("unexpected log data: %s", string(msg.Data))
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for synced log")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		entries, err := store.GetUnsynced(100)
		if err != nil {
			t.Fatalf("GetUnsynced returned error: %v", err)
		}

		if len(entries) == 0 {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("log entry was not marked synced: %+v", entries)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func TestManagerSendLogCachesWhenDisconnected(t *testing.T) {
	store, _, err := storage.OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer store.Close()

	manager := NewManager("token", "secret", "ws://localhost:8080", noopHandler{}, staticCounter(0), store, nil)
	manager.ctx = context.Background()

	if err := manager.SendLog("bot-2", "warn", "cached", map[string]any{"attempt": 1}); err != nil {
		t.Fatalf("SendLog returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	manager.cancel = cancel
	manager.ctx = ctx
	manager.wg.Add(2)
	go manager.logPersistLoop()
	go manager.logSyncLoop()

	deadline := time.Now().Add(2 * time.Second)
	for {
		entries, err := store.GetUnsynced(100)
		if err != nil {
			t.Fatalf("GetUnsynced returned error: %v", err)
		}

		if len(entries) == 1 {
			if entries[0].BotID != "bot-2" || entries[0].Synced {
				t.Fatalf("unexpected cached entry: %+v", entries[0])
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for cached log, got %d entries", len(entries))
		}

		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	manager.wg.Wait()
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	return payload
}
