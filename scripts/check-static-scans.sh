#!/usr/bin/env bash
# Go 层的静态扫描门禁（不需要数据库）。
#
# 为什么需要它：public/test/security 与 public/test/architecture 是两条**纯静态**的
# Go 扫描 —— 前者扫 DTO 形状与凭据字段暴露，后者扫跨模块事务边界与目录纪律。它们
# 编译不需要外部依赖、也不碰 PG，却只挂在 `make test` 上：于是 `make check-all`
# 全绿 **不等于** 这两层绿。本仓真的靠它们各抓到一个回归（拆文件后事务豁免键失效、
# AI DTO 的 7 个整型字段命中敏感词基线），所以把它们并入统一门禁。
#
# 与既有 shell 扫描的分工：shell 脚本扫**文本形态**（正则在源码上跑），这两个扫
# **类型与调用图形态**（要解析 Go 语法、要跟 `go list` 的包图），因此只有 Go 能做。
#
# 用法：scripts/check-static-scans.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

PKGS=(
  ./public/test/security/...
  ./public/test/architecture/...
)

echo "== Go 静态扫描（${#PKGS[@]} 组包，无需数据库）=="

FAILED=()
for pkg in "${PKGS[@]}"; do
  echo
  echo "── go test $pkg"
  if ! go test -count=1 "$pkg"; then
    FAILED+=("$pkg")
  fi
done

echo
if (( ${#FAILED[@]} > 0 )); then
  echo "✗ Go 静态扫描未通过（${#FAILED[@]}/${#PKGS[@]}）：${FAILED[*]}" >&2
  exit 1
fi
echo "✓ Go 静态扫描通过（共 ${#PKGS[@]} 组包）"
