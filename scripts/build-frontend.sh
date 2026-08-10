#!/bin/bash
# build-frontend.sh — 构建前端，输出到指定目录
# 用法: ./scripts/build-frontend.sh [output_dir]
#   output_dir  - 构建产物输出目录（默认 dist/separate/frontend）

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="${1:-$ROOT_DIR/dist/separate/frontend}"

echo "=========================================="
echo "  前端构建 - $(date)"
echo "  输出目录: $OUTPUT_DIR"
echo "=========================================="

cd "$ROOT_DIR/web"

# 安装依赖（如 node_modules 不存在则安装）
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

echo ""
echo "[3/3] 复制到输出目录..."
rm -rf "$OUTPUT_DIR"
mkdir -p "$(dirname "$OUTPUT_DIR")"
cp -r dist "$OUTPUT_DIR"

echo ""
echo "=========================================="
echo "  前端构建完成 ✅"
echo "  输出: $OUTPUT_DIR"
echo "=========================================="