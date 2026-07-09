package storage

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Store 数据库 CRUD 操作封装
type Store struct {
	db *sql.DB
	key []byte // AES-GCM 加密密钥
}

// NewStore 创建 Store 实例
func NewStore(db *sql.DB, key []byte) *Store {
	return &Store{db: db, key: key}
}

// ==================== 连接凭据 ====================

// Connection 连接凭据
type Connection struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	Token     string    `json:"-"`
	PublicURL string    `json:"public_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateConnection 创建连接凭据，密码/令牌自动加密
func (s *Store) CreateConnection(c *Connection) (int64, error) {
	passEnc, _ := Encrypt(s.key, c.Password)
	tokenEnc, _ := Encrypt(s.key, c.Token)
	res, err := s.db.Exec(`INSERT INTO alist_connections (name, url, username, password, token, public_url)
		VALUES (?, ?, ?, ?, ?, ?)`, c.Name, c.URL, c.Username, passEnc, tokenEnc, c.PublicURL)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetConnectionByName 按名称查询连接
func (s *Store) GetConnectionByName(name string) (*Connection, error) {
	row := s.db.QueryRow(`SELECT id, name, url, username, password, token, public_url, created_at, updated_at
		FROM alist_connections WHERE name = ?`, name)
	c := &Connection{}
	var passEnc, tokenEnc string
	if err := row.Scan(&c.ID, &c.Name, &c.URL, &c.Username, &passEnc, &tokenEnc, &c.PublicURL, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.Password, _ = Decrypt(s.key, passEnc)
	c.Token, _ = Decrypt(s.key, tokenEnc)
	return c, nil
}

func (s *Store) FindOrCreateConnection(name, url, username, password, token, publicURL string) (int64, error) {
	existing, err := s.GetConnectionByName(name)
	if err == nil {
		return existing.ID, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	c := &Connection{Name: name, URL: url, Username: username, Password: password, Token: token, PublicURL: publicURL}
	return s.CreateConnection(c)
}

// ==================== 快照 ====================

// SnapshotEntry 快照条目
type SnapshotEntry struct {
	ConfigID  int64
	Path      string
	Size      int64
	Modified  string
	Sign      string
}

// SaveSnapshot 批量保存快照（事务替换）
func (s *Store) SaveSnapshot(configID int64, entries []SnapshotEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM alist2strm_snapshots WHERE config_id = ?", configID); err != nil {
		return err
	}

	stmt, err := tx.Prepare("INSERT INTO alist2strm_snapshots (config_id, path, size, modified, sign) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.Exec(configID, e.Path, e.Size, e.Modified, e.Sign); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// LoadSnapshot 加载快照
func (s *Store) LoadSnapshot(configID int64) ([]SnapshotEntry, error) {
	rows, err := s.db.Query("SELECT path, size, modified, sign FROM alist2strm_snapshots WHERE config_id = ?", configID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []SnapshotEntry
	for rows.Next() {
		var e SnapshotEntry
		e.ConfigID = configID
		if err := rows.Scan(&e.Path, &e.Size, &e.Modified, &e.Sign); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ==================== 同步任务 ====================

// SyncTaskRow 同步任务行
type SyncTaskRow struct {
	ID           int64      `json:"id"`
	SyncConfigID int64      `json:"sync_config_id"`
	SrcPath      string     `json:"src_path"`
	DstPath      string     `json:"dst_path"`
	State        string     `json:"state"`
	AlistTaskID  string     `json:"alist_task_id"`
	Attempts     int        `json:"attempts"`
	LastError    string     `json:"last_error"`
	NextRetryAt  *time.Time `json:"next_retry_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// CreateSyncTask 创建同步任务
