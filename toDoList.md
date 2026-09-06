# alistsync 同步失败解决方案 ToDoList

> 来源日志：`/pt/wo -> /wo` 同步 10 文件全败，`2026-09-06 14:20:26 ~ 14:21:15`
> 三类报错：`FSGet object not found` / `FSPut 解析响应失败 invalid character '<'` / `sync_queue ...json.tmp: no such file or directory`

## P0 必修（导致本次全败）

- [x] 1. 修复 `FSPut` 返回 HTML 导致全员提交失败
  - 文件：`pkg/alist/client.go:295-330 (doRequest)`，`pkg/alist/client.go:536-558 (FSPut)`，`internal/modules/alistsync/sync.go:78`，`internal/modules/alistsync/daemon.go:200`
  - 动作：
    - `doRequest` 解析失败时打印 `endpoint + status + content-type + body前500字符`，不再只报 `invalid character '<'`。
    - 非 200 / 非 JSON 直接返回 `状态码 + body摘要`，便于区分反代 404/登录页/版本无此接口。
    - 核对目标 Alist/OpenList 版本是否有 `POST /api/fs/put`；若无，切换到该版本离线下载接口（如 `add_offline_download`）并做版本兼容。
    - `daemon.go checkRetryTask` 增加守卫：`AlistTaskID==""` 或 `RawURL==""` 时直接重新 `FSPut`，不调 `TaskRetry`。
  - 验收：10 个文件 `FSPut` 返回合法 `task_id`，不再出现 `<` 解析错；重试链路同过。

- [x] 2. 修复 `sync_queue` 嵌套路径 `no such file or directory`
  - 文件：`internal/modules/alistsync/queue.go:44-46 (taskFilePath)`，`:161-172 (fileSave)`，`:190-216 (fileLoadAll)`
  - 根因：`taskID = dstPath = /wo/电视剧/...`，`filepath.Join(queueDir, taskID+".json")` 形成深层嵌套，但 `fileSave` 未 `MkdirAll(filepath.Dir)`，必 `ENOENT`。
  - 动作：
    - `taskFilePath` 扁平化：`strings.TrimPrefix + url.PathEscape` 或 `sha256(dstPath)` 命名，杜绝 `/` 分隔符。
    - `fileSave` 前 `os.MkdirAll(filepath.Dir(path))`，保留 `.tmp + Rename` 原子写。
    - `fileLoadAll` 兼容老嵌套文件：改为 `filepath.WalkDir` 递归读 `*.json`，并做一次性迁移到扁平命名。
  - 验收：`Save/LoadAll` 在含中文/长路径下通过；`已从磁盘恢复 N 个` 不再恒为 0。

- [x] 3. 消除 `FSGet object not found` 误报为 ERROR
  - 文件：`pkg/alist/client.go:436-454 (FSGet)`，`internal/modules/alistsync/sync.go:31`
  - 根因：`sync.go` 用 `FSGet(dstPath)` 做存在性探测，目标不存在是正常预期，被 `client` 全量打成 `[ERROR]`，10 文件刷 10 条。
  - 动作：
    - `FSGet` 对 `object not found / code 500+message` 降为 `Debug`，或新增 `FSExists / IsNotFound(err)` 供调用方判断。
    - `sync.go:31` 忽略 `not found` 错误，仅非常规错误打 `Warn`；`ShouldOverwrite` 保持 `existing==nil -> 按策略需同步`。
  - 验收：目标不存在时无 `[ERROR]`，全量同步日志干净。

## P1 加固（防复发）

- [x] 4. `ShouldOverwrite` 默认策略收敛：`internal/modules/alistsync/overwrite.go:20-35` 空 `policy` 当前返回 `false`，空配置会静默一个不同步。默认改为 `if_newer` 或在 `New` 期校验报错。
- [x] 5. 单测补齐并跑通：
  - `queue_test.go`：嵌套中文路径 `Save->LoadAll` 往返 + 幂等。
  - `client_test.go`：`doRequest` 喂 HTML body，断言错误含 endpoint/status/body摘要。
  - `sync_test.go`：`FSGet not found` 不计错、`ShouldOverwrite` 空策略行为。
  - 命令：`go test ./internal/modules/alistsync/... ./pkg/alist/...`
- [ ] 6. 发布前用真实 OpenList 跑一次 `/pt/wo -> /wo`（≥10 文件），确认：无 ERROR 刷屏、有 `task_id`、守护协程 `succeeded`、重启后可恢复。

## 验证命令

```bash
go test ./internal/modules/alistsync/... ./pkg/alist/... -count=1 -v
# 真机冒烟：观察不再出现 invalid character '<' / json.tmp no such file
```
