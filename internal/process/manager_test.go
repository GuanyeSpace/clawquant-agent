package process

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GuanyeSpace/clawquant-agent/internal/command"
	"github.com/GuanyeSpace/clawquant-agent/internal/storage"
)

func TestStartAndStopBot(t *testing.T) {
	pythonBin := requirePythonSDK(t)
	t.Setenv("CLAWQUANT_PYTHON_BIN", pythonBin)

	manager, store := newTestManager(t)
	defer store.Close()

	cmd := command.CreateBotCommand{
		BotID:         "bot-start-stop",
		StrategyCode:  "from clawquant import log, sleep\n\ndef main(exchange, params):\n    log('started', {'value': params['value']})\n    sleep(30000)\n",
		Params:        json.RawMessage(`{"value": 42}`),
		EncryptionKey: "test-password",
		Exchange: command.ExchangeConfig{
			Type:            "binance",
			TradingPair:     "BTC_USDT",
			EncryptedAPIKey: encryptForTest(t, "api-key", "test-password"),
			EncryptedSecret: encryptForTest(t, "api-secret", "test-password"),
		},
	}

	if err := manager.StartBot(cmd); err != nil {
		t.Fatalf("StartBot returned error: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		return manager.GetBotCount() == 1 && hasLog(store, "bot-start-stop", "started")
	})

	if err := manager.StopBot(cmd.BotID); err != nil {
		t.Fatalf("StopBot returned error: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		return manager.GetBotCount() == 0
	})
}

func TestBotCrashProducesErrorLog(t *testing.T) {
	pythonBin := requirePythonSDK(t)
	t.Setenv("CLAWQUANT_PYTHON_BIN", pythonBin)

	manager, store := newTestManager(t)
	defer store.Close()

	cmd := command.CreateBotCommand{
		BotID:         "bot-crash",
		StrategyCode:  "from clawquant import log\n\ndef main(exchange, params):\n    log('before crash')\n    raise RuntimeError('boom')\n",
		Params:        json.RawMessage(`{}`),
		EncryptionKey: "test-password",
		Exchange: command.ExchangeConfig{
			Type:            "binance",
			TradingPair:     "BTC_USDT",
			EncryptedAPIKey: encryptForTest(t, "api-key", "test-password"),
			EncryptedSecret: encryptForTest(t, "api-secret", "test-password"),
		},
	}

	if err := manager.StartBot(cmd); err != nil {
		t.Fatalf("StartBot returned error: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		return manager.GetBotCount() == 0 && hasLogLevel(store, "bot-crash", "error")
	})
}

func TestRestartBotUsesSavedConfiguration(t *testing.T) {
	pythonBin := requirePythonSDK(t)
	t.Setenv("CLAWQUANT_PYTHON_BIN", pythonBin)

	manager, store := newTestManager(t)
	defer store.Close()

	cmd := command.CreateBotCommand{
		BotID:         "bot-restart",
		StrategyCode:  "from clawquant import log, sleep\n\ndef main(exchange, params):\n    log('restartable', {'value': params['value']})\n    sleep(30000)\n",
		Params:        json.RawMessage(`{"value": 7}`),
		EncryptionKey: "test-password",
		Exchange: command.ExchangeConfig{
			Type:            "binance",
			TradingPair:     "BTC_USDT",
			EncryptedAPIKey: encryptForTest(t, "api-key", "test-password"),
			EncryptedSecret: encryptForTest(t, "api-secret", "test-password"),
		},
	}

	if err := manager.StartBot(cmd); err != nil {
		t.Fatalf("StartBot returned error: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		return manager.GetBotCount() == 1 && hasLog(store, "bot-restart", "restartable")
	})

	if err := manager.RestartBot(cmd.BotID); err != nil {
		t.Fatalf("RestartBot returned error: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		entries, err := store.GetUnsyncedLogs(100)
		if err != nil {
			return false
		}

		count := 0
		for _, entry := range entries {
			if entry.BotID == "bot-restart" && entry.Message == "restartable" {
				count++
			}
		}

		return manager.GetBotCount() == 1 && count >= 2
	})

	if err := manager.StopBot(cmd.BotID); err != nil {
		t.Fatalf("StopBot returned error: %v", err)
	}
}

func newTestManager(t *testing.T) (*Manager, *storage.Store) {
	t.Helper()

	root := t.TempDir()
	store, _, err := storage.OpenSQLite(context.Background(), root)
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	sdkDir := filepath.Clean(filepath.Join(wd, "..", "..", "sdk"))

	manager, err := NewManager(root, sdkDir, store, log.New(testWriter{t}, "", 0))
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	return manager, store
}

func requirePythonSDK(t *testing.T) string {
	t.Helper()

	if path := strings.TrimSpace(os.Getenv("CLAWQUANT_TEST_PYTHON")); path != "" {
		return path
	}

	commands := [][]string{
		{"py", "-3", "-c", "import sys; print(sys.executable); import ccxt"},
		{"python", "-c", "import sys; print(sys.executable); import ccxt"},
		{"python3", "-c", "import sys; print(sys.executable); import ccxt"},
	}

	for _, args := range commands {
		output, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(output))
		}
	}

	t.Skip("python with ccxt is not available")
	return ""
}

func hasLog(store *storage.Store, botID, message string) bool {
	entries, err := store.GetUnsyncedLogs(100)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.BotID == botID && entry.Message == message {
			return true
		}
	}

	return false
}

func hasLogLevel(store *storage.Store, botID, level string) bool {
	entries, err := store.GetUnsyncedLogs(100)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.BotID == botID && entry.Level == level {
			return true
		}
	}

	return false
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatal("condition not met before timeout")
}

func encryptForTest(t *testing.T, plaintext, password string) string {
	t.Helper()

	salt := []byte("0123456789abcdef")
	nonce := []byte("123456789012")
	key, err := pbkdf2.Key(sha256.New, password, salt, 100000, 32)
	if err != nil {
		t.Fatalf("pbkdf2.Key returned error: %v", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher returned error: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM returned error: %v", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	blob := append(append(append([]byte{}, salt...), nonce...), ciphertext...)
	return base64.StdEncoding.EncodeToString(blob)
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}
