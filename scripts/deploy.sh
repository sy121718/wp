#!/bin/bash
# deploy.sh — 查找最新压缩包并 rsync 传输到服务器
# 用法: ./scripts/deploy.sh
set -euo pipefail

# ========== 依赖检查 ==========
for cmd in sshpass rsync; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "错误: $cmd 未安装，请先安装"
    echo "  Ubuntu/Debian: sudo apt install -y $cmd"
    echo "  macOS:         brew install $cmd"
    exit 1
  fi
done
# ================================

# ========== 服务器配置 ==========
REMOTE_USER="${REMOTE_USER:-root}"
REMOTE_HOST="${REMOTE_HOST:-43.106.91.190}"
REMOTE_PATH="${REMOTE_PATH:-/www/wwwroot/erp}"
: "${REMOTE_PASS:?请先设置 REMOTE_PASS 环境变量}"
# ================================

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

echo "=========================================="
echo "  一键部署 - $(date)"
echo "=========================================="

# [1/3] 查找最新压缩包
ARCHIVE=$(ls -t dist/embed/*_amd64.tar.gz 2>/dev/null | head -1)
if [ -z "$ARCHIVE" ]; then
  echo "错误: 未找到 dist/embed/*_amd64.tar.gz"
  exit 1
fi
echo ""
echo "[1/3] 使用压缩包: $ARCHIVE ($(du -h "$ARCHIVE" | cut -f1))"

# [2/3] 传输到服务器
echo ""
echo "[2/3] 传输到 ${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_PATH}/ ..."
sshpass -p "$REMOTE_PASS" rsync -avz --progress \
  "$ARCHIVE" \
  "${REMOTE_USER}@${REMOTE_HOST}:${REMOTE_PATH}/"
echo "  传输完成"

# [3/3] 重启远程服务
echo ""
echo "[3/3] 重启远程服务..."
sshpass -p "$REMOTE_PASS" ssh -o StrictHostKeyChecking=no \
  "${REMOTE_USER}@${REMOTE_HOST}" \
  "cd ${REMOTE_PATH} && bash deploy.sh"

echo ""
echo "=========================================="
echo "  部署完成 - $(date)"
echo "=========================================="