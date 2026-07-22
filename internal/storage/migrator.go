package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migration 一条迁移记录
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// RunMigrations 执行所有未应用的迁移
func RunMigrations(db *sql.DB) error {
	if err := ensureMigrationsTable(db); err != nil {
		return fmt.Errorf("创建 migrations 表失败: %w", err)
	}

	applied, err := getAppliedVersions(db)
	if err != nil {
		return fmt.Errorf("读取已应用版本失败: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("加载迁移文件失败: %w", err)
	}

	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("开始事务失败: %w", err)
		}

		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("执行迁移 V%03d %s 失败: %w", m.Version, m.Name, err)
		}

		if _, err := tx.Exec("INSERT INTO migrations (version, name) VALUES (?, ?)",
			m.Version, m.Name); err != nil {
			tx.Rollback()
			return fmt.Errorf("记录迁移版本失败: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移事务失败: %w", err)
		}
	}

	return nil
}

func ensureMigrationsTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	return err
}

func getAppliedVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query("SELECT version FROM migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func loadMigrations() ([]Migration, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		data, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("读取迁移文件 %s 失败: %w", entry.Name(), err)
		}

		m := Migration{
			Name: entry.Name(),
			SQL:  string(data),
		}
		if n, _ := fmt.Sscanf(entry.Name(), "V%03d", &m.Version); n != 1 {
			continue
		}

		migrations = append(migrations, m)
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}
