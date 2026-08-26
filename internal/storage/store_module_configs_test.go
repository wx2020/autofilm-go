package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"
)

func setupModuleConfigStore(t *testing.T) (*Store, func()) {
	tmpFile, err := os.CreateTemp("", "test_module_configs_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	db, err := sql.Open("sqlite", tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatal(err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS module_configs (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		module_type TEXT NOT NULL,
		cfg_id      TEXT NOT NULL,
		payload     TEXT NOT NULL,
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(module_type, cfg_id)
	);
	CREATE TABLE IF NOT EXISTS users (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		username      TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role          TEXT NOT NULL CHECK(role IN ('admin','operator','viewer')),
		enabled       BOOLEAN NOT NULL DEFAULT 1,
		created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS app_settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatal(err)
	}

	key := make([]byte, 32)
	store := NewStore(db, key)
	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}
	return store, cleanup
}

func TestUpdateModuleConfigField(t *testing.T) {
	store, cleanup := setupModuleConfigStore(t)
	defer cleanup()

	cfg := map[string]interface{}{"id": "task-a", "enable": true, "cron": "0 0 * * * *", "url": "http://localhost:5244"}
	if err := store.SaveModuleConfig("alist2strm", cfg); err != nil {
		t.Fatalf("SaveModuleConfig: %v", err)
	}

	if err := store.UpdateModuleConfigField("alist2strm", "task-a", "enable", false); err != nil {
		t.Fatalf("UpdateModuleConfigField: %v", err)
	}

	list, err := store.ListModuleConfigs("alist2strm")
	if err != nil {
		t.Fatalf("ListModuleConfigs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expect 1 config, got %d", len(list))
	}
	if enabled, ok := list[0]["enable"].(bool); !ok || enabled {
		t.Fatalf("enable = %v, want false", list[0]["enable"])
	}
	if list[0]["cron"] != "0 0 * * * *" {
		t.Fatalf("其他字段不应被修改: %v", list[0])
	}
}

// TestListModuleConfigsSkipsCorruptEntries 验证单条损坏配置被跳过而非整体失败
func TestListModuleConfigsSkipsCorruptEntries(t *testing.T) {
	store, cleanup := setupModuleConfigStore(t)
	defer cleanup()

	good := map[string]interface{}{"id": "good", "enable": true}
	if err := store.SaveModuleConfig("filemove", good); err != nil {
		t.Fatalf("SaveModuleConfig: %v", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO module_configs(module_type,cfg_id,payload) VALUES('filemove','bad','not-encrypted')`); err != nil {
		t.Fatalf("insert corrupt payload: %v", err)
	}

	list, err := store.ListModuleConfigs("filemove")
	if err != nil {
		t.Fatalf("坏配置不应导致整体失败: %v", err)
	}
	if len(list) != 1 || list[0]["id"] != "good" {
		t.Fatalf("应只返回完好配置: %v", list)
	}
}

// TestBackupCoversAllModuleTypes 验证备份导出/恢复包含 filemove
func TestBackupCoversAllModuleTypes(t *testing.T) {
	store, cleanup := setupModuleConfigStore(t)
	defer cleanup()

	for _, typ := range backupModuleTypes {
		cfg := map[string]interface{}{"id": "cfg-" + typ, "enable": true}
		if err := store.SaveModuleConfig(typ, cfg); err != nil {
			t.Fatalf("SaveModuleConfig(%s): %v", typ, err)
		}
	}

	backup, err := store.ExportBackup()
	if err != nil {
		t.Fatalf("ExportBackup: %v", err)
	}
	raw, _ := json.Marshal(backup.ModuleConfigs)
	for _, typ := range backupModuleTypes {
		if _, ok := backup.ModuleConfigs[typ]; !ok {
			t.Fatalf("备份缺少模块类型 %s: %s", typ, raw)
		}
		if len(backup.ModuleConfigs[typ]) != 1 {
			t.Fatalf("模块 %s 应包含 1 条配置", typ)
		}
	}

	// 模拟恢复到全新库
	fresh, freshCleanup := setupModuleConfigStore(t)
	defer freshCleanup()
	if err := fresh.ImportBackup(backup); err != nil {
		t.Fatalf("ImportBackup: %v", err)
	}
	list, err := fresh.ListModuleConfigs("filemove")
	if err != nil || len(list) != 1 {
		t.Fatalf("恢复后 filemove 配置丢失: n=%d err=%v", len(list), err)
	}
}

func TestBootstrapFirstUserOnlyOnce(t *testing.T) {
	store, cleanup := setupModuleConfigStore(t)
	defer cleanup()

	u1, err := store.BootstrapFirstUser("admin", "password123")
	if err != nil {
		t.Fatalf("首次 bootstrap 失败: %v", err)
	}
	if u1.Role != "admin" {
		t.Fatalf("role = %s, want admin", u1.Role)
	}
	if _, err := store.BootstrapFirstUser("admin2", "password456"); err == nil {
		t.Fatal("第二次 bootstrap 应当失败")
	}
	count, err := store.UserCount()
	if err != nil || count != 1 {
		t.Fatalf("用户数 = %d (err=%v), want 1", count, err)
	}
}
