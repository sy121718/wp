#!/usr/bin/env bash
# check-shell-migration.sh — internal/shell 与 internal/backref 退役后的终局守卫。
#
# 历史背景：这两个包是整页跳转提示与回跳包装的过渡层，迁移期间用「计数只降不增」的门禁
# 推进。两个包现已整体删除，计数门禁没有对象可数 —— 按任务书要求，退化为对终局的
# 直接断言，而不是「总是通过」：
#
#   1. 目录不得复活：internal/shell、internal/backref 不存在；
#   2. 不得有人重新 import 它们（`//go:embed` / 生成代码同样算）；
#   3. 不得恢复整页跳转提示：Go/模板里没有 meta http-equiv="refresh"，
#      模板里没有 jump-page 这类旧跳转页标记；
#   4. 安全回跳解析仍在正式属主：pkg/backpath 与 pkg/safepath 存在且被使用。
#
# 判据全部是「命中即失败」，扫描输入为空（例如扫描根写错）同样失败，不会静默空转。
#
# 用法：bash scripts/check-shell-migration.sh
# 退出码：0 通过 / 1 违规 / 2 环境或扫描输入问题。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

for dir in internal/shell internal/backref; do
  if [ -e "$dir" ]; then
    echo "✗ 旧包目录仍在：$dir —— 它已在终局删除，不得恢复" >&2
    exit 1
  fi
done

tmp_imports=$(mktemp)
tmp_refresh=$(mktemp)
trap 'rm -f "$tmp_imports" "$tmp_refresh"' EXIT

grep -rn --include='*.go' -E '"go_wp/internal/(shell|backref)"' . 2>/dev/null > "$tmp_imports" || true
if [ -s "$tmp_imports" ]; then
  echo "✗ 仍有人在 import 已删除的旧包：" >&2
  sed 's/^/    /' "$tmp_imports" >&2
  exit 1
fi

grep -rn --include='*.go' --include='*.html' --include='*.jet' -E 'http-equiv=["'"'"']?refresh' internal pkg public 2>/dev/null > "$tmp_refresh" || true
if [ -s "$tmp_refresh" ]; then
  echo "✗ 出现 meta refresh 整页跳转（写操作结果必须走统一反馈出口）：" >&2
  sed 's/^/    /' "$tmp_refresh" >&2
  exit 1
fi

if grep -rn --include='*.html' --include='*.jet' --include='*.css' -E 'jump-page|jump-[a-z]' internal pkg 2>/dev/null | grep -v 'visual-baseline' > /dev/null; then
  echo "✗ 出现旧整页跳转页模板/样式（jump-page）：应走共享提示组件与失败出口" >&2
  grep -rn --include='*.html' --include='*.jet' --include='*.css' -E 'jump-page' internal pkg 2>/dev/null | sed 's/^/    /' >&2
  exit 1
fi

for f in pkg/backpath/backpath.go pkg/safepath/safepath.go; do
  if [ ! -f "$f" ]; then
    echo "✗ 安全回跳解析的正式属主缺失：$f" >&2
    exit 1
  fi
done
if ! grep -rq --include='*.go' 'backpath\.FromRequest' internal 2>/dev/null; then
  echo "✗ 没有生产消费者使用 backpath.FromRequest —— 扫描输入为空或回跳解析被误删" >&2
  exit 1
fi

echo "✓ 旧跳转包装已退役：无 internal/shell、internal/backref、meta refresh 与 jump-page"
