# AutoFilm-Go 新功能 ToDo

> 制定日期：2026-06-28
> 目标：在现有 Alist2Strm / Ani2Alist / LibraryPoster 之上，新增三项能力

## 0. 项目现状摘要（用于排期参考）

| 模块 | 路径 | 现状 |
|---|---|---|
| 入口/调度 | `cmd/autofilm/main.go` | robfig/cron 调度 + RunOnStart；纯配置文件驱动，无运行时改写 |
| 配置 | `internal/core/config.go` | Viper + `config.yaml`；无运行时写入与重载广播 |
| Alist 客户端 | `pkg/alist/client.go` | 封装 `fs/list`、`fs/get`、`admin/storage/*`；`FSList` 内为每个文件再发 `fs/get` 拿 `RawURL`（**全量递归 + 每文件 1 次额外请求 → 触发"封号"主因**） |
| Alist2Strm | `internal/modules/alist2strm/` | `IterPath` 全量遍历；`sync_server` 靠 SmartProtection 防误删 |
| Web UI | 无 | 完全缺失 |

Alist v3 已规划使用、但目前未封装的 API：
- `/api/fs/list` / `/api/fs/get` / `/api/fs/mkdir`
- `/api/fs/put`（异步任务，body: `{path, files:[{path,url}]}`，返回 `task_id`）
- `/api/admin/task/{task_info, task_done, cancel, retry}`

---

## 1. 功能一：使用 Alist API 增量检测变更（降低封号风险）

### 1.1 目标
把当前"每次定时都全量递归 + 每文件 fs/get 拿 RawURL"的扫描方式，改为"快照 diff，仅对变更文件做动作"，并对单实例请求做 QPS 限流。

### 1.2 设计要点
1. **快照存储**：每个 Alist2Strm 条目单独保存 `config/cache/alist2strm_<id>.json`
   ```json
   {
     "updated": "2026-06-28T10:00:00Z",
     "files": {
       "<full_path>": {"size": 123, "modified": "...", "sign": "..."}
     }
   }
   ```
2. **扫描模式**（新增配置 `scan_mode: full | incremental`，默认 `incremental`）：
   - **Full**：保留现有 `IterPath` 行为（首次运行 / 异常时回退）。
   - **Incremental**：
     - 递归 `fs/list`（**不再为每个文件立刻发 `fs/get`**），把 `size/modified/sign` 写入新快照。
     - 与旧快照 diff 出三类：`new` / `modified`（size 或 modified 变化）/ `deleted`。
     - **只对 `new/modified` 按需调 `fs/get` 取 RawURL**（典型 1% 量级）。
     - `deleted` 进入待清理集合，交给现有 `StrmProtectionManager` 处理。
3. **降级与安全**：
   - 首次无快照 → 自动回退一次 full 扫描并写入快照。
   - 快照文件损坏 → 记 warn 并重建。
   - 单 Alist 任务令牌桶限流：`QPS = min(max_workers/2, 10)`，使用 `golang.org/x/time/rate`。
4. **可观测**：扫描结束输出 `全量/新增/修改/删除/请求数` 计数与耗时。

### 1.3 代码改动清单
- [ ] `internal/modules/alist2strm/snapshot.go`（新增）：快照读写与 diff
- [ ] `internal/modules/alist2strm/alist2strm.go`：在 `Run` 中根据 `scan_mode` 走不同路径
- [ ] `internal/modules/alist2strm/iter_incremental.go`（新增）：仅递归 list，不调 fs/get
- [ ] `pkg/alist/client.go`：增加 `FSListLight`（不发起 fs/get）和 `RateLimiter` 字段
- [ ] `internal/core/config.go`：扩展 `Config.ScanMode`、`QPS`、`CacheDir`
- [ ] `cmd/autofilm/main.go`：`parseAlist2StrmConfig` 解析新字段
- [ ] `config/config.yaml` 默认模板更新

