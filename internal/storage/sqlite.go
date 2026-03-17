package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

type Store struct {
	db *sql.DB
}

type LogEntry struct {
	ID        int64
	BotID     string
	Level     string
	Message   string
	Data      string
	CreatedAt int64
	Synced    bool
}

func OpenSQLite(ctx context.Context, dataDir string) (*Store, string, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "agent.db")
	dsn, err := sqliteDSN(dbPath)
	if err != nil {
		return nil, "", err
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, "", err
	}

	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, "", err
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
		db.Close()
		return nil, "", err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bot_id TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			data TEXT,
			created_at INTEGER NOT NULL,
			synced INTEGER DEFAULT 0
		);
	`); err != nil {
		db.Close()
		return nil, "", err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS storage (
			bot_id TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT,
			PRIMARY KEY(bot_id, key)
		);
	`); err != nil {
		db.Close()
		return nil, "", err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS agent_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`); err != nil {
		db.Close()
		return nil, "", err
	}

	return &Store{db: db}, dbPath, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

func (s *Store) SaveLog(botID, level, message, data string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is not initialized")
	}

	if strings.TrimSpace(data) == "" {
		data = "{}"
	}

	_, err := s.db.Exec(`
		INSERT INTO logs (bot_id, level, message, data, created_at, synced)
		VALUES (?, ?, ?, ?, ?, 0)
	`, botID, level, message, data, time.Now().Unix())
	return err
}

func (s *Store) GetUnsynced(limit int) ([]LogEntry, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is not initialized")
	}

	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(`
		SELECT id, bot_id, level, message, data, created_at, synced
		FROM logs
		WHERE synced = 0
		ORDER BY id ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var entry LogEntry
		var data sql.NullString
		var synced int64

		if err := rows.Scan(&entry.ID, &entry.BotID, &entry.Level, &entry.Message, &data, &entry.CreatedAt, &synced); err != nil {
			return nil, err
		}

		entry.Data = data.String
		entry.Synced = synced == 1
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (s *Store) MarkSynced(ids []int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is not initialized")
	}

	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	_, err := s.db.Exec(fmt.Sprintf(`
		UPDATE logs
		SET synced = 1
		WHERE id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	return err
}

func sqliteDSN(dbPath string) (string, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return "", err
	}

	return "file:" + filepath.ToSlash(absPath), nil
}
