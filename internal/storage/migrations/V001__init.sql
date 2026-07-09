CREATE TABLE IF NOT EXISTS alist_connections (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    url         TEXT    NOT NULL,
    username    TEXT    NOT NULL DEFAULT '',
    password    TEXT    NOT NULL DEFAULT '',
    token       TEXT    NOT NULL DEFAULT '',
    public_url  TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS alist2strm_configs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    cfg_id        TEXT    NOT NULL UNIQUE,
    enable        BOOLEAN NOT NULL DEFAULT 1,
    run_on_start  BOOLEAN NOT NULL DEFAULT 0,
    connection_id INTEGER NOT NULL REFERENCES alist_connections(id) ON DELETE RESTRICT,
    source_dir    TEXT    NOT NULL,
    target_dir    TEXT    NOT NULL,
    flatten_mode  BOOLEAN NOT NULL DEFAULT 0,
    subtitle      BOOLEAN NOT NULL DEFAULT 0,
    image         BOOLEAN NOT NULL DEFAULT 0,
    nfo           BOOLEAN NOT NULL DEFAULT 0,
    mode          TEXT    NOT NULL DEFAULT 'AlistURL',
    overwrite     BOOLEAN NOT NULL DEFAULT 0,
    other_ext     TEXT    NOT NULL DEFAULT '',
    max_workers   INTEGER NOT NULL DEFAULT 50,
    max_downloaders INTEGER NOT NULL DEFAULT 5,
    wait_time     REAL    NOT NULL DEFAULT 0,
    sync_server   BOOLEAN NOT NULL DEFAULT 0,
    sync_ignore   TEXT    NOT NULL DEFAULT '',
    smart_protection_json TEXT,
    scan_mode     TEXT    NOT NULL DEFAULT 'incremental',
    qps_limit     INTEGER NOT NULL DEFAULT 10,
    cron          TEXT    NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ani2alist_configs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    cfg_id        TEXT    NOT NULL UNIQUE,
    enable        BOOLEAN NOT NULL DEFAULT 1,
    run_on_start  BOOLEAN NOT NULL DEFAULT 0,
    connection_id INTEGER NOT NULL REFERENCES alist_connections(id),
    target_dir    TEXT    NOT NULL,
    rss_update    BOOLEAN NOT NULL DEFAULT 1,
    year          INTEGER,
    month         INTEGER,
    src_domain    TEXT    NOT NULL,
    rss_domain    TEXT    NOT NULL,
    key_word      TEXT    NOT NULL DEFAULT '',
    cron          TEXT    NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS alisync_configs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    cfg_id        TEXT    NOT NULL UNIQUE,
    enable        BOOLEAN NOT NULL DEFAULT 1,
    run_on_start  BOOLEAN NOT NULL DEFAULT 0,
    connection_id INTEGER NOT NULL REFERENCES alist_connections(id),
    pairs_json    TEXT    NOT NULL,
    retry_json    TEXT    NOT NULL,
    cron          TEXT    NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS alist2strm_snapshots (
    config_id   INTEGER NOT NULL REFERENCES alist2strm_configs(id) ON DELETE CASCADE,
    path        TEXT    NOT NULL,
    size        INTEGER NOT NULL,
    modified    TEXT    NOT NULL,
    sign        TEXT    NOT NULL DEFAULT '',
    captured_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (config_id, path)
);
CREATE INDEX IF NOT EXISTS idx_snapshots_captured ON alist2strm_snapshots(config_id, captured_at);

CREATE TABLE IF NOT EXISTS sync_tasks (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_config_id INTEGER NOT NULL REFERENCES alisync_configs(id) ON DELETE CASCADE,
    src_path       TEXT    NOT NULL,
    dst_path       TEXT    NOT NULL,
    state          TEXT    NOT NULL DEFAULT 'pending',
    alist_task_id  TEXT    NOT NULL DEFAULT '',
    attempts       INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT    NOT NULL DEFAULT '',
    next_retry_at  DATETIME,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sync_tasks_state ON sync_tasks(state, next_retry_at);

CREATE TABLE IF NOT EXISTS task_runs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    module_type    TEXT    NOT NULL,
    config_id      INTEGER NOT NULL,
    started_at     DATETIME NOT NULL,
    finished_at    DATETIME,
    status         TEXT    NOT NULL DEFAULT 'running',
    error_summary  TEXT    NOT NULL DEFAULT '',
    files_total    INTEGER NOT NULL DEFAULT 0,
    files_added    INTEGER NOT NULL DEFAULT 0,
    files_modified INTEGER NOT NULL DEFAULT 0,
    files_deleted  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_task_runs_module ON task_runs(module_type, config_id, started_at DESC);

CREATE TABLE IF NOT EXISTS migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
