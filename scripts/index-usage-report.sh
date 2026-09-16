#!/usr/bin/env bash
# index-usage-report.sh — 索引使用量快照与增量对比（审计 IDX-018 的运维工具）。
#
# 为什么需要它：IDX-018 列的几条候选索引是否冗余，**不能靠读代码判断** ——
# 判据是 pg_stat_user_indexes 的 idx_scan 增量。而单次快照没有区分度：
# 开发库空表上连唯一约束索引都是 0 次扫描，本地跑一遍得到的「全是 0」什么也说明不了
# （迁移 194 的核对注释里记了这次实测）。真正要的是「生产库上间隔一个完整业务周期的
# 两次快照之差」—— 本脚本就是那个差的采集与比较工具。
#
# 用法：
#   bash scripts/index-usage-report.sh --save before     # 业务周期开始前（或变更前）
#   …跑一个完整业务周期…
#   bash scripts/index-usage-report.sh --diff before     # 与 before 对比，列出零增量候选
#   bash scripts/index-usage-report.sh --list            # 列出已有快照
#   bash scripts/index-usage-report.sh                   # 只打印当前快照（不落盘）
#
# 重要告诫（审计原文）：**增量为零不等于可以删**。低频但关键的查询可能在观察期内
# 根本没跑（月末对账、季度导出、故障恢复路径）。零增量只是「进入待复核清单」，
# 真正删之前还要确认：没有唯一/主键约束语义、没有 EXPLAIN 断言测试依赖它、
# 且能有替代索引覆盖同一查询面。
#
# 依赖：psql；config.yaml 的 database 段可读（脚本里不另存口令）。
# 快照落在 public/logs/index-usage/（运维产物，不参与构建、不进版本库）。
set -euo pipefail
cd "$(dirname "$0")/.."

SNAPSHOT_DIR="public/logs/index-usage"

if ! command -v psql >/dev/null 2>&1; then
    echo "缺少 psql，无法采集索引统计。" >&2
    exit 1
fi

# 从 config.yaml 的 database 段取值（与 check-permission-gaps.sh 同一手法）。
yaml_db() {
    awk -v key="$1" '
        /^database:/ { in_db = 1; next }
        /^[a-zA-Z]/ { in_db = 0 }
        in_db && $1 == key":" { print $2; exit }
    ' config.yaml
}

DB_HOST="${DB_HOST:-$(yaml_db host)}"
DB_PORT="${DB_PORT:-$(yaml_db port)}"
DB_USER="${DB_USER:-$(yaml_db user)}"
DB_PASS="${DB_PASSWORD:-$(yaml_db password)}"
DB_NAME="${DB_NAME:-$(yaml_db dbname)}"

# 采集：按 idx_scan 升序（零使用的排最前），带体积与约束类型 ——
# 后两者是判断「能不能删」的必要上下文，只看 idx_scan 会误导。
snapshot() {
    PGPASSWORD="$DB_PASS" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAF'	' -c "
        SELECT
            s.relname,
            s.indexrelname,
            s.idx_scan,
            s.idx_tup_read,
            s.idx_tup_fetch,
            pg_relation_size(s.indexrelid),
            CASE
                WHEN i.indisprimary THEN 'primary'
                WHEN i.indisunique  THEN 'unique'
                ELSE 'plain'
            END
        FROM pg_stat_user_indexes s
        JOIN pg_index i ON i.indexrelid = s.indexrelid
        ORDER BY s.idx_scan ASC, pg_relation_size(s.indexrelid) DESC;"
}

print_snapshot() {
    printf '%-46s %-46s %10s %10s %14s %8s\n' TABLE INDEX IDX_SCAN TUP_READ SIZE_BYTES KIND
    awk -F'	' '{
        printf "%-46s %-46s %10s %10s %14s %8s\n", $1, $2, $3, $4, $6, $7
    }'
}

case "${1:---show}" in
    --save)
        name="${2:-}"
        [ -n "$name" ] || { echo "用法： $0 --save <快照名>" >&2; exit 1; }
        mkdir -p "$SNAPSHOT_DIR"
        out="$SNAPSHOT_DIR/$name.tsv"
        snapshot > "$out"
        echo "✓ 快照已保存：$out（$(wc -l < "$out") 个索引）"
        echo "  提示：一个完整业务周期后再跑 $0 --diff $name"
        ;;
    --diff)
        name="${2:-}"
        [ -n "$name" ] || { echo "用法： $0 --diff <快照名>" >&2; exit 1; }
        before="$SNAPSHOT_DIR/$name.tsv"
        [ -f "$before" ] || { echo "找不到基线快照：$before" >&2; exit 1; }
        now=$(mktemp); trap 'rm -f "$now"' EXIT
        snapshot > "$now"
        echo "→ 基线 $before → 当前"
        awk -F'	' -v OFS='	' '
            NR == FNR { base[$2] = $3; kind[$2] = $7; size[$2] = $6; tbl[$2] = $1; seen[$2] = 1; next }
            { if ($2 in base) {
                  delta[$2] = $3 - base[$2]
              } else {
                  delta[$2] = $3
                  kind[$2] = $7; size[$2] = $6; tbl[$2] = $1
              }
              known[$2] = 1 }
            END {
                printf "%-46s %-44s %9s %10s %8s\n", "TABLE", "INDEX", "DELTA", "SIZE_BYTES", "KIND"
                n = 0
                for (idx in known) {
                    if (delta[idx] + 0 == 0 && kind[idx] == "plain") {
                        printf "%-46s %-44s %9s %10s %8s\n", tbl[idx], idx, delta[idx] + 0, size[idx], kind[idx]
                        n++
                    }
                }
                printf "\n零增量候选（plain 索引、观察期内一次未扫描）：%d 条\n", n
            }' "$before" "$now"
        echo
        echo "注意：增量为零只是进入待复核清单 —— 低频但关键的查询（月末对账、导出、"
        echo "      故障恢复路径）可能在观察期内没跑。删除前确认没有唯一约束语义、"
        echo "      没有 EXPLAIN 断言测试依赖它、且有替代索引覆盖同一查询面。"
        ;;
    --list)
        if [ -d "$SNAPSHOT_DIR" ]; then
            ls -la "$SNAPSHOT_DIR"
        else
            echo "还没有任何快照（先跑 $0 --save <名字>）"
        fi
        ;;
    *)
        echo "→ 当前索引使用量（数据库 ${DB_HOST}:${DB_PORT}/${DB_NAME}）"
        snapshot | print_snapshot
        echo
        echo "提示：单次快照没有区分度（空表上所有索引都是 0 次扫描）。"
        echo "      要判断冗余请用 --save 建立基线，一个业务周期后 --diff 对比。"
        ;;
esac
