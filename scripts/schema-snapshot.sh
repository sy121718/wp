#!/usr/bin/env bash
# schema-snapshot.sh — 生成当前数据库结构的**可读快照**，并可选对账漂移。
#
# 为什么需要它：public/migrations 里有近 500 个 SQL 文件、3 万行。要理解「现在的 schema
# 长什么样」，唯一可靠的办法是跑完迁移再 dump 一次 —— 把 500 个文件的变更从头拼一遍是
# 不可能不出错的。快照与迁移文件并存，各管一件事：
#
#   迁移文件 = 变更历史（唯一的真相来源，只能追加）
#   快照     = 当前形态（可读、可 diff、可给新人看）
#
# 两种用法：
#   bash scripts/schema-snapshot.sh              # 生成/更新 schema/CURRENT.sql
#   bash scripts/schema-snapshot.sh --check      # 只比对，不一致则非零退出（CI 用）
#
# --check 的意义：迁移合进来了但快照没更新 → 红。否则快照会慢慢烂成一份误导人的文档
# （比没有更糟：新人会以为它是对的）。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# 取值优先级：显式环境变量 → config.yaml 的 database 段 → 兜底默认。
# **config.yaml 在环境变量之后、默认值之前**：它是这个仓库里唯一的连接真源，
# 跳过它去猜默认值正是上面那个 FATAL 的成因。
# yaml_db 从 config.yaml 的 database 段读一个键（与 check-page-get-authz.sh 同一套读法）。
#
# 为什么必须有它：运维脚本曾默认 user=postgres，而 config.yaml.example 是 user=root，
# CI 的 postgres:16 容器用 POSTGRES_USER=root 建库、容器里**没有 postgres 这个 role**。
# 于是脚本在 CI 上必然红（FATAL: role "postgres" does not exist）——
# 这正是"第一次接进 CI 就红"的那类问题，说明它此前从没在 CI 上跑过。
yaml_db() {
  [ -f config.yaml ] || return 0
  awk -v key="$1" '/^database:/{in_db=1;next} /^[a-zA-Z]/{in_db=0} in_db && $1==key":"{print $2;exit}' config.yaml
}

DB_HOST="${DB_HOST:-${GOWP_DB_HOST:-$(yaml_db host)}}"
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-${GOWP_DB_PORT:-$(yaml_db port)}}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-${GOWP_DB_USER:-$(yaml_db user)}}"
DB_NAME="${DB_NAME:-${GOWP_DB_NAME:-$(yaml_db dbname)}}"
DB_NAME="${DB_NAME:-wp}"
export PGPASSWORD="${DB_PASSWORD:-${GOWP_DB_PASSWORD:-$(yaml_db password)}}"

SNAPSHOT_DIR="schema"
SNAPSHOT="$SNAPSHOT_DIR/CURRENT.sql"
CHECK=0
[ "${1:-}" = "--check" ] && CHECK=1

command -v pg_dump >/dev/null 2>&1 || { echo "✗ 需要 pg_dump" >&2; exit 1; }

# ── dump ─────────────────────────────────────────────────────────────────────
# --schema-only：只要结构。数据快照属于备份（scripts/backup.sh），不是这份文档的事。
# 排除项都是「每次跑都会变、但与 schema 语义无关」的东西：
#   · 属主/权限 —— 随部署角色变，进快照会让 diff 全是噪音
#   · pg_dump 的随机 \restrict 标记与生成时间戳 —— 否则每次生成都不同，--check 永远红
dump_schema() {
  pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
    --schema-only --no-owner --no-privileges --no-comments \
    --exclude-table='sys_migration_marker' \
    | sed -E \
        -e '/^-- Dumped (from|by) /d' \
        -e '/^-- Dumped on /d' \
        -e '/^\\restrict /d' \
        -e '/^\\unrestrict /d' \
        -e '/^SET (transaction_timeout|idle_in_transaction_session_timeout) =/d' \
        -e 's/[[:space:]]+$//'
}

if [ "$CHECK" -eq 1 ]; then
  [ -f "$SNAPSHOT" ] || { echo "✗ 快照不存在：$SNAPSHOT（先跑 bash scripts/schema-snapshot.sh）" >&2; exit 1; }
  TMP="$(mktemp)"
  trap 'rm -f "$TMP"' EXIT
  dump_schema > "$TMP"
  # 比对**只跳过文件头**：头部含生成时间与迁移版本，每次都会变。头部之外逐字节比对
  # （表、列、索引、约束、函数、触发器全部在内）—— 「只比关键字行」会让列类型改动漏网。
  body_of() { awk 'f{print} /^-- 当前数据库结构快照/{f=1} f&&/^$/{exit}' "$1" ; }
  # 直接用 sed 剥掉头部更稳：头部固定以「-- PostgreSQL database dump」之前的空行为界。
  strip_header() { sed -n '/^-- PostgreSQL database dump/,$p' "$1"; }
  strip_header "$SNAPSHOT" > /tmp/snap_body.sql
  strip_header "$TMP" > /tmp/curr_body.sql
  if diff -u /tmp/snap_body.sql /tmp/curr_body.sql > /tmp/schema_diff.txt 2>&1; then
    echo "✓ schema 快照与当前迁移结果一致"
    exit 0
  fi
  echo "✗ schema 快照已过期 —— 迁移改了结构但没更新快照。" >&2
  echo "  修：bash scripts/schema-snapshot.sh" >&2
  echo >&2
  head -60 /tmp/schema_diff.txt >&2
  exit 1
fi

mkdir -p "$SNAPSHOT_DIR"
{
  echo "-- 当前数据库结构快照（由 scripts/schema-snapshot.sh 生成，请勿手改）。"
  echo "-- "
  echo "-- 这份文件回答「现在 schema 长什么样」；public/migrations 回答「它是怎么变成这样的」。"
  echo "-- 两者必须并存：把 488 个迁移文件从头读一遍才能知道当前形态，是不可行的。"
  echo "-- "
  echo "-- 更新方式：bash scripts/schema-snapshot.sh"
  echo "-- 漂移检测：bash scripts/schema-snapshot.sh --check（CI 里跑）"
  echo "-- 迁移台账版本：$(psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
        "SELECT coalesce(max(version),'none') FROM sys_migration_marker" 2>/dev/null || echo unknown)"
  echo "-- 生成时间：$(date -Iseconds)"
  echo
  dump_schema
} > "$SNAPSHOT"

TABLES=$(grep -c '^CREATE TABLE' "$SNAPSHOT" || true)
LINES=$(wc -l < "$SNAPSHOT")
echo "✓ 已更新 $SNAPSHOT（$TABLES 张表 / $LINES 行）"
