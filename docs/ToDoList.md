# OpenList v4.2.6 API 声明 vs 项目实作 — ToDoList

> 对比来源：Apifox `openlist.apifox.cn / fox.oplist.org` + `alistgo.com/guide/api`；实作：`pkg/alist/client.go`、`internal/modules/alistsync/daemon.go`
> 状态：T1 已在上一轮修复（`FSPut` 改走 `add_offline_download`），T2~T5 本轮修复。

## T1 FSPut 方法/路径/载荷（已修复，回归锁定）

- 声明：`PUT /api/fs/put` 流式上传（`File-Path + As-Task + 二进制`，返 `data.task`）；URL 落盘是 `POST /api/fs/add_offline_download {path,urls,tool,delete_policy}`，返 `data.tasks[0].id`。
- 曾实作：`POST /api/fs/put {path,files:[{path,url}]}` 解析 `data.task_id` → 必返 HTML（`invalid character '<'`）。
- 动作：`AddOfflineDownload` 新实现 + `FSPut` 转调；单测断言 `POST /api/fs/add_offline_download`。
- [x] 完成

## T2 任务轮询端点错误（本轮 P0）

- 声明：按类型分端点 `POST /api/admin/task/{upload,copy,offline_download,...}/{info,cancel,retry}`，`info` 用 `?tid=` 查单任务，`data` 为数组 `[{id,name,state,progress,error}]`；离线下载建的是 `offline_download` 任务。
- 实作：`TaskInfo/Cancel/Retry` 统一调不存在的 `POST /api/admin/task/task_info|cancel|retry + JSON{id}`（`client.go:597,613,621`），`daemon.go:144` 查出来也解析不成单对象。
- 动作：
  - 新增 `taskRequest(taskType, action, tid)`，`TaskInfo/Cancel/Retry` 默认走 `offline_download` 类型，`?tid=` 传参，兼容老端点回退。
  - `TaskInfo` 解析数组取首元素；`TaskInfoData` 补 `Error` 字段，`state` 兼容 string/int。
  - `daemon.go` 用 `info.Error` 优先于 `info.Status` 写 `LastError`。
- [x] 待实作 → 验收：httptest 断言命中 `/api/admin/task/offline_download/info?tid=` 并解析数组。

## T3 目录判断字段错误（本轮 P0）

- 声明：`FsObject{is_dir:bool, type:0=Unknown/1=Folder/2=Video/3=Audio/4=Text/5=Image}`。
- 实作：`AlistPath{Type} + IsDir()=Type==1`（`client.go:45,59`），丢 `is_dir/created/hashinfo`。
- 动作：`AlistPath` 新增 `IsDirFlag *bool json:"is_dir"`，`IsDir()` 优先用它，`Type==1` 仅回退；`FSList/FSGet` 保持兼容。
- [x] 待实作 → 验收：`type:2 + is_dir:false` 判文件，`type:1 + is_dir:true` 判目录。

## T4 列表分页截断（本轮 P0）

- 声明：`per_page min1/max100/default30`，需翻页；响应含 `total`。
- 实作：`FSListLight` 写死 `page:1,per_page:0` 且不循环（`client.go:408`），大目录静默丢文件。
- 动作：`per_page=100` 循环取到 `len<per_page || len>=total`；`FullPath` 归一化去重斜杠。
- [x] 待实作 → 验收：httptest 两页（100+1）能取全 101 条。

## T5 请求头写死 + 任务模型缺字段（本轮 P1）

- 声明：流式上传需 `Content-Type:octet-stream + File-Path` 自定义头。
- 实作：`makeHeaders/doRequest` 写死 `application/json`，无覆盖入口（`client.go:268,295`）。
- 动作：`doRequest` 新增 `doRequestWithHeaders`，支持额外头覆盖（为后续真流式上传留口，本轮不启用）；`TaskInfoData` 补 `Error string`。
- [x] 待实作 → 验收：`go build + go test` 全过。

## 验证

```bash
go build ./...
go test ./pkg/alist/... ./internal/modules/alistsync/... -count=1 -v
```
