# 发布流程说明

本项目使用 GitHub Actions 自动化发布流程。当您推送带有 `v*.*.*` 格式的标签时，会自动触发构建和发布。

## 自动发布流程

### 触发发布

创建并推送版本标签：

```bash
# 创建标签（例如 v1.0.0）
git tag v1.0.0

# 推送标签到 GitHub
git push origin v1.0.0

# 或者推送所有标签
git push origin --tags
```

### 自动构建内容

当推送标签后，GitHub Actions 会自动执行以下操作：

1. **构建 Linux AMD64 二进制文件**
   - 平台：linux/amd64
   - 输出：`autofilm-linux-amd64.tar.gz`
   - 注入版本号到二进制文件中

2. **构建并推送 Docker 镜像**
   - 注册表：`ghcr.io/akimio/autofilm`
   - 标签示例：
     - `ghcr.io/akimio/autofilm:v1.0.0`
     - `ghcr.io/akimio/autofilm:v1.0`
     - `ghcr.io/akimio/autofilm:v1`
     - `ghcr.io/akimio/autofilm:latest`
   - 平台：linux/amd64

3. **创建 GitHub Release**
   - 自动附加二进制文件
   - 生成包含 Docker 拉取命令和使用说明的 Release Notes
   - 发布到 GitHub Releases 页面

### 使用发布产物

#### Docker 方式（推荐）

```bash
# 拉取特定版本
docker pull ghcr.io/akimio/autofilm:v1.0.0

# 拉取最新版本
docker pull ghcr.io/akimio/autofilm:latest

# 运行
docker run -d \
  -v /path/to/config:/config \
  -v /path/to/logs:/logs \
  ghcr.io/akimio/autofilm:v1.0.0
```

#### 二进制文件方式

1. 从 [Releases](https://github.com/akimio/autofilm/releases) 页面下载 `autofilm-linux-amd64.tar.gz`
2. 解压并赋予执行权限

```bash
# 解压
tar -xzf autofilm-linux-amd64.tar.gz

# 赋予执行权限
chmod +x autofilm-linux-amd64

# 运行
./autofilm-linux-amd64
```

### 版本号注入

构建过程中，版本号会自动注入到二进制文件中，程序启动时会显示正确的版本信息。

- Docker 镜像会通过 `VERSION` 构建参数注入版本号
- 二进制文件通过 `-ldflags` 在编译时注入版本号

### 工作流文件

发布流程配置文件位于：[`.github/workflows/release.yml`](.github/workflows/release.yml)

## 注意事项

1. **标签格式**：必须使用 `v` 开头的语义化版本号（如 `v1.0.0`）
2. **权限**：需要 GitHub Token 有 `contents: write` 和 `packages: write` 权限
3. **构建时间**：Docker 镜像构建和上传可能需要几分钟时间
4. **存储空间**：Docker 镜像会存储在 GitHub Container Registry 中

## 本地构建测试

如果想在本地测试构建：

```bash
# 构建 Linux 二进制文件
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags="-w -s -X github.com/akimio/autofilm/internal/core.Version=v1.0.0-local" \
  -o autofilm-linux-amd64 \
  ./cmd/autofilm

# 构建 Docker 镜像
docker build --build-arg VERSION=v1.0.0-local -t autofilm:local .
```
