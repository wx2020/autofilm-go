# AutoFilm Go 项目状态

## 当前架构

AutoFilm Go 是可直接运行的 Go Web 服务。Vue 3 管理界面会在构建时生成静态资源，并通过 `go:embed` 嵌入 Go 二进制；部署时不需要 Node.js 或 npm。

- 后端：Go、Gin、SQLite、Cron
- 前端：Vue 3、Vite、Element Plus
- 配置：SQLite 持久化，旧 YAML 仅用于首次迁移
- 部署：单个 Go 可执行文件或 Docker

## 已实现功能

- Alist2Strm：扫描、STRM 生成、快照 diff、删除保护
- Ani2Alist：订阅处理和 Alist 写入
- AliSync：持久化队列、覆盖策略、失败重试
- LibraryPoster：图片拼接、渐变遮罩、标题和字体渲染
- 在线配置：系统设置及各模块均提供表单，不需要编辑 JSON/YAML
- 连接测试与字段校验
- 日志查看
- 任务运行记录和手动触发
- Prometheus 指标、运行监控、失败告警及 Webhook
- SQLite 逻辑备份与恢复
- 多用户登录和 admin/operator/viewer 权限

## 安全与数据

- 模块敏感配置使用 AES-GCM 加密后写入 SQLite
- 密码使用 PBKDF2-SHA256 加盐哈希
- 登录使用服务端会话
- 首次访问 Web 页面时创建第一个管理员
- 备份文件含解密后的业务配置，应按敏感文件保管

## 构建与验证

```bash
cd webui
npm install
npm run build

cd ..
go test ./...
go vet ./...
go build -o bin/autofilm-web.exe ./cmd/autofilm
```

日常部署和运行编译好的程序不需要 npm：

```bash
./bin/autofilm-web.exe
```

默认访问地址为 `http://127.0.0.1:8080/`。首次打开会进入管理员初始化页面。

## 运维接口

- `GET /api/health`：健康检查
- `GET /metrics`：Prometheus 指标
- Web“运行监控”：任务记录和告警
- Web“用户与备份”：用户权限、备份下载和恢复

更完整的安装、配置和使用说明见 `README.md`。
