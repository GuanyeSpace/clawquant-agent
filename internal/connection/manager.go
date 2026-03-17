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
)

const (
	defaultBackoff      = 3 * time.Second
	maxBackoff          = 60 * time.Second
	heartbeatInterval   = 30 * time.Second
	websocketWriteWait  = 10 * time.Second
	authResponseTimeout = 15 * time.Second
	sendBufferSize      = 64
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
	logger        *log.Logger
	dialer        *websocket.Dialer

	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	connCancel   context.CancelFunc
	connDone     chan struct{}
	connDoneOnce *sync.Once
	agentID      string
	closed       bool
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

func NewManager(token, secret, serverURL string, handler command.CommandHandler, statsProvider BotCounter, logger *log.Logger) *Manager {
	return &Manager{
		token:         token,
		secret:        secret,
		serverURL:     serverURL,
		sendCh:        make(chan []byte, sendBufferSize),
		handler:       handler,
		statsProvider: statsProvider,
		logger:        logger,
		dialer:        websocket.DefaultDialer,
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

	m.wg.Add(1)
	go m.superviseReconnects()

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
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"), time.Now().Add(websocketWriteWait))
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

	conn.SetWriteDeadline(time.Now().Add(websocketWriteWait))
	if err := conn.WriteJSON(auth); err != nil {
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
			conn.SetWriteDeadline(time.Now().Add(websocketWriteWait))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
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

func (m *Manager) currentAgentID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.agentID == "" {
		return m.token
	}

	return m.agentID
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
