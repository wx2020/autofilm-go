# AutoFilm Go

AutoFilm 的 Go 语言重写版本，提供与 Python 版本相同的功能，但具有更好的性能和更小的资源占用。

## 功能特性

- **Alist2Strm**: 将 Alist 云存储中的媒体文件转换为 .strm 文件，支持 Emby/Jellyfin 直链播放
- **Ani2Alist**: 集成 ANI Open 项目，自动挂载动漫内容到 Alist
- **LibraryPoster**: 自动生成和更新媒体库海报

## 快速开始

### 使用 Docker

```bash
docker run -d \
  --name autofilm \
  -v ./config:/config \
  -v ./logs:/logs \
  -v ./fonts:/fonts:ro \
  -v ./media:/media \
  -e TZ=Asia/Shanghai \
  akimio/autofilm-go:latest
```

### 使用 Docker Compose

```bash
docker-compose up -d
```

### 本地运行

```bash
# 安装依赖
go mod download

# 构建
go build -o autofilm ./cmd/autofilm

# 运行
./autofilm
```

## 配置

配置文件位于 `config/config.yaml`，首次运行会自动创建默认配置。

### Alist2Strm 配置

```yaml
Alist2StrmList:
  - id: "my-server"
    url: "http://localhost:5244"
    username: ""
    password: ""
    token: ""
    public_url: ""
    source_dir: "/"
    target_dir: "/media"
    flatten_mode: false
    subtitle: false
    image: false
    nfo: false
    mode: "AlistURL"
    overwrite: false
    max_workers: 50
    max_downloaders: 5
    sync_server: false
    smart_protection:
      enabled: true
      threshold: 100
      grace_scans: 3
    cron: "0 */6 * * *"
```

### Ani2Alist 配置

```yaml
Ani2AlistList:
  - id: "anime"
    url: "http://localhost:5244"
    username: ""
    password: ""
    token: ""
    target_dir: "/Anime"
    rss_update: true
    src_domain: "aniopen.an-i.workers.dev"
    rss_domain: "api.ani.rip"
    cron: "0 */12 * * *"
```

### LibraryPoster 配置

```yaml
LibraryPosterList:
  - id: "poster"
    url: "http://localhost:8096"
    api_key: "your-api-key"
    title_font_path: "/fonts/title.ttf"
    subtitle_font_path: "/fonts/subtitle.ttf"
    configs:
      - library_name: "Movies"
        title: "电影"
        subtitle: "Movie Library"
        limit: 15
    cron: "0 4 * * *"
```

## 模式说明

### Alist2Strm 模式

- **AlistURL**: 使用 Alist 下载链接（默认）
- **RawURL**: 使用原始直链
- **AlistPath**: 使用 Alist 路径

## 项目结构

```
autofilm-go/
├── cmd/
│   └── autofilm/          # 主程序入口
├── internal/
│   ├── core/              # 核心模块（配置、日志）
│   ├── extensions/        # 扩展（文件类型、Logo）
│   ├── modules/           # 功能模块
│   │   ├── alist2strm/    # Alist转STRM
│   │   ├── ani2alist/     # ANI转Alist
│   │   └── libraryposter/ # 库海报生成
│   └── utils/             # 工具函数
├── pkg/                   # 公共包
│   ├── alist/             # Alist客户端
│   └── httpclient/        # HTTP客户端
├── config/                # 配置文件目录
├── logs/                  # 日志文件目录
├── fonts/                 # 字体文件目录
└── Dockerfile             # Docker构建文件
```

## 性能优势

相比 Python 版本：

- **更低的内存占用**: 约 30-50 MB vs 200-300 MB
- **更快的启动速度**: 毫秒级 vs 秒级
- **更好的并发性能**: 原生 goroutine 支持
- **更小的镜像体积**: 约 20 MB vs 300+ MB

## 开发

```bash
# 格式化代码
go fmt ./...

# 运行测试
go test ./...

# 构建多平台版本
GOOS=linux GOARCH=amd64 go build -o autofilm-linux-amd64 ./cmd/autofilm
GOOS=windows GOARCH=amd64 go build -o autofilm-windows-amd64.exe ./cmd/autofilm
GOOS=darwin GOARCH=amd64 go build -o autofilm-darwin-amd64 ./cmd/autofilm
```

## 许可证

MIT License

## 致谢

本项目基于 [AutoFilm](https://github.com/akimio/AutoFilm) Python 版本重写。
