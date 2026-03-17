package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoreSavesAndMarksLogsSynced(t *testing.T) {
	store, _, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer store.Close()

	if err := store.SaveLog("bot-1", "info", "started", `{"step":1}`); err != nil {
		t.Fatalf("SaveLog returned error: %v", err)
	}

	if err := store.SaveLog("bot-2", "error", "failed", `{"code":"E1"}`); err != nil {
		t.Fatalf("SaveLog returned error: %v", err)
	}

	entries, err := store.GetUnsynced(100)
	if err != nil {
		t.Fatalf("GetUnsynced returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 unsynced entries, got %d", len(entries))
	}

	if entries[0].BotID != "bot-1" || entries[1].BotID != "bot-2" {
		t.Fatalf("unexpected entry order: %+v", entries)
	}

	if err := store.MarkSynced([]int64{entries[0].ID}); err != nil {
		t.Fatalf("MarkSynced returned error: %v", err)
	}

	remaining, err := store.GetUnsynced(100)
	if err != nil {
		t.Fatalf("GetUnsynced returned error: %v", err)
	}

	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining unsynced entry, got %d", len(remaining))
	}

	if remaining[0].BotID != "bot-2" {
		t.Fatalf("unexpected remaining entry: %+v", remaining[0])
	}
}
