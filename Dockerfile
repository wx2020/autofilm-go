# 构建参数
ARG VERSION=dev
ARG GO_VERSION=1.24

# 多阶段构建
FROM golang:${GO_VERSION}-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache git gcc musl-dev

WORKDIR /build

# 复制go mod文件
COPY go.mod go.sum* ./
RUN go mod download

# 复制源代码
COPY . .

# 构建参数传递
ARG VERSION

# 编译，注入版本号
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -installsuffix cgo \
    -ldflags="-w -s -X github.com/akimio/autofilm/internal/core.Version=${VERSION}" \
    -o autofilm ./cmd/autofilm

# 最终镜像
FROM alpine:latest

# 安装运行时依赖
RUN apk add --no-cache ca-certificates tzdata

# 设置时区
ENV TZ=Asia/Shanghai

# 创建目录
RUN mkdir -p /config /logs /fonts /media

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/autofilm /app/autofilm

# 设置权限
RUN chmod +x /app/autofilm

# 挂载点
VOLUME ["/config", "/logs", "/fonts", "/media"]

# 默认命令
CMD ["/app/autofilm"]
