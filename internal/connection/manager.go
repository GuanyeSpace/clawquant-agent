package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/GuanyeSpace/clawquant-agent/internal/command"
	clawcrypto "github.com/GuanyeSpace/clawquant-agent/internal/crypto"
	"github.com/GuanyeSpace/clawquant-agent/internal/storage"
)

const (
	defaultBackoff      = 3 * time.Second
	maxBackoff          = 60 * time.Second
	heartbeatInterval   = 30 * time.Second
	websocketWriteWait  = 10 * time.Second
	authResponseTimeout = 15 * time.Second
	sendBufferSize      = 64
	logBufferSize       = 256
	logSyncBatchSize    = 100
)

var ErrNotConnected = errors.New("websocket not connected")

type BotCounter interface {
	BotsRunning() int
}

type Manager struct {
	token     string
	secret    string
	serverURL string

	conn      *websocket.Conn
	connected bool
	sendCh    chan []byte
	handler   command.CommandHandler

	statsProvider BotCounter
	store         *storage.Store
	logger        *log.Logger
	dialer        *websocket.Dialer

	mu           sync.RWMutex
	writeMu      sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	connCancel   context.CancelFunc
	connDone     chan struct{}
	connDoneOnce *sync.Once
	agentID      string
	closed       bool
	logCh        chan queuedLog
	logSyncCh    chan struct{}
	workersReady bool
}

type authMessage struct {
	Type      string `json:"type"`
	Token     string `json:"token"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
}

type authResult struct {
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Message string `json:"message"`
	AgentID string `json:"agent_id"`
}

type heartbeatMessage struct {
	Type        string  `json:"type"`
	AgentID     string  `json:"agent_id"`
	BotsRunning int     `json:"bots_running"`
	CPU         float64 `json:"cpu"`
	Memory      float64 `json:"memory"`
}

type envelope struct {
	Type string `json:"type"`
}

type queuedLog struct {
	BotID   string
	Level   string
	Message string
	Data    string
}

type logMessage struct {
	Type    string          `json:"type"`
	BotID   string          `json:"bot_id"`
	Level   string          `json:"level"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Time    int64           `json:"time"`
}