### 1.4 验收
- [ ] 首次运行后产生快照文件
- [ ] 二次运行无变更时，对源目录的请求数 ≤ 文件数（理论值）
- [ ] 新增/修改/删除文件能被正确处理
- [ ] 单元测试：snapshot diff 边界（同名换 size、换 mtime、目录删除）

---

## 2. 功能二：Alist 多文件夹同步 + 失败守护重试

### 2.1 目标
新增独立模块 `internal/modules/alissync/`，支持"源目录 → 目标目录"的双向/单向同步，**同步失败不丢任务**——由守护协程按指数退避自动重试。

### 2.2 设计要点
1. **配置**（新增 `AlistSyncList` 段）：
   ```yaml
   AlistSyncList:
     - id: "cloud-a-to-b"
       enable: true
       url: "http://localhost:5244"
       username: ""
       password: ""
       token: ""
       pairs:
         - src: "/aliyun/Movies"
           dst: "/backup/Movies"
           delete_src: false       # 是否在同步后删除源
           overwrite: "never|always|if_newer"
       cron: "0 */2 * * *"
       retry:
         max_attempts: 10
         backoff: "expo"           # 30s, 1m, 2m, 4m, 8m, 16m, 30m
         jitter: 0.2
   ```
2. **同步流程**（复用 `pkg/alist`，新增 `put/task` 系列封装）：
   - `fs/list(src)` → 计算需要的目标文件集合
   - `fs/mkdir -p` 在目标建立目录树
   - 对每个需上传文件：`fs/put`（`{dst_path, files:[{path: name, url: src_raw_url}]}`）→ 返回 `task_id`
   - 守护协程轮询 `/api/admin/task/task_info?id=...`：
     - `succeeded` → 入账
     - `failed/canceled` → 入重试队列
3. **守护重试**（独立 goroutine，启动时从磁盘恢复）：
   - 持久化：`config/sync_queue/<task_id>.json`
   - 指数退避 + 抖动；超过 `max_attempts` 标记 `dead_letter`
   - 即便没有 cron 触发，重试 goroutine 也持续运行；Alist 重启/网络抖动后自动恢复
4. **删除同步（可选）**：当 `delete_src=true` 且目标已存在，使用 `fs/remove` 清理源文件

### 2.3 代码改动清单
- [ ] `pkg/alist/client.go`：新增 `FSPut`、`TaskInfo`、`TaskCancel`、`TaskRetry`
- [ ] `internal/modules/alissync/sync.go`（新增）：同步主流程
- [ ] `internal/modules/alissync/daemon.go`（新增）：守护重试协程
- [ ] `internal/modules/alissync/queue.go`（新增）：重试队列磁盘持久化
- [ ] `internal/modules/alissync/overwrite.go`（新增）：覆盖策略判定
- [ ] `cmd/autofilm/main.go`：新增 `addAlistSyncJobs`
- [ ] `internal/core/config.go`：注册 `AlistSyncList` 段
- [ ] `config/config.yaml` 默认模板更新

### 2.4 验收
- [ ] 同步 10 个文件，记录任务 `task_id` 与目标路径
- [ ] 模拟 Alist 临时不可用（关掉 Alist 容器 1 分钟）→ 重试队列积压 → 恢复后自动重试成功
- [ ] 超过 `max_attempts` 的任务进入 `dead_letter` 并告警
- [ ] 单元测试：退避时间计算、队列序列化、覆盖策略

---

## 3. 功能三：前端控制页面

### 3.1 目标
提供浏览器端配置与运维入口：可视化编辑配置、查看模块状态、手动触发任务、查看重试队列、实时日志。

