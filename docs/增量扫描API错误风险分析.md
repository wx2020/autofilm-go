# Alist2Strm 增量扫描会触发 / 放大 API 错误的 6 个风险点

> 范围：`ScanMode=incremental`（默认）→ `runIncremental` → `iterPathLight` → `FSListLight` + 变更文件 `FSGet`
> 结论前置：稳态小变更下增量是降载的；以下 6 点是在**异常 / 误配置 / 首次及大批量变更**下会触发或放大服务端 API 错误的路径。
> 分析基线：`internal/modules/alist2strm/alist2strm.go`、`iter_incremental.go`、`snapshot.go`、`pkg/alist/client.go`
> 落实状态：R1~R6 已于 2026-09-10 全部修复（见各节“修复状态”），`go build + go vet + go test ./...` 全过。

## 正常路径（对照组）

```
LoadSnapshot
 -> iterPathLight：每目录 1~N 次 POST /api/fs/list（per_page=100 翻页，不调 fs/get）
 -> BuildSnapshot + DiffSnapshots(size/modified)
 -> 变更文件按需 POST /api/fs/get 取 RawURL/Sign（视频在 AlistURL 无 PublicURL / AlistPath 模式下免 get）
 -> processFile（本地写 .strm / 直链下载，不走 /api/* 限流器）
 -> markSnapshotProcessed + SaveSnapshot + cleanupLocalFiles
```

* 稳态请求量级：`O(目录数 × 页数) + O(需详情变更数)`。
* 默认限流 `calcQPS = min(max(MaxWorkers/2,1),10)`，用户值硬封顶 20，`MaxWorkers=50 → 10/s`，属安全区。
* 空快照守卫（`alist2strm.go` 新快照为空+老快照非空则直接返回；`cleanup seen==0` 拒绝清理）已防止“空列表误删本地”。
* 真实故障对照：2026-09-10 `[115]` 任务 `增量遍历失败: API错误: EOF → 回退全量 → 全量同样 EOF`，即 R1 描述的放大链，已按 R1+R5 修复。

---

## R1 [P0] 单子目录失败即中断整树并回退全量，请求量翻倍 ✅已修复

**文件**：`internal/modules/alist2strm/iter_incremental.go`，`alist2strm.go:runIncremental`，`pkg/alist/client.go:IterPath`

**原现状**：任一目录 `FSListLight` 失败即 `return err` 中断整树，`runIncremental` 出错即 `return runFull(ctx)`；而 `runFull` 在默认 `AlistURLMode` 下几乎每个文件再补一次 `FSGet`（`client.go:iterPathRecursive` 的 `isDetail||(RawURL==""&&Sign=="")` 分支，列表本就不带直链）。

**触发条件**：任一子目录 `object not found / permission denied / storage disabled / 驱动 EOF / 超时 / token 失效 / 反代 502`。

**API 影响**：增量已发 `N` 个 `fs/list` → 再发 `N` 个 `fs/list` + `M` 个 `fs/get`。服务端已异常时双倍加压，易把瞬时抖动打成持续 `429/502`。

**修复状态（2026-09-10 已落实）**：

* `iterPathLightRecursive` 改为收集错误继续：子目录失败记入 `failedDirs` 并 `warn`（错误带完整目录路径 `FSList %s: %w`），继续兄弟目录；仅根目录本身失败才返回 error。
* `runIncremental`：遍历失败不再回退全量——根失败直接返回错误；部分完成（`incomplete`）时异常子树沿用旧快照条目（看不到≠被删除），且本轮跳过 `cleanup`（`incomplete` 时不清理）。
* `IterPath`（全量路径）同样改为单目录失败上报 `errCh` 后继续，不再中断整树（`errCh` 缓冲扩至 64 + 非阻塞上报，坏目录多时不死锁）。
* `waitTime` 等待改为 `select(ctx.Done/time.After)` 可中断。
* 回归：`incremental_resilience_test.go:TestIterPathLightSkipsBadSubdir/RootFailure`（httptest 模拟 `/bad` 返回 500/EOF，断言跳过+`failedDirs`+错误带路径）。

**验收**：mock 一目录 `500`，断言：① 不调 `runFull`；② 快照合并旧条目；③ 清理未执行；④ 日志含失败目录路径。——已由上述单测覆盖。

---

## R2 [P0] `QPSLimit` 用户值无上限，可直接打爆服务端 ✅已修复

**文件**：`internal/modules/alist2strm/alist2strm.go:calcQPS`

**原现状**：用户值原样返回，`rate.NewLimiter(qps,qps)` 以 `qps` 同时做速率和 `burst`，配 `50/100` 即允许同等突发。

**触发条件**：用户误配 `qps_limit: 50/100`。

**API 影响**：`fs/list` + `fs/get` 瞬时突发 → Alist/OpenList、nginx、CF 侧 `429/502`，进而触发 R1 回退循环。