type botStatusMessage struct {
	Type   string `json:"type"`
	BotID  string `json:"bot_id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

func NewManager(token, secret, serverURL string, handler command.CommandHandler, statsProvider BotCounter, store *storage.Store, logger *log.Logger) *Manager {
	return &Manager{
		token:         token,
		secret:        secret,
		serverURL:     serverURL,
		sendCh:        make(chan []byte, sendBufferSize),
		handler:       handler,
		statsProvider: statsProvider,
		store:         store,
		logger:        logger,
		dialer:        websocket.DefaultDialer,
		logCh:         make(chan queuedLog, logBufferSize),
		logSyncCh:     make(chan struct{}, 1),
	}
}

func (m *Manager) Connect(ctx context.Context) error {
	if m.handler == nil {
		return fmt.Errorf("command handler is required")
	}

	m.mu.Lock()
	if m.ctx != nil {
		m.mu.Unlock()
		return fmt.Errorf("manager already started")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()

	if err := m.connectOnce(); err != nil {
		m.cancel()
		return err
	}

	m.mu.Lock()
	if !m.workersReady {
		m.workersReady = true
		m.wg.Add(3)
		go m.superviseReconnects()
		go m.logPersistLoop()
		go m.logSyncLoop()
	}
	m.mu.Unlock()

	m.signalLogSync()

	return nil
}

func (m *Manager) Reconnect() error {
	return m.connectOnce()
}

func (m *Manager) Send(msg []byte) error {
	m.mu.RLock()
	ctx := m.ctx
	connected := m.connected
	closed := m.closed
	m.mu.RUnlock()

	if closed {
		return context.Canceled
	}

	if ctx == nil {
		return ErrNotConnected
	}

	if !connected {
		return ErrNotConnected
	}

	payload := append([]byte(nil), msg...)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.sendCh <- payload:
		return nil
	default:
		return fmt.Errorf("send queue full")
	}
}

func (m *Manager) SendLog(botID, level, message string, data interface{}) error {
	rawData, err := marshalLogData(data)
	if err != nil {
		return err
	}

	entry := queuedLog{
		BotID:   botID,
		Level:   level,
		Message: message,
		Data:    rawData,
	}

	m.mu.RLock()
	ctx := m.ctx
	closed := m.closed
	m.mu.RUnlock()

	if closed {
		return context.Canceled
	}

	if ctx == nil {
		return ErrNotConnected
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.logCh <- entry:
		return nil
	default:
		return fmt.Errorf("log queue full")
	}
}

func (m *Manager) SendBotStatus(botID, status, errMsg string) error {
	payload, err := json.Marshal(botStatusMessage{
		Type:   "bot_status",
		BotID:  botID,
		Status: status,
		Error:  errMsg,
	})
	if err != nil {
		return err
	}

	return m.Send(payload)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}

	m.closed = true
	cancel := m.cancel
	conn := m.conn
	done := m.connDone
	doneOnce := m.connDoneOnce
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if conn != nil {
		m.writeMu.Lock()
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"), time.Now().Add(websocketWriteWait))
		m.writeMu.Unlock()
		_ = conn.Close()
	}

	if doneOnce != nil && done != nil {
		doneOnce.Do(func() {
			close(done)
		})
	}

	m.wg.Wait()
	return nil
}

func (m *Manager) connectOnce() error {
	ctx := m.currentContext()
	if ctx == nil {
		return context.Canceled
	}

	endpoint, err := buildAgentEndpoint(m.serverURL)
	if err != nil {
		return err
	}

	conn, _, err := m.dialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return err
	}

	if err := m.authenticate(conn); err != nil {
		_ = conn.Close()
		return err
	}

	connCtx, connCancel := context.WithCancel(ctx)
	connDone := make(chan struct{})
	connDoneOnce := &sync.Once{}

	m.mu.Lock()
	m.conn = conn
	m.connected = true
	m.connCancel = connCancel
	m.connDone = connDone
	m.connDoneOnce = connDoneOnce
	if m.agentID == "" {
		m.agentID = m.token
	}
	m.mu.Unlock()

	if m.logger != nil {
		m.logger.Printf("WebSocket connected to %s", endpoint)
	}

	m.wg.Add(3)
	go m.readPump(connCtx, conn)
	go m.writePump(connCtx, conn)
	go m.heartbeatLoop(connCtx)

	m.signalLogSync()

	return nil
}

func (m *Manager) authenticate(conn *websocket.Conn) error {
	timestamp := time.Now().Unix()
	auth := authMessage{
		Type:      "auth",
		Token:     m.token,
		Timestamp: timestamp,
		Signature: clawcrypto.Sign(m.token, m.secret, timestamp),
	}

	if err := m.writeJSON(conn, auth); err != nil {
		return err
	}

	conn.SetReadDeadline(time.Now().Add(authResponseTimeout))
	defer conn.SetReadDeadline(time.Time{})

	_, payload, err := conn.ReadMessage()
	if err != nil {
		return err
	}

	var result authResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return err
	}

	if result.Type != "auth_result" {
		return fmt.Errorf("unexpected auth response type %q", result.Type)
	}

	if !result.Success {
		if result.Message == "" {
			result.Message = "authentication failed"
		}

		return errors.New(result.Message)
	}

	m.mu.Lock()
	if strings.TrimSpace(result.AgentID) != "" {
		m.agentID = result.AgentID
	} else {
		m.agentID = m.token
	}
	m.mu.Unlock()

	return nil
}

func (m *Manager) readPump(ctx context.Context, conn *websocket.Conn) {
	defer m.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			m.handleDisconnect(err)
			return
		}

		var env envelope
		if err := json.Unmarshal(payload, &env); err != nil {
			m.logf("Discard invalid JSON message: %v", err)
			continue
		}

		if err := m.dispatch(payload, env.Type); err != nil {
			m.logf("Handle %s failed: %v", env.Type, err)
		}
	}
}

func (m *Manager) writePump(ctx context.Context, conn *websocket.Conn) {
	defer m.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-m.sendCh:
			if err := m.writeMessage(conn, websocket.TextMessage, msg); err != nil {
				m.handleDisconnect(err)
				return
			}
		}
	}
}

func (m *Manager) heartbeatLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			payload, err := json.Marshal(heartbeatMessage{
				Type:        "heartbeat",
				AgentID:     m.currentAgentID(),
				BotsRunning: m.botsRunning(),
				CPU:         0,
				Memory:      0,
			})
			if err != nil {
				m.logf("Encode heartbeat failed: %v", err)
				continue
			}

			if err := m.Send(payload); err != nil && !errors.Is(err, ErrNotConnected) && !errors.Is(err, context.Canceled) {
				m.logf("Send heartbeat failed: %v", err)
			}
		}
	}
}

func (m *Manager) superviseReconnects() {
	defer m.wg.Done()

	backoff := defaultBackoff

	for {
		connDone := m.currentConnDone()
		if connDone == nil {
			return
		}

		select {
		case <-m.currentContext().Done():
			return
		case <-connDone:
		}

		if err := m.currentContext().Err(); err != nil {
			return
		}

		for {
			m.logf("WebSocket disconnected, retrying in %s", backoff)

			timer := time.NewTimer(backoff)
			select {
			case <-m.currentContext().Done():
				timer.Stop()
				return
			case <-timer.C:
			}

			if err := m.Reconnect(); err != nil {
				m.logf("Reconnect failed: %v", err)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			m.logf("Reconnected to platform")
			backoff = defaultBackoff
			break
		}
	}
}

func (m *Manager) dispatch(payload []byte, messageType string) error {
	switch messageType {
	case "create_bot":
		var cmd command.CreateBotCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			return err
		}
		return m.handler.HandleCreateBot(cmd)
	case "stop_bot":
		var cmd command.StopBotCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			return err
		}
		return m.handler.HandleStopBot(cmd)
	case "restart_bot":
		var cmd command.RestartBotCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			return err
		}
		return m.handler.HandleRestartBot(cmd)
	default:
		m.logf("Ignoring unsupported message type %q", messageType)
		return nil
	}
}

func (m *Manager) logPersistLoop() {
	defer m.wg.Done()

	ctx := m.currentContext()
	if ctx == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case entry := <-m.logCh:
			if m.store == nil {
				m.logf("Log store not configured, discarding bot log for %s", entry.BotID)
				continue
			}

			if err := m.store.SaveLog(entry.BotID, entry.Level, entry.Message, entry.Data); err != nil {
				m.logf("Persist bot log failed: %v", err)
				continue
			}

			m.signalLogSync()
		}
	}
}

func (m *Manager) logSyncLoop() {
	defer m.wg.Done()

	ctx := m.currentContext()
	if ctx == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.logSyncCh:
			if err := m.syncUnsyncedLogs(); err != nil && !errors.Is(err, ErrNotConnected) && !errors.Is(err, context.Canceled) {
				m.logf("Sync unsynced logs failed: %v", err)
			}
		}
	}
}

func (m *Manager) syncUnsyncedLogs() error {
	if m.store == nil {
		return nil
	}

	for {
		if !m.isConnected() {
			return ErrNotConnected
		}

		entries, err := m.store.GetUnsynced(logSyncBatchSize)
		if err != nil {
			return err
		}

		if len(entries) == 0 {
			return nil
		}

		var syncedIDs []int64
		for _, entry := range entries {
			payload, err := encodeStoredLog(entry)
			if err != nil {
				m.logf("Encode stored log failed for id=%d: %v", entry.ID, err)
				syncedIDs = append(syncedIDs, entry.ID)
				continue
			}

			if err := m.writeDirect(payload); err != nil {
				if err := m.store.MarkSynced(syncedIDs); err != nil {
					m.logf("Mark synced partial batch failed: %v", err)
				}
				return err
			}

			syncedIDs = append(syncedIDs, entry.ID)
		}

		if err := m.store.MarkSynced(syncedIDs); err != nil {
			return err
		}

		if len(entries) < logSyncBatchSize {
			return nil
		}
	}
}

func (m *Manager) handleDisconnect(err error) {
	m.mu.Lock()
	cancel := m.connCancel
	conn := m.conn
	done := m.connDone
	doneOnce := m.connDoneOnce
	connected := m.connected
	m.connected = false
	m.conn = nil
	m.connCancel = nil
	m.mu.Unlock()

	if !connected {
		return
	}

	if err != nil {
		m.logf("WebSocket connection lost: %v", err)
	}

	if cancel != nil {
		cancel()
	}

	if conn != nil {
		_ = conn.Close()
	}

	if doneOnce != nil && done != nil {
		doneOnce.Do(func() {
			close(done)
		})
	}
}

func buildAgentEndpoint(serverURL string) (string, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}

	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", fmt.Errorf("server must use ws or wss scheme")
	}

	basePath := strings.TrimRight(parsed.Path, "/")
	parsed.Path = basePath + "/ws/agent"
	parsed.RawPath = ""
	parsed.RawQuery = ""

	return parsed.String(), nil
}

func marshalLogData(data interface{}) (string, error) {
	if data == nil {
		return "{}", nil
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	if len(payload) == 0 {
		return "{}", nil
	}

	return string(payload), nil
}

func encodeStoredLog(entry storage.LogEntry) ([]byte, error) {
	rawData := entry.Data
	if strings.TrimSpace(rawData) == "" {
		rawData = "{}"
	}

	message := logMessage{
		Type:    "log",
		BotID:   entry.BotID,
		Level:   entry.Level,
		Message: entry.Message,
		Data:    json.RawMessage(rawData),
		Time:    entry.CreatedAt,
	}

	return json.Marshal(message)
}

func (m *Manager) currentContext() context.Context {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.ctx
}

func (m *Manager) currentConnDone() chan struct{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.connDone
}

func (m *Manager) isConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.connected && m.conn != nil
}

func (m *Manager) currentAgentID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.agentID == "" {
		return m.token
	}

	return m.agentID
}

func (m *Manager) signalLogSync() {
	select {
	case m.logSyncCh <- struct{}{}:
	default:
	}
}

func (m *Manager) writeJSON(conn *websocket.Conn, value interface{}) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return m.writeMessage(conn, websocket.TextMessage, payload)
}

func (m *Manager) writeMessage(conn *websocket.Conn, messageType int, payload []byte) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	conn.SetWriteDeadline(time.Now().Add(websocketWriteWait))
	return conn.WriteMessage(messageType, payload)
}

func (m *Manager) writeDirect(payload []byte) error {
	m.mu.RLock()
	conn := m.conn
	connected := m.connected
	m.mu.RUnlock()

	if !connected || conn == nil {
		return ErrNotConnected
	}

	if err := m.writeMessage(conn, websocket.TextMessage, payload); err != nil {
		m.handleDisconnect(err)
		return err
	}

	return nil
}

func (m *Manager) botsRunning() int {
	if m.statsProvider == nil {
		return 0
	}

	return m.statsProvider.BotsRunning()
}

func (m *Manager) logf(format string, args ...any) {
	if m.logger != nil {
		m.logger.Printf(format, args...)
	}
}