### 3.2 设计要点
1. **后端**（新增 `internal/web/`，路由用 `github.com/go-chi/chi/v5`，轻量零分配）：
   ```
   GET  /api/health
   GET  /api/config                  # 当前配置（敏感字段遮蔽）
   PUT  /api/config                  # 全量/增量保存 → 原子写 config.yaml → ReloadConfig
   GET  /api/modules                 # 模块条目 + 启用/cron/下次运行/last_run/last_error
   POST /api/modules/:type/:id/run   # 手动触发
   POST /api/modules/:type/:id/toggle
   GET  /api/alist/test              # 连通性 + fs/list 一次
   GET  /api/sync/queue              # 重试队列
   POST /api/sync/queue/retry/:tid
   GET  /api/logs?lines=500&level=
   WS   /api/logs/stream             # 实时日志（logrus hook → chan）
   ```
2. **鉴权**（`Settings.Web`）：
   ```yaml
   Settings:
     Web:
       enabled: true
       host: "0.0.0.0"
       port: 8080
       token: ""        # 空 → 仅本机访问
   ```
3. **热重载**：Web 写配置后 → 原子写 yaml → `SettingManager.Save + ReloadConfig` → 通过内存 `chan struct{}` 通知 `main` 重建 cron 任务。
4. **前端**（`webui/`，Vue 3 + Vite + Bootstrap 5）：
   - 路由：`/` Overview、`/alist2strm`、`/ani2alist`、`/libraryposter`、`/sync`、`/settings`、`/logs`
   - 关键组件：
     - `ModuleCard`：id、enable 开关、cron、下次执行、立即运行、上次结果
     - `ConfigForm`：Alist2Strm 嵌套 `smart_protection` 表单、增量模式开关、QPS 限制
     - `SyncQueueTable`：实时显示重试队列，可手动重试/取消
     - `LogViewer`：虚拟滚动 + WS 实时推送
   - 样式：使用 **Bootstrap 5**（通过 `bootstrap` 与 `@popperjs/core` npm 依赖引入），组件按 Bootstrap class 编写；表单校验使用 Bootstrap 的 `is-invalid / invalid-feedback`；如需图标，使用 **Bootstrap Icons**
   - 构建：`make web` 产出 `internal/web/dist/`，二进制用 `//go:embed` 嵌入
5. **Docker 适配**：`docker-compose.yml` 增加 `8080:8080` 端口

### 3.3 代码改动清单
- [ ] `internal/web/server.go`（新增）：HTTP 服务入口
- [ ] `internal/web/router.go`（新增）：路由注册
- [ ] `internal/web/handler_config.go`（新增）：配置读写
- [ ] `internal/web/handler_modules.go`（新增）：模块管理
- [ ] `internal/web/handler_sync.go`（新增）：同步队列
- [ ] `internal/web/handler_logs.go`（新增）：日志 tail/WS
- [ ] `internal/web/auth.go`（新增）：token 鉴权中间件
- [ ] `internal/core/reload.go`（新增）：配置重载广播
- [ ] `internal/core/logger.go`：增加 WS hook
- [ ] `cmd/autofilm/main.go`：启动 Web server + 监听重载信号
- [ ] `webui/`（新增）：Vue 3 + Vite + Bootstrap 5 项目（`package.json` 依赖：`vue`、`vite`、`bootstrap@5`、`@popperjs/core`、`bootstrap-icons`）
- [ ] `internal/web/dist/.gitkeep` + `//go:embed dist` 占位
- [ ] `Makefile`：新增 `web` / `web-build` 目标
- [ ] `Dockerfile`：构建时 `make web` 再 `go build`
- [ ] `docker-compose.yml`：暴露 8080

### 3.4 验收
- [ ] 浏览器打开 `http://host:8080` 可见 SPA
- [ ] 修改 Alist2Strm 配置并保存 → 内存 cron 任务被重建
- [ ] 手动触发任务，日志能在 WS 实时看到
- [ ] 设置 token 后，无 token 请求返回 401
- [ ] 关闭 AlistSync 的同步任务后，重试队列持久化存在

---

## 4. 实施阶段

> **核心调整**：数据库（功能四）前移到 P1/P2 之前，让所有持久化状态从一开始就落到统一存储，避免 P1/P2 的 JSON 临时态被 P3 推翻。

