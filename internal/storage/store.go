package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Store 数据库 CRUD 操作封装
type Store struct {
	db  *sql.DB
	key []byte // AES-GCM 加密密钥
}

type AppSettings struct {
	Debug        bool   `json:"debug"`
	Timezone     string `json:"timezone"`
	WebEnabled   bool   `json:"web_enabled"`
	WebHost      string `json:"web_host"`
	WebPort      int    `json:"web_port"`
	WebToken     string `json:"web_token,omitempty"`
	AlertWebhook string `json:"alert_webhook,omitempty"`
}

func DefaultAppSettings() AppSettings {
	return AppSettings{Timezone: "Asia/Shanghai", WebEnabled: true, WebHost: "127.0.0.1", WebPort: 8080}
}

func (s *Store) GetAppSettings() (AppSettings, error) {
	settings := DefaultAppSettings()
	rows, err := s.db.Query("SELECT key, value FROM app_settings")
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return settings, err
		}
		values[k] = v
	}
	if v := values["settings"]; v != "" {
		if err := json.Unmarshal([]byte(v), &settings); err != nil {
			return settings, err
		}
	}
	return settings, rows.Err()
}

func (s *Store) SaveAppSettings(settings AppSettings) error {
	if settings.Timezone == "" {
		settings.Timezone = "Asia/Shanghai"
	}
	if settings.WebHost == "" {
		settings.WebHost = "127.0.0.1"
	}
	if settings.WebPort < 1 || settings.WebPort > 65535 {
		return fmt.Errorf("Web 端口必须在 1-65535 之间")
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO app_settings(key,value,updated_at) VALUES('settings',?,CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`, string(data))
	return err
}

func (s *Store) ListModuleConfigs(moduleType string) ([]map[string]interface{}, error) {
	rows, err := s.db.Query("SELECT payload FROM module_configs WHERE module_type=? ORDER BY created_at, id", moduleType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]interface{}{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		plain, err := Decrypt(s.key, payload)
		if err != nil {
			return nil, err
		}
		var cfg map[string]interface{}
		if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
			return nil, err
		}
		result = append(result, cfg)
	}
	return result, rows.Err()
}

// GetModuleConfigCreatedAt returns the database creation time of a module configuration.
func (s *Store) GetModuleConfigCreatedAt(moduleType, configID string) (time.Time, error) {
	var createdAt time.Time
	err := s.db.QueryRow("SELECT created_at FROM module_configs WHERE module_type=? AND cfg_id=?", moduleType, configID).Scan(&createdAt)
	return createdAt, err
}

func (s *Store) SaveModuleConfig(moduleType string, cfg map[string]interface{}) error {
	id := fmt.Sprint(cfg["id"])
	if id == "" || id == "<nil>" {
		return fmt.Errorf("配置 ID 不能为空")
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	payload, err := Encrypt(s.key, string(data))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO module_configs(module_type,cfg_id,payload) VALUES(?,?,?)
		ON CONFLICT(module_type,cfg_id) DO UPDATE SET payload=excluded.payload,updated_at=CURRENT_TIMESTAMP`, moduleType, id, payload)
	return err
}

func (s *Store) DeleteModuleConfig(moduleType, id string) error {
	_, err := s.db.Exec("DELETE FROM module_configs WHERE module_type=? AND cfg_id=?", moduleType, id)
	return err
}

// MigrateModuleType renames a persisted module type while preserving existing data.
func (s *Store) MigrateModuleType(oldType, newType string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM module_configs WHERE module_type=? AND cfg_id IN
		(SELECT cfg_id FROM module_configs WHERE module_type=?)`, newType, oldType); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE module_configs SET module_type=? WHERE module_type=?", newType, oldType); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE task_runs SET module_type=? WHERE module_type=?", newType, oldType); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE alerts SET source=REPLACE(source, ?, ?) WHERE source LIKE ?", oldType+":", newType+":", oldType+":%"); err != nil {
		return err
	}
	return tx.Commit()
}

// NewStore 创建 Store 实例
func NewStore(db *sql.DB, key []byte) *Store {
	return &Store{db: db, key: key}
}

// DB 返回底层数据库连接
func (s *Store) DB() *sql.DB {
	return s.db
}

// QueryRow 便捷方法，直接代理 sql.DB.QueryRow
func (s *Store) QueryRow(query string, args ...interface{}) *sql.Row {
	return s.db.QueryRow(query, args...)
}

var (
	globalStore *Store
	storeMu    sync.RWMutex
)

// SetGlobalStore 设置全局 Store 实例（供模块使用）
func SetGlobalStore(s *Store) {
	storeMu.Lock()
	defer storeMu.Unlock()
	globalStore = s
}

// GlobalStore 获取全局 Store 实例（可能为 nil，表示 DB 不可用）
func GlobalStore() *Store {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return globalStore
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

// ==================== 模块配置查询（返回 map 格式兼容 viper） ====================

// ListAlist2StrmConfigs 从 DB 读取所有 Alist2Strm 配置，返回 viper 兼容的 map 切片
func (s *Store) ListAlist2StrmConfigs() ([]map[string]interface{}, error) {
	return s.listConfigs("alist2strm_configs", s.scanAlist2StrmConfig)
}

// ListAlisyncConfigs 从 DB 读取所有 Alisync 配置
func (s *Store) ListAlisyncConfigs() ([]map[string]interface{}, error) {
	return s.listConfigs("alisync_configs", s.scanAlisyncConfig)
}

// ListAni2AlistConfigs 从 DB 读取所有 Ani2Alist 配置
func (s *Store) ListAni2AlistConfigs() ([]map[string]interface{}, error) {
	return s.listConfigs("ani2alist_configs", s.scanAni2AlistConfig)
}

func (s *Store) listConfigs(table string, scan func(*sql.Rows) (map[string]interface{}, error)) ([]map[string]interface{}, error) {
	query := `SELECT c.*, a.name, a.url, a.username, a.password, a.token, a.public_url
		FROM ` + table + ` c JOIN alist_connections a ON c.connection_id = a.id ORDER BY c.id`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			continue
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *Store) scanAlist2StrmConfig(rows *sql.Rows) (map[string]interface{}, error) {
	var (
		id, cfgID, srcDir, tgtDir, mode, otherExt, syncIgnore, smartJSON, sMode, cron string
		enable, runOnStart, flattenMode, subtitle, image, nfo, overwrite, syncServer  bool
		maxWorkers, maxDownloaders, qpsLimit, connID                                  int
		waitTime                                                                      float64
		createdAt, updatedAt                                                          string
		connName, url, username, password, token, publicURL                           string
	)
	err := rows.Scan(&id, &cfgID, &enable, &runOnStart, &connID,
		&srcDir, &tgtDir, &flattenMode, &subtitle, &image, &nfo,
		&mode, &overwrite, &otherExt, &maxWorkers, &maxDownloaders,
		&waitTime, &syncServer, &syncIgnore, &smartJSON,
		&sMode, &qpsLimit, &cron, &createdAt, &updatedAt,
		&connName, &url, &username, &password, &token, &publicURL)
	if err != nil {
		return nil, err
	}
	m := map[string]interface{}{
		"id": cfgID, "enable": enable, "run_on_start": runOnStart,
		"url": url, "username": username, "password": password, "token": token, "public_url": publicURL,
		"source_dir": srcDir, "target_dir": tgtDir, "flatten_mode": flattenMode,
		"subtitle": subtitle, "image": image, "nfo": nfo, "mode": mode, "overwrite": overwrite,
		"other_ext": otherExt, "max_workers": maxWorkers, "max_downloaders": maxDownloaders,
		"wait_time": waitTime, "sync_server": syncServer, "sync_ignore": syncIgnore,
		"scan_mode": sMode, "qps_limit": qpsLimit, "cron": cron,
	}
	if smartJSON != "" {
		var sp map[string]interface{}
		json.Unmarshal([]byte(smartJSON), &sp)
		if sp != nil {
			m["smart_protection"] = sp
		}
	}
	return m, nil
}

func (s *Store) scanAlisyncConfig(rows *sql.Rows) (map[string]interface{}, error) {
	var (
		id, cfgID, pairsJSON, retryJSON, cron    string
		enable, runOnStart                       bool
		connID                                   int
		createdAt, updatedAt                     string
		connName, url, username, password, token string
	)
	err := rows.Scan(&id, &cfgID, &enable, &runOnStart, &connID,
		&pairsJSON, &retryJSON, &cron, &createdAt, &updatedAt,
		&connName, &url, &username, &password, &token)
	if err != nil {
		return nil, err
	}
	m := map[string]interface{}{
		"id": cfgID, "enable": enable, "run_on_start": runOnStart,
		"url": url, "username": username, "password": password, "token": token, "cron": cron,
	}
	if pairsJSON != "" {
		var pairs []interface{}
		json.Unmarshal([]byte(pairsJSON), &pairs)
		m["pairs"] = pairs
	}
	if retryJSON != "" {
		var retry map[string]interface{}
		json.Unmarshal([]byte(retryJSON), &retry)
		m["retry"] = retry
	}
	return m, nil
}

func (s *Store) scanAni2AlistConfig(rows *sql.Rows) (map[string]interface{}, error) {
	var (
		id, cfgID, tgtDir, srcDomain, rssDomain, keyWord, cron string
		enable, runOnStart, rssUpdate                          bool
		year, month                                            sql.NullInt64
		connID                                                 int
		createdAt, updatedAt                                   string
		connName, url, username, password, token               string
	)
	err := rows.Scan(&id, &cfgID, &enable, &runOnStart, &connID,
		&tgtDir, &rssUpdate, &year, &month, &srcDomain, &rssDomain, &keyWord, &cron,
		&createdAt, &updatedAt,
		&connName, &url, &username, &password, &token)
	if err != nil {
		return nil, err
	}
	m := map[string]interface{}{
		"id": cfgID, "enable": enable, "run_on_start": runOnStart,
		"url": url, "username": username, "password": password, "token": token,
		"target_dir": tgtDir, "rss_update": rssUpdate,
		"src_domain": srcDomain, "rss_domain": rssDomain, "key_word": keyWord, "cron": cron,
	}
	if year.Valid {
		m["year"] = int(year.Int64)
	}
	if month.Valid {
		m["month"] = int(month.Int64)
	}
	return m, nil
}

// ==================== 快照 ====================

// SnapshotEntry 快照条目
type SnapshotEntry struct {
	ConfigID int64
	Path     string
	Size     int64
	Modified string
	Sign     string
}

// SaveSnapshot 批量保存快照（使用 UPDATE + INSERT 避免数据丢失）
func (s *Store) SaveSnapshot(configID int64, entries []SnapshotEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, e := range entries {
		result, err := tx.Exec(`UPDATE alist2strm_snapshots SET size=?, modified=?, sign=?
			WHERE config_id=? AND path=?`, e.Size, e.Modified, e.Sign, configID, e.Path)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			_, err = tx.Exec(`INSERT INTO alist2strm_snapshots (config_id, path, size, modified, sign)
				VALUES (?, ?, ?, ?, ?)`, configID, e.Path, e.Size, e.Modified, e.Sign)
			if err != nil {
				return err
			}
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

func (s *Store) SaveSnapshotByUID(configUID string, entries []SnapshotEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM module_snapshots WHERE config_uid=?", configUID); err != nil {
		return err
	}
	stmt, err := tx.Prepare("INSERT INTO module_snapshots(config_uid,path,size,modified,sign) VALUES(?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, entry := range entries {
		if _, err = stmt.Exec(configUID, entry.Path, entry.Size, entry.Modified, entry.Sign); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LoadSnapshotByUID(configUID string) ([]SnapshotEntry, error) {
	rows, err := s.db.Query("SELECT path,size,modified,sign FROM module_snapshots WHERE config_uid=?", configUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []SnapshotEntry{}
	for rows.Next() {
		var e SnapshotEntry
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
	ConfigUID    string     `json:"config_uid"`
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
	res, err := s.db.Exec(`INSERT INTO sync_tasks (sync_config_id, config_uid, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.SyncConfigID, t.ConfigUID, t.SrcPath, t.DstPath, t.State, t.AlistTaskID, t.Attempts, t.LastError, t.NextRetryAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateSyncTask 更新同步任务
func (s *Store) UpdateSyncTask(t *SyncTaskRow) error {
	_, err := s.db.Exec(`UPDATE sync_tasks SET config_uid=?, state=?, alist_task_id=?, attempts=?, last_error=?, next_retry_at=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`, t.ConfigUID, t.State, t.AlistTaskID, t.Attempts, t.LastError, t.NextRetryAt, t.ID)
	return err
}

// ListSyncTasksByState 按状态列出同步任务
func (s *Store) ListSyncTasksByState(state string) ([]*SyncTaskRow, error) {
	rows, err := s.db.Query(`SELECT id, sync_config_id, config_uid, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at, created_at, updated_at
		FROM sync_tasks WHERE state = ? ORDER BY created_at`, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSyncTasks(rows)
}

// ListPendingRetryTasks 列出待重试任务（state=failed 且 next_retry_at <= now）
func (s *Store) ListPendingRetryTasks() ([]*SyncTaskRow, error) {
	rows, err := s.db.Query(`SELECT id, sync_config_id, config_uid, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at, created_at, updated_at
		FROM sync_tasks WHERE state IN ('failed','dead_letter') ORDER BY next_retry_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSyncTasks(rows)
}

// ListAllSyncTasks 列出所有同步任务
func (s *Store) ListAllSyncTasks() ([]*SyncTaskRow, error) {
	rows, err := s.db.Query(`SELECT id, sync_config_id, config_uid, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at, created_at, updated_at
		FROM sync_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSyncTasks(rows)
}

func scanSyncTasks(rows *sql.Rows) ([]*SyncTaskRow, error) {
	tasks := make([]*SyncTaskRow, 0)
	for rows.Next() {
		t := &SyncTaskRow{}
		if err := rows.Scan(&t.ID, &t.SyncConfigID, &t.ConfigUID, &t.SrcPath, &t.DstPath, &t.State, &t.AlistTaskID,
			&t.Attempts, &t.LastError, &t.NextRetryAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// GetSyncTaskByDstPath 按目标路径查询同步任务
func (s *Store) GetSyncTaskByDstPath(dstPath string) (*SyncTaskRow, error) {
	row := s.db.QueryRow(`SELECT id, sync_config_id, config_uid, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at, created_at, updated_at
		FROM sync_tasks WHERE dst_path = ?`, dstPath)
	t := &SyncTaskRow{}
	if err := row.Scan(&t.ID, &t.SyncConfigID, &t.ConfigUID, &t.SrcPath, &t.DstPath, &t.State, &t.AlistTaskID,
		&t.Attempts, &t.LastError, &t.NextRetryAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) GetSyncTaskByID(id int64) (*SyncTaskRow, error) {
	row := s.db.QueryRow(`SELECT id, sync_config_id, config_uid, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at, created_at, updated_at
		FROM sync_tasks WHERE id = ?`, id)
	t := &SyncTaskRow{}
	if err := row.Scan(&t.ID, &t.SyncConfigID, &t.ConfigUID, &t.SrcPath, &t.DstPath, &t.State, &t.AlistTaskID,
		&t.Attempts, &t.LastError, &t.NextRetryAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

// UpsertSyncTask 更新或插入同步任务（按 dst_path 匹配，使用原子操作避免竞态）
func (s *Store) UpsertSyncTask(t *SyncTaskRow) error {
	_, err := s.db.Exec(`INSERT INTO sync_tasks (sync_config_id, config_uid, src_path, dst_path, state, alist_task_id, attempts, last_error, next_retry_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(dst_path) DO UPDATE SET
			config_uid=excluded.config_uid,
			state=excluded.state,
			alist_task_id=excluded.alist_task_id,
			attempts=excluded.attempts,
			last_error=excluded.last_error,
			next_retry_at=excluded.next_retry_at,
			updated_at=CURRENT_TIMESTAMP`,
		t.SyncConfigID, t.ConfigUID, t.SrcPath, t.DstPath, t.State, t.AlistTaskID, t.Attempts, t.LastError, t.NextRetryAt)
	return err
}

// DeleteSyncTaskByDstPath 按目标路径删除同步任务
func (s *Store) DeleteSyncTaskByDstPath(dstPath string) error {
	_, err := s.db.Exec("DELETE FROM sync_tasks WHERE dst_path = ?", dstPath)
	return err
}

// ==================== 任务运行记录 ====================

// TaskRun 任务运行记录
type TaskRun struct {
	ID            int64      `json:"id"`
	ModuleType    string     `json:"module_type"`
	ConfigID      int64      `json:"config_id"`
	ConfigUID     string     `json:"config_uid"`
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
	res, err := s.db.Exec(`INSERT INTO task_runs (module_type, config_id, config_uid, started_at, status, files_total, files_added, files_modified, files_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ModuleType, r.ConfigID, r.ConfigUID, r.StartedAt, r.Status, r.FilesTotal, r.FilesAdded, r.FilesModified, r.FilesDeleted)
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
	rows, err := s.db.Query(`SELECT id, module_type, config_id, config_uid, started_at, finished_at, status, error_summary,
		files_total, files_added, files_modified, files_deleted FROM task_runs
		WHERE module_type=? ORDER BY started_at DESC LIMIT ?`, moduleType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*TaskRun
	for rows.Next() {
		r := &TaskRun{}
		if err := rows.Scan(&r.ID, &r.ModuleType, &r.ConfigID, &r.ConfigUID, &r.StartedAt, &r.FinishedAt,
			&r.Status, &r.ErrorSummary, &r.FilesTotal, &r.FilesAdded, &r.FilesModified, &r.FilesDeleted); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// GetLatestTaskRun returns the latest run for a module configuration.
func (s *Store) GetLatestTaskRun(moduleType, configUID string) (*TaskRun, error) {
	r := &TaskRun{}
	err := s.db.QueryRow(`SELECT id, module_type, config_id, config_uid, started_at, finished_at, status, error_summary,
		files_total, files_added, files_modified, files_deleted FROM task_runs
		WHERE module_type=? AND config_uid=? ORDER BY started_at DESC LIMIT 1`, moduleType, configUID).Scan(
		&r.ID, &r.ModuleType, &r.ConfigID, &r.ConfigUID, &r.StartedAt, &r.FinishedAt,
		&r.Status, &r.ErrorSummary, &r.FilesTotal, &r.FilesAdded, &r.FilesModified, &r.FilesDeleted)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) ListRecentTaskRuns(limit int) ([]*TaskRun, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id, module_type, config_id, config_uid, started_at, finished_at, status, error_summary,
		files_total, files_added, files_modified, files_deleted FROM task_runs
		ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*TaskRun
	for rows.Next() {
		r := &TaskRun{}
		if err := rows.Scan(&r.ID, &r.ModuleType, &r.ConfigID, &r.ConfigUID, &r.StartedAt, &r.FinishedAt,
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
	case "alistsync", "alissync":
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
