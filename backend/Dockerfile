# ---------- 构建阶段：编译 Go 二进制 ----------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# 纯 Go 构建（modernc.org/sqlite 不需要 CGO），静态链接便于在 alpine 运行
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS=-mod=mod

# 先拷贝依赖清单，利用镜像层缓存，代码变更时无需重新下载依赖
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=off

RUN go mod download

# 再拷贝源码并编译
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/ssedeploy ./cmd/server

# ---------- 运行阶段：最小化镜像 ----------
# 使用 glibc 基础镜像（debian-slim）：NVIDIA 容器运行时会挂载宿主机的
# nvidia-smi 二进制与驱动库，alpine 的 musl 无法执行该二进制，会导致
# 平台 GPU 识别数量为 0。debian 下再配合 GPU 直通扩展即可正常采集。
FROM debian:bookworm-slim

# 版本标识：由打包脚本通过 --build-arg APP_VERSION 注入，便于后续升级与排查
ARG APP_VERSION=dev
LABEL org.opencontainers.image.title="ssedeploy-backend" \
      org.opencontainers.image.description="SSEDeploy 平台后端（Gin + client-go + SQLite）" \
      org.opencontainers.image.version="${APP_VERSION}"

# ca-certificates：访问 Kubernetes API（HTTPS）需要
# tzdata：日志与图表的时间显示
# wget：compose healthcheck 使用
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata wget \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# 只拷贝编译产物，镜像体积更小
COPY --from=builder /out/ssedeploy /app/ssedeploy

# 默认配置：监听 8080，SQLite 落在 /app/data（由 compose 挂载持久化）
ENV SSD_HTTP_ADDR=:8080 \
    SSD_DB_PATH=/app/data/ssedeploy.db \
    SSD_K8S_NAMESPACE=ssedeploy \
    TZ=Asia/Shanghai

EXPOSE 8080

# 数据目录：SQLite 数据库文件持久化位置
VOLUME ["/app/data"]

ENTRYPOINT ["/app/ssedeploy"]