### 4.1 阶段总览

| 阶段 | 内容 | 依赖 | 估时 | 关键交付物 |
|---|---|---|---|---|
| **P0** | 抽象 `ConfigStore` 接口（先 yaml 实现）+ reload chan | — | 0.5d | `type ConfigStore interface`；yaml-backed 实现 |
| **P3** | 功能四：SQLite + 自写 migrator + SQL 实现 ConfigStore + 旧 yaml/json importer | P0 | 1.5d | `data/autofilm.db` 自动初始化；`task_runs` 写入 |
| **P1** | 功能一：增量扫描 + 快照 + QPS 限流（**直接写库表**） | P0 + P3 | 1.0d | `scan_mode` 切换；`FSListLight` + 令牌桶 |
| **P2** | 功能二：AlistSync 模块 + 守护重试（**直接写库表**） | P0 + P3 | 1.5d | `sync_tasks` 守护轮询；退避算法 |
| **P4** | Web 后端 API（基于库表）+ 热重载 | P0 + P3 | 1.5d | `internal/web/` + `/api/*` 全部从 `*Store` 读 |
| **P5** | Web 前端 SPA（Vue 3 + Bootstrap 5）+ 嵌入 | P4 | 2.0d | `webui/dist/` + `//go:embed` |
| **P6** | 文档（README、Docker compose、CHANGELOG） | 全部 | 0.5d | 增量更新每阶段后即补 |

**总估时：8.5d**（相比原 9.5d 减少 1d；消除 JSON 临时态与重复 importer 工作）

### 4.2 依赖图

```
        ┌──── P0 接口抽象 ────┐
        ↓                    ↓
        P3 SQLite+migrator ←──┘   (P3 期间完成 importer 吸收旧 yaml)
        ↓
   ┌────┴────┐
   ↓         ↓
  P1       P2                 ← P1 与 P2 并行无依赖
  增量      同步
   ↓         ↓
   └────┬────┘
        ↓
       P4 Web 后端
        ↓
       P5 Web 前端
        ↓
       P6 文档
```

### 4.3 横向并行子任务（不阻塞主线）

下列工作可在任意阶段并行穿插，避免最后堆积：
- **`pkg/alist` 客户端扩展**：P1 需 `FSListLight`（不发 fs/get）、P2 需 `FSPut / TaskInfo / TaskCancel / TaskRetry`——建议在 P0 期间或 P3 早期统一封装
- **`internal/core/reload.go`**：配置变更广播 chan，P3 期间完成，P4 直接消费
- **P6 文档**：每阶段结束即增量更新对应章节，最后只做收口与 CHANGELOG 汇总

### 4.4 各阶段衔接点

| 阶段 | 衔接给下一阶段 |
|---|---|
| **P0** ✅ 已完成 | P3 实现 SQL 版 ConfigStore 时直接 `swap`（接口已稳定） |
| **P3** | P1/P2 直接调用 `*Store` CRUD API；模块 `New(cfg)` 改造为从库表读 |
| **P1** | 产出 `task_runs` 记录供 P4 Web 展示；`alist2strm_snapshots` 表数据供 Web "上次扫描时间" 卡片 |
| **P2** | `sync_tasks` 队列状态供 P4 Web 队列表实时展示；`dead_letter` 告警前端可见 |
| **P4** | 前端开发期即可用 `curl /api/...` 联调；无需等 P5 |
| **P5** | 早期可用 `json-server` 风格 mock 搭前端骨架，后期对接 P4 |

### 4.6 P0 交付物

- ✅ `internal/core/configstore.go` —— `ConfigStore` 接口定义
- ✅ `internal/core/yamlstore.go` —— `YamlStore` 实现，包装 `SettingManager` + fsnotify 文件监听 + 订阅广播
- ✅ `internal/core/store.go` —— 全局 `Store()` / `SetStore()` 访问器
- ✅ `internal/core/config.go` —— 新增 `OnConfigChange(callback)` 方法，桥接 viper
- ✅ `cmd/autofilm/main.go` —— 启动时调用 `core.Store()` 触发初始化
- ✅ `internal/core/yamlstore_test.go` —— 单元测试（订阅/退订/重载/并发）