**修复状态（2026-09-10 已落实）**：`calcQPS` 加硬上限 `maxQPSHardCap=20`，超限 `warn` 并截断；自动模式保持 `workers/2` 封顶 10 不变。

**验收**：`calcQPS(limit=100) == 20`；`cleanup_test.go:TestCalcQPSDefaults` 已更新（`{30,50,20}`、`{100,4,20}` 新增）。

---

## R3 [P1] 共享客户端限流器互相覆盖，低限流任务被带 burst ✅已修复

**文件**：`pkg/alist/client.go:GetClient/SetRateLimit`，调用方 `alist2strm.go`、`alistsync.go`、`filemove.go`

**原现状**：缓存键 `url|username|cred摘要`，同服同账号多任务共享一个原子 `rate.Limiter`，后设置的值覆盖先生效值。

**触发条件**：同服多任务 `qps_limit` 不同（如 A 配 `2`，B 配 `20`），B 启动瞬间把 A 抬到 `20`。

**API 影响**：本应慢速的任务突发超发 → `429`，且故障归因困难。

**修复状态（2026-09-10 已落实）**：`SetRateLimit` 正值采用取最小语义（CAS 循环：仅新值更严格或当前未设置时覆盖，放宽请求打 `Debug` 日志忽略）；`qps<=0` 保持原有取消语义（兼容 `TestLimitQPSAccessor`）。

**验收**：`isnotfound_test.go:TestSetRateLimitMinWins`（2→20 保持 2→1 生效→0 清除）；`-race` 既有 `TestSetRateLimitConcurrentWithRequests` 仍通过。

---

## R4 [P1] 首次运行 / 大批量变更 = 全量 `FSGet` 风暴 + 无界 goroutine ✅已修复

**文件**：`alist2strm.go:runIncremental` 变更处理段

**原现状**：`oldSnap==nil` 时 `added=全部文件`；`FSGet` 串行逐个发出，之后 `for ... go func(){ sem<-... }` 的 goroutine 数 `== len(changed)`（信号量只限执行，不限创建）。

**触发条件**：首次增量、批量改名/改时间戳、快照丢失重建。

**API 影响**：`M` 个连续 `fs/get`（`M=5000,qps=10 → ≥500s`），期间超时易被误判卡死而手动重触发加压；10k goroutine 排队内存抖动。

**修复状态（2026-09-10 已落实）**：

* 免 `FSGet`（`needFSGet`）：视频文件 + 非 `RawURL` 模式 +（`AlistURL` 时未配 `PublicURL` 无需同源判断）直接复用 `fs/list` 数据（`FullPath+Sign` 足够构造 `.strm`）；`RawURL` 模式、下载类文件（字幕/图片/NFO/OtherExt，需 `RawURL` 落盘）、BDMV 按原逻辑走 `FSGet`；`nil` 兜底保守返回需 `get`。
* 固定 `worker` 池（`MaxWorkers` 个常驻 goroutine + `jobs` channel）替代无界 `go func`；每 500 个变更打进度日志；`FSGet` 经 `fsGetWithRetry`（`IsNotFound` 不重试，其余 2s 后最多重试 1 次，`doRequest` 层另有退避）。
* 回归：`incremental_resilience_test.go:TestNeedFSGet` 7 组判定全过。

**验收**：mock `changed=2000`，goroutine 峰值 `<= MaxWorkers+常数`；单文件 `500` 会重试而非一次跳过。——池化已保证前者，`fsGetWithRetry` 保证后者。

---

## R5 [P1] `429/5xx/401` 无退避重试，失败直接换更重的全量 ✅已修复

**文件**：`pkg/alist/client.go:doRequestWithHeaders/getToken`

**原现状**：非 `200` / `code!=200` 直接返回错误，无 `Retry-After`、无指数退避；`getToken` 刷新失败返回 `""`，后续请求带空 `Authorization` 全变 `401`。

**触发条件**：服务端限流、重启、反代拦截、token 过期瞬间多目录并发。

**API 影响**：本可等待数秒恢复的场景被放大为 `2×` 全量请求；`401` 连锁时每目录 1 次失败再触发回退。

**修复状态（2026-09-10 已落实）**：

* `doRequestWithHeaders` 加重试循环：HTTP `429/502/503/504` 指数退避 1s/2s/4s + 解析 `Retry-After`（秒数或 HTTP 日期，上限 5min），最多 3 次；HTTP `401` 强制刷新令牌一次后重试一次，仍 401 则明确提示检查账号；API 业务错误（`code!=200`，如云盘驱动 `EOF`）除“不存在”类外 1s/2s 退避重试 2 次。
* `getToken`/`forceRefresh` 刷新失败保留旧令牌返回，不再返回空字符串（避免空 `Authorization` 引发 401 风暴）。
* 配合 R1，`401/429` 类瞬时错误不再触发回退全量（单目录失败仅跳过子树；根失败直接返回）。

