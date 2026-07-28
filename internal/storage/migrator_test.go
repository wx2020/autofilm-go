package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err = RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return NewStore(db, make([]byte, 32))
}

func TestMigrationsAreReplaySafe(t *testing.T) {
	s := testStore(t)
	if err := RunMigrations(s.db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"module_configs", "users", "sessions", "alerts", "task_runs"} {
		var name string
		if err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
}

func TestModuleConfigEncryptedRoundTrip(t *testing.T) {
	s := testStore(t)
	cfg := map[string]interface{}{"id": "demo", "token": "top-secret", "cron": "0 0 * * * *"}
	if err := s.SaveModuleConfig("alist2strm", cfg); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.db.QueryRow("SELECT payload FROM module_configs").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" || raw == "top-secret" {
		t.Fatal("payload was not encrypted")
	}
	list, err := s.ListModuleConfigs("alist2strm")
	if err != nil || len(list) != 1 || list[0]["token"] != "top-secret" {
		t.Fatalf("roundtrip failed: %#v %v", list, err)
	}
}

func TestUsersSessionsAndBackupRestore(t *testing.T) {
	s := testStore(t)
	user, err := s.CreateUser("admin", "strong-password", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate("admin", "wrong-password"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err = s.Authenticate("admin", "strong-password"); err != nil {
		t.Fatal(err)
	}
	token, err := s.CreateSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sessionUser, err := s.UserBySession(token)
	if err != nil || sessionUser.Role != "admin" {
		t.Fatalf("session user=%#v err=%v", sessionUser, err)
	}

	cfg := map[string]interface{}{"id": "backup-demo", "cron": "0 0 * * * *"}
	if err = s.SaveModuleConfig("ani2alist", cfg); err != nil {
		t.Fatal(err)
	}
	backup, err := s.ExportBackup()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteModuleConfig("ani2alist", "backup-demo"); err != nil {
		t.Fatal(err)
	}
	if err = s.ImportBackup(backup); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListModuleConfigs("ani2alist")
	if err != nil || len(list) != 1 || list[0]["id"] != "backup-demo" {
		t.Fatalf("restored=%#v err=%v", list, err)
	}
}
