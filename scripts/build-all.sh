#!/bin/bash
# build-all.sh — Go-MVC 完整打包脚本
# 使用 GoReleaser 构建后端，支持嵌入和分离两种模式。
#
# 用法:
#   ./scripts/build-all.sh embed              # 嵌入模式打包
#   ./scripts/build-all.sh separate           # 分离模式打包
#   ./scripts/build-all.sh embed v1.0.0       # 指定版本
#
# 环境变量:
#   GOOS        - 目标操作系统（默认 linux）
#   GOARCH      - 目标架构（默认 amd64）

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
GORELEASER_DIR="$ROOT_DIR/dist/goreleaser"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
VERSION="${2:-$TIMESTAMP}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"

# 颜色输出
info()  { echo -e "\033[36m[INFO] $*\033[0m"; }
ok()    { echo -e "\033[32m[OK] $*\033[0m"; }
err()   { echo -e "\033[31m[ERROR] $*\033[0m" >&2; }

usage() {
  cat <<EOF
用法: ./scripts/build-all.sh <mode> [version]

mode:
  embed    嵌入模式 — 前端 + 后端合并为单二进制
  separate 分离模式 — 前端和后端分别打包

version: 版本号（默认时间戳）

环境变量: GOOS, GOARCH
EOF
  exit 1
}

check_deps() {
  if ! command -v goreleaser &>/dev/null; then
    err "goreleaser 未安装，请先安装: go install github.com/goreleaser/goreleaser/v2@latest"
    exit 1
  fi
}

# ============ embed 模式 ============
do_embed() {
  local goreleaser_config="$ROOT_DIR/.goreleaser.embed.yaml"

  echo "=========================================="
  echo "  嵌入模式打包"
  echo "  版本: $VERSION"
  echo "  目标: $GOOS/$GOARCH"
  echo "=========================================="

  # Step 1: 构建前端
  info ""
  info "[1/3] 构建前端..."
  "$ROOT_DIR/scripts/prepare-embed.sh"

  # Step 2: 使用 GoReleaser 构建后端并打包
  info ""
  info "[2/3] GoReleaser 构建后端 + 打包..."
  cd "$ROOT_DIR"

  export GOOS GOARCH
  info "  GOOS=$GOOS  GOARCH=$GOARCH"

  goreleaser release \
    --config "$goreleaser_config" \
    --snapshot \
    --clean

  ok "GoReleaser 构建完成"

  # Step 3: 整理产物到 dist/embed/
  info ""
  info "[3/3] 整理产物..."
  mkdir -p "$DIST_DIR/embed"
  mv "$GORELEASER_DIR"/*.tar.gz "$DIST_DIR/embed/" 2>/dev/null || true
  mv "$GORELEASER_DIR"/*.txt "$DIST_DIR/embed/" 2>/dev/null || true
  rm -rf "$GORELEASER_DIR" "$ROOT_DIR/dist/embed/web"

  echo ""
  echo "=========================================="
  echo "  嵌入模式打包完成 ✅"
  ls -lh "$DIST_DIR/embed/"*.tar.gz 2>/dev/null | awk '{print "  " $NF " (" $5 ")"}'
  echo "=========================================="
  echo "  部署: tar -xzf <包> && ./app"
  echo "=========================================="
}

# ============ separate 模式 ============
do_separate() {
  local frontend_dir="$DIST_DIR/separate/frontend"
  local frontend_archive="go-mvc_frontend_${VERSION}.tar.gz"
  local goreleaser_config="$ROOT_DIR/.goreleaser.separate.yaml"

  echo "=========================================="
  echo "  分离模式打包"
  echo "  版本: $VERSION"
  echo "  目标: $GOOS/$GOARCH"
  echo "=========================================="

  # Step 1: 构建前端
  info ""
  info "[1/3] 构建前端..."
  "$ROOT_DIR/scripts/build-frontend.sh" "$frontend_dir"

  # Step 2: 打包前端
  info ""
  info "[2/3] 打包前端..."
  mkdir -p "$DIST_DIR/separate"
  cd "$ROOT_DIR/web"
  tar -czf "$DIST_DIR/separate/$frontend_archive" dist/
  ok "前端包: $DIST_DIR/separate/$frontend_archive"

  # Step 3: 使用 GoReleaser 构建后端并打包
  info ""
  info "[3/3] GoReleaser 构建后端 + 打包..."
  cd "$ROOT_DIR"

  export GOOS GOARCH
  info "  GOOS=$GOOS  GOARCH=$GOARCH"

  goreleaser release \
    --config "$goreleaser_config" \
    --snapshot \
    --clean

  ok "GoReleaser 构建完成"

  # 整理产物到 dist/separate/
  mv "$GORELEASER_DIR"/*.tar.gz "$DIST_DIR/separate/" 2>/dev/null || true
  mv "$GORELEASER_DIR"/*.txt "$DIST_DIR/separate/" 2>/dev/null || true
  rm -rf "$GORELEASER_DIR" "$frontend_dir"

  echo ""
  echo "=========================================="
  echo "  分离模式打包完成 ✅"
  ls -lh "$DIST_DIR/separate/"*.tar.gz 2>/dev/null | awk '{print "  " $NF " (" $5 ")"}'
  echo "=========================================="
  echo "  部署:"
  echo "    后端: tar -xzf <backend包> && ./app"
  echo "    前端: 解压 frontend 包并配置 nginx 代理到后端 API"
  echo "=========================================="
}

# ============ 入口 ============
if [ $# -lt 1 ]; then
  usage
fi

check_deps

MODE="$1"

case "$MODE" in
  embed)
    do_embed
    ;;
  separate)
    do_separate
    ;;
  *)
    err "未知模式: $MODE，可选: embed、separate"
    usage
    ;;
esac