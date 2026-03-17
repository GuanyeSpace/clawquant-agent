package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"sync"
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

	entries, err := store.GetUnsyncedLogs(100)
	if err != nil {
		t.Fatalf("GetUnsyncedLogs returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 unsynced entries, got %d", len(entries))
	}

	if entries[0].BotID != "bot-1" || entries[1].BotID != "bot-2" {
		t.Fatalf("unexpected entry order: %+v", entries)
	}

	if err := store.MarkLogsSynced([]int64{entries[0].ID}); err != nil {
		t.Fatalf("MarkLogsSynced returned error: %v", err)
	}

	remaining, err := store.GetUnsyncedLogs(100)
	if err != nil {
		t.Fatalf("GetUnsyncedLogs returned error: %v", err)
	}

	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining unsynced entry, got %d", len(remaining))
	}

	if remaining[0].BotID != "bot-2" {
		t.Fatalf("unexpected remaining entry: %+v", remaining[0])
	}
}

func TestStoreStorageCRUD(t *testing.T) {
	store, _, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer store.Close()

	if err := store.SetStorage("bot-1", "state", `{"count":1}`); err != nil {
		t.Fatalf("SetStorage returned error: %v", err)
	}

	if err := store.SetStorage("bot-1", "state", `{"count":2}`); err != nil {
		t.Fatalf("SetStorage overwrite returned error: %v", err)
	}

	value, err := store.GetStorage("bot-1", "state")
	if err != nil {
		t.Fatalf("GetStorage returned error: %v", err)
	}

	if value != `{"count":2}` {
		t.Fatalf("unexpected storage value: %q", value)
	}

	if err := store.DeleteBotStorage("bot-1"); err != nil {
		t.Fatalf("DeleteBotStorage returned error: %v", err)
	}

	if _, err := store.GetStorage("bot-1", "state"); err == nil || err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}
}

func TestStoreDeleteBotLogs(t *testing.T) {
	store, _, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer store.Close()

	if err := store.SaveLog("bot-1", "info", "a", "{}"); err != nil {
		t.Fatalf("SaveLog returned error: %v", err)
	}
	if err := store.SaveLog("bot-2", "info", "b", "{}"); err != nil {
		t.Fatalf("SaveLog returned error: %v", err)
	}

	if err := store.DeleteBotLogs("bot-1"); err != nil {
		t.Fatalf("DeleteBotLogs returned error: %v", err)
	}

	entries, err := store.GetUnsyncedLogs(100)
	if err != nil {
		t.Fatalf("GetUnsyncedLogs returned error: %v", err)
	}

	if len(entries) != 1 || entries[0].BotID != "bot-2" {
		t.Fatalf("unexpected logs after delete: %+v", entries)
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	store, _, err := OpenSQLite(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer store.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := "key-" + strconv.Itoa(i)
			if err := store.SetStorage("bot-1", key, strconv.Itoa(i)); err != nil {
				t.Errorf("SetStorage returned error: %v", err)
			}
			if err := store.SaveLog("bot-1", "info", key, "{}"); err != nil {
				t.Errorf("SaveLog returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	entries, err := store.GetUnsyncedLogs(100)
	if err != nil {
		t.Fatalf("GetUnsyncedLogs returned error: %v", err)
	}

	if len(entries) != 20 {
		t.Fatalf("expected 20 log entries, got %d", len(entries))
	}

	for i := 0; i < 20; i++ {
		value, err := store.GetStorage("bot-1", "key-"+strconv.Itoa(i))
		if err != nil {
			t.Fatalf("GetStorage returned error: %v", err)
		}
		if value != strconv.Itoa(i) {
			t.Fatalf("unexpected stored value for key-%d: %q", i, value)
		}
	}
}
