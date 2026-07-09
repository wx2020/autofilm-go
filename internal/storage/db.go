package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	db   *sql.DB
	once sync.Once
	mu   sync.Mutex
)

// Config DB 配置
type Config struct {
	Path         string
	MaxOpenConns int
}

// DefaultConfig 返回默认 DB 配置
func DefaultConfig(dataDir string) *Config {
	return &Config{
		Path:         filepath.Join(dataDir, "autofilm.db"),
		MaxOpenConns: 1,
	}
}

// InitDB 初始化 SQLite 数据库（WAL 模式，单 writer）
func InitDB(cfg *Config) (*sql.DB, error) {
	mu.Lock()
	defer mu.Unlock()

	if db != nil {
		return db, nil
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	conn, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// WAL 模式 + 忙等待超时
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-8000",
	}
	for _, p := range pragmas {
		if _, err := conn.Exec(p); err != nil {
			conn.Close()
			return nil, fmt.Errorf("设置 PRAGMA 失败 %s: %w", p, err)
		}
	}

	conn.SetMaxOpenConns(cfg.MaxOpenConns)
	conn.SetMaxIdleConns(1)

	db = conn
	return db, nil
}

// GetDB 获取全局数据库连接
func GetDB() *sql.DB {
	mu.Lock()
	defer mu.Unlock()
	return db
}