**P0 阶段未完成的（非 P0 范围，留给后续阶段）**：
- main.go 中 `settings.GetAlistServerList()` 等仍直接走 `SettingManager`；切换到 `Store().GetAlist2StrmList()` 在 P3 阶段统一改造
- Web 后端的 `Subscribe` 消费方在 P4 阶段实现（重建 cron 任务）
- `SetStore(SQLStore)` 的注入点在 P3 阶段启用

### 4.5 风险控制

- **P0 阶段需严格定义 ConfigStore 接口**（method 名、返回类型、错误约定），否则 P3 SQL 实现时易反复调整
- **P3 importer 一次性导入旧 yaml/JSON** 必须保留原始文件作为备份，不可删除
- **P1/P2 与 P3 同步开发**（如果采用并行）：P1/P2 必须 mock Store 接口或先 fork 一份内存版
- **P4 上线前**：P1/P2 必须保证所有"持久化状态"路径走库表（无 JSON 残留）

---

## 5. 功能四：使用 SQLite 数据库存储必要数据

> 把当前散落在 `config.yaml` 和 `config/cache/*.json`、`config/sync_queue/*.json` 中的状态，全部迁入 SQLite 单文件 `data/autofilm.db`，统一管理连接凭据、快照、任务队列、运行历史。

### 5.1 设计要点
1. **驱动与依赖**：
   - 使用 `modernc.org/sqlite`（**纯 Go**，无需 CGO；与 `database/sql` 兼容）
   - 默认数据库文件 `data/autofilm.db`，WAL 模式，`_journal`/`_wal` 同样落在 `data/`
   - 配置项 `Settings.Database.Path`（默认 `./data/autofilm.db`）、`MaxOpenConns`（默认 1，写多读少场景安全）
2. **迁移工具**：自写轻量 migrator，仅 SQLite 适用
   - 迁移文件：`internal/storage/migrations/V001__init.sql`、`V002__xxx.sql` …
   - 启动时扫描目录，`SELECT version FROM migrations` 决定从哪条开始；逐条 `BEGIN; 执行 SQL; INSERT INTO migrations; COMMIT`
   - 不支持 down（生产简化）；损坏时回退到上一个 `good_version`
