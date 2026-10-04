#!/usr/bin/env bash
# 全量门禁的统一入口（审计 R-02 / R-03）。
#
# 为什么需要它：门禁脚本曾散在 scripts/ 下 11 个，Makefile 只跑 3 个、CI 只跑 6 个 ——
# 「本地全绿 ≠ CI 绿」，而**没人调用的脚本永远不会变红**（其中 4 个连一次都没被自动跑过，
# 包括迁移 262 的上线只读预检）。入口收敛后，新增脚本只要加进下面的数组就自动纳入。
#
# 用法：
#   scripts/check-all.sh              # 无外部依赖的门禁（unit 层可跑）
#   scripts/check-all.sh --db-only    # 只跑需要 PostgreSQL 的门禁（必须排在迁移之后）
#   scripts/check-all.sh --with-db    # 两组合并（本地跑全量）
#
# 与 CI 的分工：CI 的 unit job 跑无依赖组、integration job 跑 `--db-only` 组 ——
# 两个 job 合起来正好是全量，且都经由这一个入口。
set -uo pipefail

# 与 CI 同口径：多端 CSS 守卫只在 error 模式下才是硬门禁（默认 warn 会静默放过）。
export SKY_CSS_GUARD=error

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

MODE="plain"
case "${1:-}" in
  --db-only) MODE="db" ;;
  --with-db) MODE="all" ;;
  "" ) MODE="plain" ;;
  *) echo "用法：$0 [--db-only|--with-db]" >&2; exit 2 ;;
esac

# 无外部依赖（纯文件扫描 / 纯计算），unit 层可跑。
PLAIN_SCRIPTS=(
  check-no-internal-error-leak.sh
  check-i18n-coverage.sh
  check-i18n-keys-seeded.sh
  check-service-db-boundary.sh
  check-page-endpoint-authz.py
  check-multidevice-css.sh
  check-empty-state-table-head.sh
  check-inventory-sku-format.sh
  check-stock-sku-prefix-collisions.sh
  check-workbench.sh
  check-contract-deps.sh
  check-dto-immutability.sh
)

# 需要真实 PostgreSQL，且必须在迁移（go run ./cmd -migrate-only）之后。
DB_SCRIPTS=(
  check-permission-gaps.sh
)

declare -a SELECTED=()
case "$MODE" in
  plain) SELECTED=("${PLAIN_SCRIPTS[@]}") ;;
  db)    SELECTED=("${DB_SCRIPTS[@]}") ;;
  all)   SELECTED=("${PLAIN_SCRIPTS[@]}" "${DB_SCRIPTS[@]}") ;;
esac

declare -a FAILED=()
echo "== 门禁（$MODE）：${#SELECTED[@]} 个脚本 =="

for script in "${SELECTED[@]}"; do
  if [[ ! -f "scripts/$script" ]]; then
    echo "✗ scripts/$script（脚本不存在 —— 改名/删除时记得同步这里）" >&2
    FAILED+=("$script")
    continue
  fi
  echo
  echo "── $script"
  cmd=("bash" "scripts/$script")
  [[ "$script" == *.py ]] && cmd=("python3" "scripts/$script")
  if "${cmd[@]}"; then
    rc=0
  else
    rc=$?
  fi
  if (( rc != 0 )); then
    echo "✗ $script（退出码 $rc）" >&2
    FAILED+=("$script")
  fi
done

echo
if (( ${#FAILED[@]} > 0 )); then
  echo "✗ 门禁未通过（${#FAILED[@]}/${#SELECTED[@]}）：${FAILED[*]}" >&2
  exit 1
fi
echo "✓ 全部门禁通过（$MODE，共 ${#SELECTED[@]} 个脚本）"
