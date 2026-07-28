# AutoFilm Go

AutoFilm 的 Go 实现：将 Alist/OpenList 媒体生成 STRM，维护动漫挂载、云端同步和 Jellyfin/Emby 媒体库海报，并提供完整 Web 管理界面。

## 功能

- **Alist2Strm**：全量/增量扫描、STRM、字幕/图片/NFO、BDMV、并发与 QPS、智能删除保护
- **Ani2Alist**：ANI Open 数据与 RSS 更新、季度和关键词过滤
- **AlistSync**：多目录对、覆盖策略、SQLite 任务队列、指数退避和 dead letter
- **LibraryPoster**：1200×1800 拼图、主视觉、标题/副标题、自定义 TTF 字体
- **Web 管理**：选项化配置、任务执行、日志、监控、告警、用户与备份
- **安全**：AES-GCM 凭据加密，管理员/操作员/只读用户 RBAC，24 小时会话
- **运维**：SQLite、Prometheus `/metrics`、Webhook、Docker、单二进制部署

## 快速开始

### Docker Compose

```bash
docker compose up -d
```

访问 `http://localhost:8080`，首次打开时创建管理员账户。容器默认通过 `AUTOFILM_WEB_HOST=0.0.0.0` 监听。

### 本地构建

```bash
cd webui
npm ci
npm run build
cd ..
go build -o autofilm ./cmd/autofilm
./autofilm
```

Vue 生产资源使用 `go:embed` 打入二进制，正式运行不需要 Node.js 或 npm。

## 配置与数据

系统和模块配置保存在 `data/autofilm.db`，凭据使用 `data/.db_key` 加密。旧版 `config.yaml` 仅在数据库为空时自动导入一次，之后不参与运行。

WebUI 中可以：

- 新增、编辑、删除四类模块配置
- 测试 Alist/OpenList 连接
- 设置时区、监听地址、端口、Token 和告警 Webhook
- 创建管理员、操作员和只读用户
- 下载或恢复逻辑配置备份

环境变量：

| 变量 | 用途 |
|---|---|
| `AUTOFILM_WEB_HOST` | 覆盖监听地址 |
| `AUTOFILM_WEB_PORT` | 覆盖监听端口 |
| `AUTOFILM_WEB_ENABLED` | 启用或禁用 Web |
| `AUTOFILM_WEB_TOKEN` | 首次创建用户前的兼容 Token |
| `AUTOFILM_DB_KEY` | Base64 编码的 32 字节数据库加密密钥 |

## 权限

| 角色 | 权限 |
|---|---|
| `admin` | 系统设置、用户、备份恢复和全部操作 |
| `operator` | 配置模块、执行任务、重试和确认告警 |
| `viewer` | 查看状态、日志、运行记录和告警 |

## 监控与告警

- Prometheus：`GET /metrics`
- Web 指标：`GET /api/metrics`
- 任务失败会写入 SQLite 告警表
- 配置告警 Webhook 后会 POST：

```json
{"level":"error","source":"alist2strm:movies","message":"错误信息"}
```

## 开发验证

```bash
go fmt ./...
go test ./...
go vet ./...
```

当前测试覆盖数据库迁移回放、旧 YAML 导入、AES-GCM、密码哈希、配置密文、快照 diff、同步覆盖和退避、海报生成、Web 参数校验、嵌入 SPA 和 Prometheus 指标。

## 许可证

MIT
