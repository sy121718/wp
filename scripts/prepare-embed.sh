#!/bin/bash
# prepare-embed.sh — 构建前端并复制到 internal/embed/dist/
# 嵌入模式：前端资源编译进 Go 二进制，单文件即可运行
# 用法: ./scripts/prepare-embed.sh
# 产物: internal/embed/dist/  — 前端构建产物（被 //go:embed 编译进二进制）

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
EMBED_DIST_DIR="$ROOT_DIR/internal/embed/dist"

echo "=========================================="
echo "  嵌入模式前端准备 - $(date)"
echo "  目标: $EMBED_DIST_DIR"
echo "=========================================="

cd "$ROOT_DIR/web"

# 安装依赖
if [ ! -d "node_modules" ]; then
  echo ""
  echo "[1/3] 安装前端依赖..."
  pnpm install
else
  echo ""
  echo "[1/3] node_modules 已存在，跳过安装"
fi

# 构建
echo ""
echo "[2/3] 构建前端..."
pnpm build

# 复制到嵌入目录（清空旧文件，复制新文件）
echo ""
echo "[3/3] 复制到 internal/embed/dist/ ..."
rm -rf "$EMBED_DIST_DIR"
cp -r dist "$EMBED_DIST_DIR"

# 确保 .gitkeep 占位被移除（不再需要）
rm -f "$EMBED_DIST_DIR/.gitkeep"

echo ""
echo "=========================================="
echo "  嵌入模式前端准备完成 ✅"
echo "  前端已嵌入: internal/embed/dist/"
echo "  （共 $(find "$EMBED_DIST_DIR" -type f | wc -l) 个文件）"
echo "=========================================="