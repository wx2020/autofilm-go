CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS module_configs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    module_type TEXT NOT NULL,
    cfg_id      TEXT NOT NULL,
    payload     TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(module_type, cfg_id)
);
CREATE INDEX IF NOT EXISTS idx_module_configs_type ON module_configs(module_type, id);
