# 构建参数
ARG VERSION=dev
ARG GO_VERSION=1.25
ARG NODE_VERSION=22

# ========= 前端构建阶段 =========
FROM node:${NODE_VERSION}-alpine AS web-builder

WORKDIR /build/webui
COPY webui/package.json webui/pnpm-lock.yaml ./
RUN npm install --global pnpm@9.15.9 && pnpm install --frozen-lockfile
COPY webui/ .
RUN pnpm run build

# ========= Go 构建阶段 =========
FROM golang:${GO_VERSION}-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache git gcc musl-dev

WORKDIR /build

# 复制go mod文件
COPY go.mod go.sum* ./
RUN go mod download

# 复制源代码
COPY . .

# 从前端构建阶段复制 dist
COPY --from=web-builder /build/internal/web/dist ./internal/web/dist

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
RUN mkdir -p /config /logs /fonts /media /data

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /build/autofilm /app/autofilm

# 设置权限
RUN chmod +x /app/autofilm

# 挂载点
VOLUME ["/app/config", "/app/logs", "/app/data", "/fonts", "/media"]

# 默认命令
CMD ["/app/autofilm"]