3. **表结构**（首版 `V001__init.sql`）：

   ```sql
   -- Alist 连接凭据（被各模块条目引用）
   CREATE TABLE alist_connections (
     id          INTEGER PRIMARY KEY AUTOINCREMENT,
     name        TEXT    NOT NULL UNIQUE,    -- 用户可识别名
     url         TEXT    NOT NULL,
     username    TEXT    NOT NULL DEFAULT '',
     password    TEXT    NOT NULL DEFAULT '', -- 建议加密：见 §5.2
     token       TEXT    NOT NULL DEFAULT '',
     public_url  TEXT    NOT NULL DEFAULT '',
     created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
   );

   -- Alist2Strm 条目
   CREATE TABLE alist2strm_configs (
     id            INTEGER PRIMARY KEY AUTOINCREMENT,
     cfg_id        TEXT    NOT NULL UNIQUE,   -- 业务 id（与 yaml 对齐）
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
     smart_protection_json TEXT,               -- 嵌套 JSON
     scan_mode     TEXT    NOT NULL DEFAULT 'incremental',  -- full | incremental
     qps_limit     INTEGER NOT NULL DEFAULT 10,
     cron          TEXT    NOT NULL,
     created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
   );

   -- Ani2Alist 条目
   CREATE TABLE ani2alist_configs (
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

   -- AlistSync 条目（一对多 pairs）
   CREATE TABLE alisync_configs (
     id            INTEGER PRIMARY KEY AUTOINCREMENT,
     cfg_id        TEXT    NOT NULL UNIQUE,
     enable        BOOLEAN NOT NULL DEFAULT 1,
     run_on_start  BOOLEAN NOT NULL DEFAULT 0,
     connection_id INTEGER NOT NULL REFERENCES alist_connections(id),
     pairs_json    TEXT    NOT NULL,           -- [{src,dst,delete_src,overwrite}, ...]
     retry_json    TEXT    NOT NULL,           -- {max_attempts,backoff,jitter}
     cron          TEXT    NOT NULL,
     created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
   );

   -- Alist2Strm 增量快照（替代 cache JSON）
   CREATE TABLE alist2strm_snapshots (
     config_id  INTEGER NOT NULL REFERENCES alist2strm_configs(id) ON DELETE CASCADE,
     path       TEXT    NOT NULL,
     size       INTEGER NOT NULL,
     modified   TEXT    NOT NULL,
     sign       TEXT    NOT NULL DEFAULT '',
     captured_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     PRIMARY KEY (config_id, path)
   );
   CREATE INDEX idx_snapshots_captured ON alist2strm_snapshots(config_id, captured_at);

   -- AlistSync 同步任务与重试队列（替代 sync_queue JSON）
   CREATE TABLE sync_tasks (
     id            INTEGER PRIMARY KEY AUTOINCREMENT,
     sync_config_id INTEGER NOT NULL REFERENCES alisync_configs(id) ON DELETE CASCADE,
     src_path      TEXT    NOT NULL,
     dst_path      TEXT    NOT NULL,
     state         TEXT    NOT NULL DEFAULT 'pending', -- pending|running|succeeded|failed|dead_letter
     alist_task_id TEXT    NOT NULL DEFAULT '',
     attempts      INTEGER NOT NULL DEFAULT 0,
     last_error    TEXT    NOT NULL DEFAULT '',
     next_retry_at DATETIME,
     created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
   );
   CREATE INDEX idx_sync_tasks_state ON sync_tasks(state, next_retry_at);

   -- 任务执行历史
   CREATE TABLE task_runs (
     id              INTEGER PRIMARY KEY AUTOINCREMENT,
     module_type     TEXT    NOT NULL,         -- alist2strm|ani2alist|alisync
     config_id       INTEGER NOT NULL,
     started_at      DATETIME NOT NULL,
     finished_at     DATETIME,
     status          TEXT    NOT NULL DEFAULT 'running', -- running|succeeded|failed
     error_summary   TEXT    NOT NULL DEFAULT '',
     files_total     INTEGER NOT NULL DEFAULT 0,
     files_added     INTEGER NOT NULL DEFAULT 0,
     files_modified  INTEGER NOT NULL DEFAULT 0,
     files_deleted   INTEGER NOT NULL DEFAULT 0
   );
   CREATE INDEX idx_task_runs_module ON task_runs(module_type, config_id, started_at DESC);

   -- migrator 元数据
   CREATE TABLE migrations (
     version    INTEGER PRIMARY KEY,
     name       TEXT    NOT NULL,
     applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
   );
   ```

4. **凭据加密**（§5.2）：`alist_connections.password` / `token` 默认经 AES-GCM 加密后存储；密钥来自 `Settings.Database.EncryptionKey`（env 变量 `AUTOFILM_DB_KEY` 优先）。首次启动若未设置密钥 → 自动生成并写入 `data/.db_key`（权限 0600）。
5. **迁移兼容**：
   - 首次启动时若 `data/autofilm.db` 不存在 → 自动执行所有 migrations
   - **若旧的 `config/config.yaml` 中已有 `Alist2StrmList/Ani2AlistList/AlistSyncList`** → 自动导入到库表（V001 migration 中以 Go 侧 `importYamlIfNeeded()` 完成），导入后保留 yaml 作为备份
   - 旧 `config/cache/*.json` 与 `config/sync_queue/*.json` 启动时一次性导入对应表