func (s *Store) CreateSyncTask(t *SyncTaskRow) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO sync_tasks (sync_config_id, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.SyncConfigID, t.SrcPath, t.DstPath, t.State, t.AlistTaskID, t.Attempts, t.LastError, t.NextRetryAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateSyncTask 更新同步任务
func (s *Store) UpdateSyncTask(t *SyncTaskRow) error {
	_, err := s.db.Exec(`UPDATE sync_tasks SET state=?, alist_task_id=?, attempts=?, last_error=?, next_retry_at=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`, t.State, t.AlistTaskID, t.Attempts, t.LastError, t.NextRetryAt, t.ID)
	return err
}

// ListSyncTasksByState 按状态列出同步任务
func (s *Store) ListSyncTasksByState(state string) ([]*SyncTaskRow, error) {
	rows, err := s.db.Query(`SELECT id, sync_config_id, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at, created_at, updated_at
		FROM sync_tasks WHERE state = ? ORDER BY created_at`, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSyncTasks(rows)
}

// ListPendingRetryTasks 列出待重试任务（state=failed 且 next_retry_at <= now）
func (s *Store) ListPendingRetryTasks() ([]*SyncTaskRow, error) {
	rows, err := s.db.Query(`SELECT id, sync_config_id, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at, created_at, updated_at
		FROM sync_tasks WHERE state IN ('failed','dead_letter') ORDER BY next_retry_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSyncTasks(rows)
}

// ListAllSyncTasks 列出所有同步任务
func (s *Store) ListAllSyncTasks() ([]*SyncTaskRow, error) {
	rows, err := s.db.Query(`SELECT id, sync_config_id, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at, created_at, updated_at
		FROM sync_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSyncTasks(rows)
}

func scanSyncTasks(rows *sql.Rows) ([]*SyncTaskRow, error) {
	var tasks []*SyncTaskRow
	for rows.Next() {
		t := &SyncTaskRow{}
		if err := rows.Scan(&t.ID, &t.SyncConfigID, &t.SrcPath, &t.DstPath, &t.State, &t.AlistTaskID,
			&t.Attempts, &t.LastError, &t.NextRetryAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ==================== 任务运行记录 ====================

// TaskRun 任务运行记录
type TaskRun struct {
	ID            int64      `json:"id"`
	ModuleType    string     `json:"module_type"`
	ConfigID      int64      `json:"config_id"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	Status        string     `json:"status"`
	ErrorSummary  string     `json:"error_summary"`
	FilesTotal    int        `json:"files_total"`
	FilesAdded    int        `json:"files_added"`
	FilesModified int        `json:"files_modified"`
	FilesDeleted  int        `json:"files_deleted"`
}

// CreateTaskRun 创建运行记录
func (s *Store) CreateTaskRun(r *TaskRun) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO task_runs (module_type, config_id, started_at, status, files_total, files_added, files_modified, files_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ModuleType, r.ConfigID, r.StartedAt, r.Status, r.FilesTotal, r.FilesAdded, r.FilesModified, r.FilesDeleted)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishTaskRun 完成运行记录
func (s *Store) FinishTaskRun(id int64, status, errSummary string, filesTotal, filesAdded, filesModified, filesDeleted int) error {
	_, err := s.db.Exec(`UPDATE task_runs SET status=?, error_summary=?, finished_at=CURRENT_TIMESTAMP,
		files_total=?, files_added=?, files_modified=?, files_deleted=? WHERE id=?`,
		status, errSummary, filesTotal, filesAdded, filesModified, filesDeleted, id)
	return err
}

// ListTaskRuns 列出运行记录
func (s *Store) ListTaskRuns(moduleType string, limit int) ([]*TaskRun, error) {
	rows, err := s.db.Query(`SELECT id, module_type, config_id, started_at, finished_at, status, error_summary,
		files_total, files_added, files_modified, files_deleted FROM task_runs
		WHERE module_type=? ORDER BY started_at DESC LIMIT ?`, moduleType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*TaskRun
	for rows.Next() {
		r := &TaskRun{}
		if err := rows.Scan(&r.ID, &r.ModuleType, &r.ConfigID, &r.StartedAt, &r.FinishedAt,
			&r.Status, &r.ErrorSummary, &r.FilesTotal, &r.FilesAdded, &r.FilesModified, &r.FilesDeleted); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ==================== 模块配置从 yaml 导入到 DB ====================

// ImportConfigMap 将 yaml 配置条目写入 DB（返回 connection_id）
// 用于首次启动时从 config.yaml 导入
func (s *Store) ImportConfigMap(moduleType string, cfg map[string]interface{}) error {
	url, _ := cfg["url"].(string)
	username, _ := cfg["username"].(string)
	password, _ := cfg["password"].(string)
	token, _ := cfg["token"].(string)
	publicURL, _ := cfg["public_url"].(string)
	idField, _ := cfg["id"].(string)

	connID, err := s.FindOrCreateConnection(idField, url, username, password, token, publicURL)
	if err != nil {
		return err
	}

	switch moduleType {
	case "alist2strm":
		return s.importAlist2Strm(cfg, connID)
	case "ani2alist":
		return s.importAni2Alist(cfg, connID)
	case "alissync":
		return s.importAlissync(cfg, connID)
	}
	return nil
}

func (s *Store) importAlist2Strm(cfg map[string]interface{}, connID int64) error {
	smartProtection, _ := json.Marshal(cfg["smart_protection"])
	spJSON := string(smartProtection)
	if spJSON == "null" {
		spJSON = ""
	}

	_, err := s.db.Exec(`INSERT OR IGNORE INTO alist2strm_configs
		(cfg_id, enable, run_on_start, connection_id, source_dir, target_dir, flatten_mode,
		 subtitle, image, nfo, mode, overwrite, other_ext, max_workers, max_downloaders,
		 wait_time, sync_server, sync_ignore, smart_protection_json, scan_mode, qps_limit, cron)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		strVal(cfg, "id"), boolVal(cfg, "enable", true), boolVal(cfg, "run_on_start", false),
		connID, strVal(cfg, "source_dir"), strVal(cfg, "target_dir"), boolVal(cfg, "flatten_mode", false),
		boolVal(cfg, "subtitle", false), boolVal(cfg, "image", false), boolVal(cfg, "nfo", false),
		strVal(cfg, "mode"), boolVal(cfg, "overwrite", false), strVal(cfg, "other_ext"),
		intVal(cfg, "max_workers", 50), intVal(cfg, "max_downloaders", 5),
		floatVal(cfg, "wait_time", 0), boolVal(cfg, "sync_server", false), strVal(cfg, "sync_ignore"),
		spJSON, strVal(cfg, "scan_mode"), intVal(cfg, "qps_limit", 10), strVal(cfg, "cron"))
	return err
}

func (s *Store) importAni2Alist(cfg map[string]interface{}, connID int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO ani2alist_configs
		(cfg_id, enable, run_on_start, connection_id, target_dir, rss_update,
		 year, month, src_domain, rss_domain, key_word, cron)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		strVal(cfg, "id"), boolVal(cfg, "enable", true), boolVal(cfg, "run_on_start", false),
		connID, strVal(cfg, "target_dir"), boolVal(cfg, "rss_update", true),
		intOrNil(cfg, "year"), intOrNil(cfg, "month"),
		strVal(cfg, "src_domain"), strVal(cfg, "rss_domain"), strVal(cfg, "key_word"), strVal(cfg, "cron"))
	return err
}

func (s *Store) importAlissync(cfg map[string]interface{}, connID int64) error {
	pairs, _ := json.Marshal(cfg["pairs"])
	retry, _ := json.Marshal(cfg["retry"])

	_, err := s.db.Exec(`INSERT OR IGNORE INTO alisync_configs
		(cfg_id, enable, run_on_start, connection_id, pairs_json, retry_json, cron)
		VALUES (?,?,?,?,?,?,?)`,
		strVal(cfg, "id"), boolVal(cfg, "enable", true), boolVal(cfg, "run_on_start", false),
		connID, string(pairs), string(retry), strVal(cfg, "cron"))
	return err
}

// 辅助类型转换
func strVal(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func boolVal(m map[string]interface{}, k string, def bool) bool {
	if v, ok := m[k]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

func intVal(m map[string]interface{}, k string, def int) int {
	if v, ok := m[k]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return def
}

func floatVal(m map[string]interface{}, k string, def float64) float64 {
	if v, ok := m[k]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return def
}

func intOrNil(m map[string]interface{}, k string) interface{} {
	if v, ok := m[k]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return nil
}
