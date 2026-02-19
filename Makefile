.PHONY: all build run test clean docker-build docker-push

# 变量定义
BINARY_NAME=autofilm
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -w -s"

# Go 相关
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Docker 相关
DOCKER_IMAGE=autofilm-go
DOCKER_TAG=$(VERSION)

all: build

## build: 编译二进制文件
build:
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) ./cmd/autofilm

## run: 运行程序
run: build
	./$(BINARY_NAME)

## test: 运行测试
test:
	$(GOTEST) -v ./...

## clean: 清理构建文件
clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@rm -f *.exe
	@rm -rf dist/

## docker-build: 构建Docker镜像
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_IMAGE):latest

## docker-push: 推送Docker镜像
docker-push: docker-build
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

## deps: 下载依赖
deps:
	$(GOMOD) download
	$(GOMOD) tidy

## fmt: 格式化代码
fmt:
	$(GOCMD) fmt ./...

## vet: 代码检查
vet:
	$(GOCMD) vet ./...

## build-all: 构建多平台版本
build-all: clean
	@echo "Building for multiple platforms..."
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-amd64 ./cmd/autofilm
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-arm64 ./cmd/autofilm
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-windows-amd64.exe ./cmd/autofilm
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-amd64 ./cmd/autofilm
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-arm64 ./cmd/autofilm
	@echo "Build complete!"

help: ## 显示帮助信息
	@echo "可用的make命令:"
	@grep -E '^## ' ${MAKEFILE_LIST} | sed 's/^## /  /'
