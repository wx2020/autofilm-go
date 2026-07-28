CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK(role IN ('admin','operator','viewer')),
    enabled       BOOLEAN NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS alerts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    level       TEXT NOT NULL,
    source      TEXT NOT NULL,
    message     TEXT NOT NULL,
    acknowledged BOOLEAN NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);

ALTER TABLE task_runs ADD COLUMN config_uid TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS module_snapshots (
    config_uid TEXT NOT NULL,
    path TEXT NOT NULL,
    size INTEGER NOT NULL,
    modified TEXT NOT NULL,
    sign TEXT NOT NULL DEFAULT '',
    captured_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(config_uid, path)
);

DROP INDEX IF EXISTS idx_sync_tasks_state;
ALTER TABLE sync_tasks RENAME TO sync_tasks_legacy;
CREATE TABLE sync_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_config_id INTEGER NOT NULL DEFAULT 0,
    config_uid TEXT NOT NULL DEFAULT '',
    src_path TEXT NOT NULL,
    dst_path TEXT NOT NULL UNIQUE,
    state TEXT NOT NULL DEFAULT 'pending',
    alist_task_id TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    next_retry_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO sync_tasks(id,sync_config_id,src_path,dst_path,state,alist_task_id,attempts,last_error,next_retry_at,created_at,updated_at)
SELECT id,sync_config_id,src_path,dst_path,state,alist_task_id,attempts,last_error,next_retry_at,created_at,updated_at FROM sync_tasks_legacy;
DROP TABLE sync_tasks_legacy;
CREATE INDEX idx_sync_tasks_state ON sync_tasks(state, next_retry_at);
