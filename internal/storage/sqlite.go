package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func OpenSQLite(ctx context.Context, dataDir string) (*sql.DB, string, error) {
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
		CREATE TABLE IF NOT EXISTS agent_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`); err != nil {
		db.Close()
		return nil, "", err
	}

	return db, dbPath, nil
}

func sqliteDSN(dbPath string) (string, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return "", err
	}

	return (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(absPath),
	}).String(), nil
}
