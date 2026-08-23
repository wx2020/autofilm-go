# AutoFilm Go

AutoFilm 是一个 Go 实现的媒体自动化工具，提供 Alist/OpenList 媒体处理、本地文件整理、同步、海报生成以及 Web 管理界面。

## 功能模块

- **Alist2Strm**：扫描 Alist/OpenList 媒体目录并生成 STRM，支持增量扫描、字幕/图片/NFO、并发、QPS 限制和智能删除保护。
- **Ani2Alist**：根据 ANI Open 数据或 RSS 更新 Alist 目录结构，支持季度、关键词和 RSS 模式。
- **AlistSync**：同步多个 Alist/OpenList 目录对，支持 `never`、`always`、`if_newer` 覆盖策略、任务队列和失败重试。
- **FileMove**：递归扫描本地目录，按相对路径正则和文件大小筛选文件，定时移动并保留目录结构。
- **LibraryPoster**：从 Jellyfin/Emby 媒体库生成拼图海报，支持自定义标题、副标题和 TTF 字体。
- **Web 管理**：模块配置、手动执行、启停任务、日志、监控、告警、用户权限和备份恢复。

## 快速开始

### Docker Compose

```bash
docker compose up -d
```

默认访问地址为 <http://localhost:8080>。首次访问时创建管理员账户。

### 本地运行

先构建前端资源，再编译 Go 程序：

```bash
cd webui
npm ci
npm run build
cd ..
go build -o autofilm ./cmd/autofilm
./autofilm
```

前端构建产物会嵌入 Go 二进制，正式运行不需要 Node.js 或 npm。

也可以使用 Make：

```bash
make all       # 构建前端并编译程序
make test      # 运行 Go 测试
make vet       # 运行 go vet
```

## 配置与数据

系统配置和模块配置保存在 SQLite：

- 数据库：`data/autofilm.db`
- 数据库密钥：`data/.db_key`
- 日志目录：`logs/`
- WebUI：`http://127.0.0.1:8080`

当前运行时以 SQLite 中的模块配置为准。旧版 `config.yaml` 仅在数据库为空时自动导入一次，之后不再参与运行时配置。

WebUI 支持配置以下模块：

- `/alist2strm`
- `/ani2alist`
- `/libraryposter`
- `/sync`
- `/filemove`

### FileMove 配置

FileMove 用于将本地源目录中的文件递归移动到目标目录。正则表达式匹配源目录下使用 `/` 分隔的相对路径；移动后会保留相对目录结构。

示例：

```yaml
FileMoveList:
  - id: "local-archive"
    enable: true
    run_on_start: false
    backend: "local"     # local 或 openlist
    source_dir: "D:/downloads"
    target_dir: "D:/media"
    regex: "(?i)\\.(mkv|mp4)$"
    size: null             # 精确大小，单位：字节；null 表示不限制
    min_size: 0            # 最小大小，0 表示不限制
    max_size: 0            # 最大大小，0 表示不限制
    overwrite: false       # 目标文件存在时是否覆盖
    cron: "0 */10 * * * *"
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `source_dir` | 本地源目录，必须存在且为目录 |
| `target_dir` | 本地目标目录，不允许位于源目录内部 |
| `regex` | 匹配相对路径的 Go 正则表达式，可为空 |
| `size` | 精确文件大小，单位为字节，可为空 |
| `min_size` | 最小文件大小，单位为字节；`0` 表示不限制 |
| `max_size` | 最大文件大小，单位为字节；`0` 表示不限制 |
| `overwrite` | 是否覆盖目标中的同名文件，默认 `false` |
| `cron` | 定时表达式，支持秒字段 |

FileMove 默认跳过符号链接和非普通文件。跨文件系统移动时会先复制到目标目录的临时文件，完成后再删除源文件。

### FileMove OpenList 模式

FileMove 的 `backend` 默认为 `local`。设置为 `openlist` 后，模块通过 OpenList API 递归扫描源目录，并按正则和文件大小筛选文件；目标目录不存在时会自动创建，移动使用同一个 OpenList 实例的 `fs/move` 接口。

OpenList 模式需要额外配置 `url`、`username`/`password` 或 `token`，且 `source_dir`、`target_dir` 必须使用 OpenList 路径（以 `/` 开头）。FileMove 仅支持同一个 OpenList 实例内移动；跨 OpenList 实例的整目录同步仍使用 AlistSync。

### 环境变量

Docker Compose 会将 `config/`、`logs/` 和 `data/` 持久化到容器的 `/app` 目录对应位置；其中 `data/autofilm.db` 和 `data/.db_key` 必须保留，否则重新创建容器后会被视为全新安装，需要重新创建管理员。

| 变量 | 作用 |
| --- | --- |
| `AUTOFILM_WEB_HOST` | 覆盖 Web 监听地址 |
| `AUTOFILM_WEB_PORT` | 覆盖 Web 监听端口 |
| `AUTOFILM_WEB_ENABLED` | 启用或禁用 Web 服务 |
| `AUTOFILM_WEB_TOKEN` | Web/API 访问令牌 |
| `AUTOFILM_DB_KEY` | Base64 编码的 32 字节数据库加密密钥 |

## 权限

| 角色 | 权限 |
| --- | --- |
| `admin` | 系统设置、用户、备份恢复和全部模块操作 |
| `operator` | 模块配置、手动执行、启停任务、重试和告警确认 |
| `viewer` | 查看状态、日志、运行记录和告警 |

## 监控与接口

- Prometheus 指标：`GET /metrics`
- Web 指标：`GET /api/metrics`
- 健康检查：`GET /api/health`
- 模块列表：`GET /api/modules`
- 模块手动执行：`POST /api/modules/{type}/{id}/run`
- 模块启停：`POST /api/modules/{type}/{id}/toggle`

任务失败会写入运行记录和告警表；配置了 Webhook 后，会向 Webhook 地址发送 JSON 告警。

## 开发验证

```bash
go fmt ./...
go test ./...
go vet ./...
```

前端验证：

```bash
cd webui
npm run build
```

## 许可证

MIT
