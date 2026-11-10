#!/usr/bin/env bash
# =============================================================================
# 开发机打包脚本（Linux / macOS，需要 Docker 与 zip）
#
# 功能：构建前端与后端镜像 → 拉取内嵌 K3s 镜像 → 组装离线目录 → 导出镜像 tar → 打包 zip
#
# 用法：
#   bash deploy/offline/scripts/build-package.sh              # 使用 VERSION 文件中的版本
#   bash deploy/offline/scripts/build-package.sh 1.0.1        # 指定版本
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OFFLINE_DIR="$REPO_ROOT/deploy/offline"

VERSION="${1:-$(cat "$REPO_ROOT/VERSION" | tr -d '[:space:]')}"
[ -n "$VERSION" ] || { echo "无法读取版本号，请检查 VERSION 文件" >&2; exit 1; }

K3S_IMAGE="rancher/k3s:v1.35.5-k3s1"
DIST_ROOT="$REPO_ROOT/dist"
PKG_DIR="$DIST_ROOT/ssedeploy-$VERSION"
IMAGES_DIR="$PKG_DIR/images"
IMAGE_TAR="$IMAGES_DIR/ssedeploy-images-$VERSION.tar"

echo "==> SSEDeploy 打包开始，版本：$VERSION"

# 0. 环境检查
if ! docker info >/dev/null 2>&1; then
  echo "Docker 未就绪，请先启动 dockerd" >&2
  exit 1
fi

# 1. 构建业务镜像（多阶段构建内部完成 npm build 与 go build）
echo "==> 构建后端镜像 ssedeploy/backend:$VERSION"
docker build -t "ssedeploy/backend:$VERSION" --build-arg "APP_VERSION=$VERSION" "$REPO_ROOT/backend"

echo "==> 构建前端镜像 ssedeploy/frontend:$VERSION"
docker build -t "ssedeploy/frontend:$VERSION" --build-arg "APP_VERSION=$VERSION" "$REPO_ROOT/frontend"

# 2. 拉取内嵌 K3s 镜像
echo "==> 拉取 K3s 镜像 $K3S_IMAGE"
docker pull "$K3S_IMAGE"

# 3. 组装交付目录
rm -rf "$PKG_DIR"
mkdir -p "$IMAGES_DIR" "$PKG_DIR/manifests" "$PKG_DIR/scripts"

cp "$OFFLINE_DIR/docker-compose.yml" "$PKG_DIR/"
cp "$OFFLINE_DIR/README.md" "$PKG_DIR/"
cp "$OFFLINE_DIR/.env.example" "$PKG_DIR/"
cp "$OFFLINE_DIR/manifests/shared-pv.yaml" "$PKG_DIR/manifests/"
echo "$VERSION" > "$PKG_DIR/VERSION"

for f in backend-entrypoint.sh start.ps1 start.sh stop.ps1 stop.sh load-images.ps1 load-images.sh clean.ps1 clean.sh; do
  cp "$OFFLINE_DIR/scripts/$f" "$PKG_DIR/scripts/"
done
# 保证容器内脚本可执行（Windows 打包时可能丢失执行位）
chmod +x "$PKG_DIR/scripts/"*.sh "$PKG_DIR/scripts/"*.ps1 2>/dev/null || true

# 4. 导出镜像（合并为一个 tar）
echo "==> 导出镜像到 $IMAGE_TAR（体积较大，请耐心等待）"
docker save -o "$IMAGE_TAR" \
  "ssedeploy/backend:$VERSION" \
  "ssedeploy/frontend:$VERSION" \
  "$K3S_IMAGE"
printf 'ssedeploy/backend:%s\nssedeploy/frontend:%s\n%s\n' "$VERSION" "$VERSION" "$K3S_IMAGE" \
  > "$IMAGES_DIR/images.txt"

# 5. 生成 .env（写入版本号）
sed "s/^APP_VERSION=.*/APP_VERSION=$VERSION/" "$OFFLINE_DIR/.env.example" > "$PKG_DIR/.env"

# 6. 压缩
ZIP_PATH="$DIST_ROOT/ssedeploy-$VERSION.zip"
rm -f "$ZIP_PATH"
echo "==> 压缩离线包 $ZIP_PATH"
(cd "$DIST_ROOT" && zip -qr "$(basename "$ZIP_PATH")" "ssedeploy-$VERSION")

echo ""
echo "==> 打包完成"
echo "    目录：$PKG_DIR"
echo "    压缩包：$ZIP_PATH"
echo "    版本：$VERSION"
echo "    交付方式：拷贝 zip 到目标机，解压后执行 scripts/start.sh（Linux）或 scripts/start.ps1（Windows）"
