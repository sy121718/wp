#!/usr/bin/env bash
# check-service-db-boundary.sh — service 层的数据访问边界（审计 CQ-025）。
#
# 为什么需要一条脚本而不是靠评审记：AGENTS.md 给 admin 开了「service 层可经
# DB(ctx) 直查」的明文豁免，但豁免的边界只写在文档里 —— 新模块照抄 admin 的写法
# 不会有任何东西拦下来，而一旦破窗，表隔离约定就靠自觉了。
#
# 两条判据（都命中即失败）：
#   ① 非 admin 模块的 service 出现 DB(ctx) / RevisionDB(ctx)；
#   ② 任何模块的 service 直接 import 其它模块的 model / service 包 ——
#      service 只能依赖对方 contract 与不可变 dto，绕过它等于绕过对方的仓储方法。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET="$ROOT/internal/module"

if [ ! -d "$TARGET" ]; then
  echo "找不到目标目录：$TARGET" >&2
  exit 2
fi

failed=0

# 判据 ①：service 目录下的裸 gorm 句柄（admin 豁免）。
DIRECT=$(grep -rn --include='*.go' -E '\.(DB|RevisionDB)\(ctx\)' "$TARGET" \
  | grep '/service/' \
  | grep -v '/admin/service/' \
  | grep -v '_test\.go:' \
  | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' || true)

if [ -n "$DIRECT" ]; then
  echo "✗ 非 admin 模块的 service 直接使用了裸 gorm 句柄：" >&2
  echo "$DIRECT" >&2
  echo "" >&2
  echo "  持久化的唯一入口是 model 的具名方法；缺查询就给 model 加方法，" >&2
  echo "  不要在 service 里拼 .Where()/.Create()（AGENTS.md「model 层定位」）。" >&2
  echo "  仅 admin 的简单 CRUD 有豁免。" >&2
  failed=1
fi

# 判据 ②：service 跨模块 import model / service。
CROSS=$(python3 - "$TARGET" <<'PY'
import glob, os, re, sys

target = sys.argv[1]
hits = []
pattern = re.compile(r'"go_wp/internal/module/([^"]+)/(model|service)"')

def owner_of(path):
    """internal/module/<owner>/.../service/x.go → <owner>（含两级模块如 product/inventory）。"""
    parts = path.split(os.sep)
    try:
        i = parts.index("module")
    except ValueError:
        return ""
    rest = parts[i + 1:]
    j = rest.index("service")
    return "/".join(rest[:j])

for path in glob.glob(os.path.join(target, "**", "service", "*.go"), recursive=True):
    if path.endswith("_test.go"):
        continue
    owner = owner_of(path)
    try:
        src = open(path, encoding="utf-8").read()
    except OSError:
        continue
    for m in pattern.finditer(src):
        other = m.group(1)
        # 本模块（含子模块）之间的引用不算跨模块：product 与 product/inventory 是一棵树。
        if other == owner or other.startswith(owner + "/") or owner.startswith(other + "/"):
            continue
        hits.append("%s -> %s" % (os.path.relpath(path, os.path.dirname(target)), m.group(0)))

for h in sorted(set(hits)):
    print(h)
PY
)

if [ -n "$CROSS" ]; then
  echo "✗ service 直接 import 了其它模块的 model / service：" >&2
  echo "$CROSS" >&2
  echo "" >&2
  echo "  跨模块只能用对方 contract 与不可变 dto（internal/module/CLAUDE.md「表隔离约定」）。" >&2
  echo "  对方的能力不够用就给对方 contract 加方法，不要伸进它的仓储层。" >&2
  echo "  注意：只有模块间才算 —— product 与 product/inventory 属同一棵树，直接引用不算违规。" >&2
  failed=1
fi

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "✓ service 层数据访问边界无违规（无直查、无跨模块 model/service 引用）"