6. **模块读取改造**：
   - 各模块的 `New(cfg)` 改为接收从库表读出的 `*Config`
   - `main.go` 增加 `internal/storage/store.go` 单例，启动时初始化并 `LoadAll()` 把库表行转成模块配置
   - Web 后端读写配置都走 `*store`；不再走 viper

### 5.3 代码改动清单
- [ ] `go.mod` 增加 `modernc.org/sqlite`
- [ ] `internal/storage/db.go`（新增）：`*sql.DB` 单例 + `PRAGMA`（WAL、busy_timeout、foreign_keys=ON）
- [ ] `internal/storage/migrator.go`（新增）：自写 migrator，扫描 `migrations/`
- [ ] `internal/storage/migrations/V001__init.sql`（新增）：上述建表 SQL
- [ ] `internal/storage/store.go`（新增）：封装 CRUD（连接、配置、快照、任务、运行）
- [ ] `internal/storage/crypto.go`（新增）：AES-GCM 加解密辅助
- [ ] `internal/storage/importer.go`（新增）：从旧 yaml/json 一次性导入
- [ ] `internal/core/config.go`：保留 `Settings.{Database, General}`，移除模块列表字段
- [ ] `internal/modules/alist2strm/alist2strm.go`：使用 `*storage.Store` 读取/写入快照
- [ ] `internal/modules/alist2strm/snapshot.go`：快照存库（替代 JSON）
- [ ] `internal/modules/alissync/daemon.go`、`queue.go`：队列存库（替代 JSON）
- [ ] 所有模块 `Run()` 入口增加 `task_runs` 写入
- [ ] `cmd/autofilm/main.go`：启动顺序调整为 init DB → migrator → importer → load configs → cron
- [ ] `Dockerfile` / `docker-compose.yml`：新增 `data:/data` 卷
- [ ] `config/config.yaml` 模板：简化为只保留 `Settings`（General/Database/Web）

### 5.4 验收
- [ ] 首次启动自动建库并执行所有 migrations
- [ ] 旧 yaml 配置能自动导入且不丢失
- [ ] 旧的 cache JSON / sync_queue JSON 能在启动后从库表完整还原
- [ ] 单实例并发运行下无 SQLITE_BUSY
- [ ] `data/autofilm.db` 与 `data/.db_key` 文件权限 0600
- [ ] 单元测试：migrator 版本回放、importer 字段映射、crypto 加解密

### 5.5 风险与权衡
- 加密密钥管理：若 `data/.db_key` 丢失，凭据无法恢复；需在 README 强调备份
- 并发：WAL + 单 writer 模式对单机足够；如未来要多实例需切到 MySQL/PG
- 迁移不可逆：避免在已发布版本 `DROP COLUMN`，统一用"新增列 + 数据回填"方式

---

## 6. 已确认决策

- [x] **前端技术栈**：Vue 3 + Bootstrap 5
- [x] **数据库**：SQLite（`modernc.org/sqlite` 纯 Go 驱动）
- [x] **数据库存储范围**：Alist 连接凭据、Alist2Strm 快照、AlistSync 任务与重试队列、任务执行历史
- [x] **迁移工具**：自写轻量 migrator（仅 SQLite 适用）

## 7. 待用户确认事项

- [ ] Alist `fs/put` 权限：是否区分 `admin_token`（同步用）与 `user_token`（播放用）？
- [ ] Web 鉴权强度：默认单 token 即可；是否需要多用户/角色？
- [ ] 是否同步引入 Prometheus `/metrics` 端点？
- [ ] 是否将"封号真凶"定位在逐文件 `fs/get` 拉直链？如是，可在 P1 一并改为"生成 .strm 时懒取"以进一步省请求
- [ ] 凭据加密：是否同意默认 AES-GCM + `data/.db_key`（0600）方案？还是仅 base64 存储？
- [ ] 数据库是否需要提供"导出/导入"按钮（Web 端备份）？
