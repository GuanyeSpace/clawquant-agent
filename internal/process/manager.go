package process

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GuanyeSpace/clawquant-agent/internal/command"
	"github.com/GuanyeSpace/clawquant-agent/internal/connection"
	clawcrypto "github.com/GuanyeSpace/clawquant-agent/internal/crypto"
	"github.com/GuanyeSpace/clawquant-agent/internal/storage"
)

const (
	stdoutScannerBufferSize = 1024 * 1024
	stopTimeout             = 3 * time.Second
)

type Manager struct {
	processes map[string]*BotProcess
	configs   map[string]command.CreateBotCommand
	mutex     sync.RWMutex

	dataDir string
	sdkDir  string
	connMgr *connection.Manager
	storage *storage.Store
	logger  *log.Logger
}

type BotProcess struct {
	BotID      string
	Ctx        context.Context
	Cmd        *exec.Cmd
	Cancel     context.CancelFunc
	Stdin      io.WriteCloser
	StdoutDone chan struct{}
	StartedAt  time.Time

	Done          chan struct{}
	StrategyPath  string
	stopRequested atomic.Bool
	reportedExit  atomic.Int32
}

type stdoutEnvelope struct {
	Type    string          `json:"type"`
	Level   string          `json:"level"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Value   float64         `json:"value"`
	Code    int             `json:"code"`
	Error   string          `json:"error"`
}

func NewManager(dataDir, sdkDir string, store *storage.Store, logger *log.Logger) (*Manager, error) {
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}

	absSDKDir, err := filepath.Abs(sdkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve sdk dir: %w", err)
	}

	if info, err := os.Stat(absSDKDir); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return nil, fmt.Errorf("sdk dir %s is unavailable: %w", absSDKDir, err)
	}

	return &Manager{
		processes: make(map[string]*BotProcess),
		configs:   make(map[string]command.CreateBotCommand),
		dataDir:   absDataDir,
		sdkDir:    absSDKDir,
		storage:   store,
		logger:    logger,
	}, nil
}

func (m *Manager) SetConnectionManager(connMgr *connection.Manager) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.connMgr = connMgr
}

func (m *Manager) StartBot(cmd command.CreateBotCommand) (err error) {
	if strings.TrimSpace(cmd.BotID) == "" {
		return fmt.Errorf("bot_id is required")
	}

	if strings.TrimSpace(cmd.StrategyCode) == "" {
		return fmt.Errorf("strategy_code is required")
	}

	if !json.Valid(normalizeParams(cmd.Params)) {
		return fmt.Errorf("params must be valid JSON")
	}

	m.mutex.RLock()
	if _, exists := m.processes[cmd.BotID]; exists {
		m.mutex.RUnlock()
		return fmt.Errorf("bot %s is already running", cmd.BotID)
	}
	m.mutex.RUnlock()

	defer func() {
		if err != nil {
			m.sendBotStatus(cmd.BotID, "error", err.Error())
		}
	}()

	apiKey, secret, err := resolveExchangeCredentials(cmd.Exchange, cmd.EncryptionKey)
	if err != nil {
		return err
	}

	botDir := filepath.Join(m.dataDir, "bots", cmd.BotID)
	if err := os.MkdirAll(botDir, 0o755); err != nil {
		return fmt.Errorf("create bot directory: %w", err)
	}

	strategyPath := filepath.Join(botDir, "strategy.py")
	if err := os.WriteFile(strategyPath, []byte(cmd.StrategyCode), 0o600); err != nil {
		return fmt.Errorf("write strategy file: %w", err)
	}

	pythonBin, pythonArgs, err := detectPythonCommand()
	if err != nil {
		return err
	}

	args := append([]string{}, pythonArgs...)
	args = append(args, "-m", "clawquant.runner", strategyPath)

	ctx, cancel := context.WithCancel(context.Background())
	processCmd := exec.Command(pythonBin, args...)
	processCmd.Dir = m.sdkDir
	processCmd.Env = buildPythonEnv(os.Environ(), m.sdkDir, botDir, cmd, apiKey, secret)
	processCmd.Stderr = logWriter{logger: m.logger, botID: cmd.BotID}

	stdin, err := processCmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := processCmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := processCmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return fmt.Errorf("start python process: %w", err)
	}

	process := &BotProcess{
		BotID:        cmd.BotID,
		Ctx:          ctx,
		Cmd:          processCmd,
		Cancel:       cancel,
		Stdin:        stdin,
		StdoutDone:   make(chan struct{}),
		StartedAt:    time.Now(),
		Done:         make(chan struct{}),
		StrategyPath: strategyPath,
	}

	m.mutex.Lock()
	if _, exists := m.processes[cmd.BotID]; exists {
		m.mutex.Unlock()
		cancel()
		_ = stdin.Close()
		_ = processCmd.Process.Kill()
		_, _ = processCmd.Process.Wait()
		return fmt.Errorf("bot %s is already running", cmd.BotID)
	}
	m.processes[cmd.BotID] = process
	m.configs[cmd.BotID] = cmd
	m.mutex.Unlock()

	go m.readStdout(cmd.BotID, stdout)
	go m.monitorProcess(cmd.BotID, processCmd)

	m.logf("Started bot %s with PID %d", cmd.BotID, processCmd.Process.Pid)
	m.sendBotStatus(cmd.BotID, "running", "")
	return nil
}

func (m *Manager) readStdout(botID string, stdout io.Reader) {
	process := m.getProcess(botID)
	if process == nil {
		return
	}

	defer close(process.StdoutDone)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), stdoutScannerBufferSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		m.logf("bot %s stdout: %s", botID, line)
		m.handleStdoutLine(botID, line)
	}

	if err := scanner.Err(); err != nil {
		m.emitLog(botID, "error", "failed to read bot stdout", map[string]any{"error": err.Error()})
	}
}

func (m *Manager) monitorProcess(botID string, cmd *exec.Cmd) {
	process := m.getProcess(botID)
	if process == nil {
		return
	}

	err := cmd.Wait()
	<-process.StdoutDone

	stopRequested := process.stopRequested.Load()
	status := "stopped"
	errMsg := ""
	if err != nil && !stopRequested {
		status = "error"
		errMsg = err.Error()
	} else if code := process.reportedExit.Load(); code != 0 && !stopRequested {
		status = "error"
		errMsg = fmt.Sprintf("exit code %d", code)
	}

	if process.Stdin != nil {
		_ = process.Stdin.Close()
	}

	m.mutex.Lock()
	delete(m.processes, botID)
	m.mutex.Unlock()

	m.sendBotStatus(botID, status, errMsg)
	m.logf("Bot %s exited with status=%s error=%s", botID, status, errMsg)
	close(process.Done)
}

func (m *Manager) StopBot(botID string) error {
	process := m.getProcess(botID)
	if process == nil {
		return fmt.Errorf("bot %s is not running", botID)
	}

	process.stopRequested.Store(true)
	process.Cancel()
	if process.Stdin != nil {
		_ = process.Stdin.Close()
	}

	if process.Cmd != nil && process.Cmd.Process != nil {
		_ = process.Cmd.Process.Signal(os.Interrupt)
	}

	timer := time.NewTimer(stopTimeout)
	defer timer.Stop()

	select {
	case <-process.Done:
		return nil
	case <-timer.C:
		if process.Cmd != nil && process.Cmd.Process != nil {
			if err := process.Cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return fmt.Errorf("kill bot %s: %w", botID, err)
			}
		}
		<-process.Done
		return nil
	}
}

func (m *Manager) RestartBot(botID string) error {
	m.mutex.RLock()
	config, ok := m.configs[botID]
	m.mutex.RUnlock()
	if !ok {
		return fmt.Errorf("bot %s has no saved configuration", botID)
	}

	if process := m.getProcess(botID); process != nil {
		if err := m.StopBot(botID); err != nil {
			return err
		}
	}

	return m.StartBot(config)
}

func (m *Manager) GetRunningBots() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	bots := make([]string, 0, len(m.processes))
	for botID := range m.processes {
		bots = append(bots, botID)
	}

	return bots
}

func (m *Manager) GetBotCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return len(m.processes)
}

func (m *Manager) BotsRunning() int {
	return m.GetBotCount()
}

func (m *Manager) StopAll(ctx context.Context) error {
	var firstErr error

	for _, botID := range m.GetRunningBots() {
		select {
		case <-ctx.Done():
			if firstErr != nil {
				return firstErr
			}
			return ctx.Err()
		default:
		}

		if err := m.StopBot(botID); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (m *Manager) handleStdoutLine(botID, line string) {
	var msg stdoutEnvelope
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		m.emitLog(botID, "info", line, nil)
		return
	}

	switch msg.Type {
	case "log":
		level := strings.TrimSpace(msg.Level)
		if level == "" {
			level = "info"
		}
		m.emitLog(botID, level, msg.Message, rawDataToAny(msg.Data))
	case "profit":
		m.emitLog(botID, "profit", "profit update", map[string]any{"value": msg.Value})
	case "status":
		m.emitLog(botID, "status", msg.Message, nil)
		if m.storage != nil {
			_ = m.storage.SetStorage(botID, "last_status", msg.Message)
		}
	case "exit":
		if process := m.getProcess(botID); process != nil {
			process.reportedExit.Store(int32(msg.Code))
		}
	case "error":
		message := msg.Message
		if message == "" {
			message = msg.Error
		}
		m.emitLog(botID, "error", message, rawDataToAny(msg.Data))
	default:
		m.emitLog(botID, "info", line, nil)
	}
}

func (m *Manager) emitLog(botID, level, message string, data any) {
	if strings.TrimSpace(message) == "" {
		message = level
	}

	if m.connMgr != nil {
		if err := m.connMgr.SendLog(botID, level, message, data); err == nil {
			return
		} else {
			m.logf("send log via connection failed for bot %s: %v", botID, err)
		}
	}

	if m.storage != nil {
		payload, err := marshalDataString(data)
		if err != nil {
			m.logf("marshal log payload failed for bot %s: %v", botID, err)
			payload = "{}"
		}
		if err := m.storage.SaveLog(botID, level, message, payload); err != nil {
			m.logf("persist log failed for bot %s: %v", botID, err)
		}
	}
}

func (m *Manager) sendBotStatus(botID, status, errMsg string) {
	if m.connMgr == nil {
		return
	}

	if err := m.connMgr.SendBotStatus(botID, status, errMsg); err != nil {
		m.logf("send bot status failed for bot %s: %v", botID, err)
	}
}

func (m *Manager) getProcess(botID string) *BotProcess {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.processes[botID]
}

func resolveEncryptionKey(commandKey string) (string, error) {
	if strings.TrimSpace(commandKey) != "" {
		return commandKey, nil
	}

	envKey := strings.TrimSpace(os.Getenv("CLAWQUANT_ENCRYPTION_KEY"))
	if envKey != "" {
		return envKey, nil
	}

	return "", fmt.Errorf("encryption key is required")
}

func resolveExchangeCredentials(exchange command.ExchangeConfig, commandKey string) (string, string, error) {
	apiKey := strings.TrimSpace(exchange.APIKey)
	secret := strings.TrimSpace(exchange.Secret)
	if apiKey != "" || secret != "" {
		if apiKey == "" || secret == "" {
			return "", "", fmt.Errorf("api key and secret must both be provided")
		}
		return apiKey, secret, nil
	}

	if strings.TrimSpace(exchange.EncryptedAPIKey) == "" || strings.TrimSpace(exchange.EncryptedSecret) == "" {
		return "", "", fmt.Errorf("api key and secret are required")
	}

	encryptionKey, err := resolveEncryptionKey(commandKey)
	if err != nil {
		return "", "", err
	}

	apiKey, err = clawcrypto.Decrypt(exchange.EncryptedAPIKey, encryptionKey)
	if err != nil {
		return "", "", fmt.Errorf("decrypt api key: %w", err)
	}

	secret, err = clawcrypto.Decrypt(exchange.EncryptedSecret, encryptionKey)
	if err != nil {
		return "", "", fmt.Errorf("decrypt secret: %w", err)
	}

	return apiKey, secret, nil
}

func buildPythonEnv(baseEnv []string, sdkDir, botDir string, cmd command.CreateBotCommand, apiKey, secret string) []string {
	env := make(map[string]string, len(baseEnv)+8)
	for _, item := range baseEnv {
		parts := strings.SplitN(item, "=", 2)
		key := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}
		env[key] = value
	}

	pythonPath := sdkDir
	if existing := strings.TrimSpace(env["PYTHONPATH"]); existing != "" {
		pythonPath = sdkDir + string(os.PathListSeparator) + existing
	}

	env["PYTHONPATH"] = pythonPath
	env["CLAWQUANT_BOT_ID"] = cmd.BotID
	env["CLAWQUANT_EXCHANGE_TYPE"] = cmd.Exchange.Type
	env["CLAWQUANT_API_KEY"] = apiKey
	env["CLAWQUANT_SECRET"] = secret
	env["CLAWQUANT_TRADING_PAIR"] = cmd.Exchange.TradingPair
	env["CLAWQUANT_PARAMS"] = string(normalizeParams(cmd.Params))
	env["CLAWQUANT_DATA_DIR"] = botDir
	env["CLAWQUANT_TESTNET"] = boolToFlag(cmd.Exchange.Testnet)

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}

	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+env[key])
	}

	return result
}

func normalizeParams(params json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(params))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	return params
}

func boolToFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func marshalDataString(data any) (string, error) {
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

func rawDataToAny(data json.RawMessage) any {
	if len(data) == 0 {
		return nil
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return map[string]any{"raw": string(data)}
	}
	return value
}

func detectPythonCommand() (string, []string, error) {
	if override := strings.TrimSpace(os.Getenv("CLAWQUANT_PYTHON_BIN")); override != "" {
		parts := strings.Fields(override)
		if len(parts) == 0 {
			return "", nil, fmt.Errorf("CLAWQUANT_PYTHON_BIN is invalid")
		}
		if _, err := exec.LookPath(parts[0]); err != nil {
			return "", nil, fmt.Errorf("python executable %s not found: %w", parts[0], err)
		}
		return parts[0], parts[1:], nil
	}

	candidates := [][]string{
		{"python"},
		{"python3"},
	}
	if runtime.GOOS == "windows" {
		candidates = append([][]string{{"py", "-3"}}, candidates...)
	}

	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return candidate[0], candidate[1:], nil
		}
	}

	return "", nil, fmt.Errorf("python executable not found")
}

func (m *Manager) logf(format string, args ...any) {
	if m.logger != nil {
		m.logger.Printf(format, args...)
	}
}

type logWriter struct {
	logger *log.Logger
	botID  string
}

func (w logWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text != "" && w.logger != nil {
		w.logger.Printf("bot %s stderr: %s", w.botID, text)
	}
	return len(p), nil
}