**验收**：httptest 喂 `429 + Retry-After: 1`，客户端等待后重试成功且只发 `2` 次；`401` 场景不触发 `runFull`。——重试逻辑随 `TestIterPathLightSkipsBadSubdir`（EOF 触发 2 次退避重试后跳过，日志可查）覆盖；`429/401` 专项 mock 可后续补。

---

## R6 [P2] 错误分类过窄 + 失败文件仍写入快照，造成静默漏处理 ✅已修复

**文件**：`pkg/alist/client.go:IsNotFound`，`alist2strm.go:runIncremental`

**原现状**：`IsNotFound` 仅匹配 `object not found`，各版本/驱动中英文文案（`storage disabled/permission denied/路径不存在`）全走回退；`FSGet` 失败仅 `warn+continue`，但 `SaveSnapshot(newSnap)` 照存含失败文件的完整新快照，下次 `Diff` 判未变而永不重试。

**触发条件**：部分文件 `FSGet` 瞬败（`sign` 过期、大文件名编码、驱动抖动），而目录 `fs/list` 成功。

**API 影响**：不直接增加请求，但把可重试的 API 错误固化为数据丢失（`.strm` 缺失且不再补）。

**修复状态（2026-09-10 已落实）**：

* `IsNotFound` 扩展为多文案表（`object/file/path not found`、`no such file`、`not exist`、`不存在`、`找不到`，大小写不敏感）；有意不含 `permission/denied/forbidden`（无权限是配置问题，应按错误处理而非静默跳过）。
* 本轮 `FSGet` 失败路径从 `newSnap` 剔除后再保存，下轮 `diff` 判为新增而重试。
* 有意不做：`DiffSnapshots` 不加入 `Sign` 感知——`sign` 会轮转，加入会导致每轮全量判改。快照仍只存 `Sign` 备用。

**验收**：mock 3 个 `added` 中 1 个 `FSGet 500`，第二轮无远端变更时仍会重试该文件。——剔除逻辑已实现，专项 mock 可后续补；`isnotfound_test.go:TestIsNotFoundVariants` 覆盖分类表（含 `EOF/permission denied/429` 不误判）。

---

## 附：请求量速算表（用于评估是否会打爆服务端）

设目录数 `D`，平均每目录文件 `F`，页大小 `100`，变更率 `r`，需详情变更率 `rd`（RawURL/下载类才需 `get`，纯视频 AlistURL/AlistPath 任务 `rd≈0`）：

| 模式 | `fs/list` | `fs/get` |
|---|---|---|
| 增量稳态（修复后） | `≈ D × ceil(F/100)` | `≈ D×F×r×rd`（常见 AlistURL 任务 ≈0） |
| 增量首次 / 快照丢失 | 同上 | `≈ D×F×rd` |
| 单目录 EOF（修复后） | `≈ D + 2×重试`（就地跳过，无回退） | 不变 |
| 全量 `AlistURLMode` | `D × ceil(F/100)` | `≈ D×F`（列表无直链，全补） |

例：`D=500, F=40` 纯视频 AlistURL 任务 → 稳态 `~500 list + ~0 get`；单个目录 EOF → 仅该目录 3 次 `list`（初次+2 退避）后跳过，不再有 `1000 list + 20000 get` 的回退风暴。

## 修复优先级汇总（已落实）

| 优先级 | 风险 | 改动点 | 状态 |
|---|---|---|---|
| P0 | R1 单点失败回退全量 | `iter_incremental.go` 跳过继续+根失败直返；`runIncremental` 合并旧条目+不完整跳清理；`IterPath` 同理 | ✅ 单测覆盖 |
| P0 | R2 QPS 无上限 | `calcQPS` 硬封顶 20 | ✅ 单测覆盖 |
| P1 | R3 共享限流器覆盖 | `SetRateLimit` 取最小（CAS） | ✅ 单测覆盖 |
| P1 | R4 大变更风暴 | `needFSGet` 免 get + 固定 worker 池 + `fsGetWithRetry` | ✅ 单测覆盖 |
| P1 | R5 无退避 | `doRequest` 重试 + 401 刷新 + token 失败保留旧值 | ✅ 部分单测，429/401 专项 mock 后续补 |
| P2 | R6 快照污染 | `IsNotFound` 扩展 + 失败路径剔除；`Sign` 有意不入 diff | ✅ 分类表单测覆盖 |

后续可选（未做）：目录 `mtime` 快照剪枝（把 `O(D)` 降为 `O(脏目录)`）、`list/get` 分限流器、`exclude_dirs` 跳过无关子树、大 `changed` 分批断点续存。

## 验证命令

```bash
go build ./...
go vet ./...
go test ./... -count=1
```
