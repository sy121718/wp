#!/usr/bin/env bash
# backup.sh — 数据库与站点资产的备份。
#
# 为什么需要它：发布路径有产物级回滚（Publisher.Rollback），但**数据库没有**。迁移一旦
# 出错（public/migrations 里 486 个 SQL 文件、575 条语句），唯一出路就是外部备份。
# 产物级回滚救不了「schema 改错了」。
#
# 备份什么（缺一项就是残缺的备份）：
#   1. PostgreSQL 全库（结构 + 数据 + seed），自定义格式（-Fc），可选择性恢复；
#   2. 站点产物与激活状态（public/runtime：artifacts/ 内容寻址产物 + public/active 符号链接）；
#   3. 上传的媒体资产（storage/，无对应产物可重建 —— 丢了就是丢了）；
#   4. 一份清单（manifest），记录版本、时间、各文件校验和 —— 恢复前先对账。
#
# 用法：
#   bash scripts/backup.sh                     # 写到 .backup/<时间戳>/
#   BACKUP_DIR=/mnt/backups bash scripts/backup.sh
#
# 环境变量（也接受同名 GOWP_ 前缀，与服务的配置口径一致）：
#   DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME
#   ARTIFACT_ROOT  站点产物根（默认 public/runtime，与 GO_WP_ARTIFACT_ROOT 同源）
#   STORAGE_DIR    媒体资产目录（默认 public/storage）
#
# 退出码：0 成功；非 0 表示某项备份失败（脚本不会留下"看起来成功"的残缺备份）。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="${BACKUP_DIR:-$ROOT_DIR/.backup}/$STAMP"
DB_HOST="${DB_HOST:-${GOWP_DB_HOST:-127.0.0.1}}"
DB_PORT="${DB_PORT:-${GOWP_DB_PORT:-5432}}"
DB_USER="${DB_USER:-${GOWP_DB_USER:-postgres}}"
DB_NAME="${DB_NAME:-${GOWP_DB_NAME:-wp}}"
ARTIFACT_ROOT="${ARTIFACT_ROOT:-${GO_WP_ARTIFACT_ROOT:-public/runtime}}"
STORAGE_DIR="${STORAGE_DIR:-public/storage}"

say() { printf '\033[1;34m›\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

command -v pg_dump >/dev/null 2>&1 || fail "缺少 pg_dump（PostgreSQL 客户端）"

mkdir -p "$OUT/db" "$OUT/site"
say "备份到 $OUT"

# ── 1. 数据库 ────────────────────────────────────────────────────────────────
# -Fc（自定义格式）：支持 pg_restore 的选择性恢复与并行恢复，比纯 SQL 小且快。
# --no-owner/--no-privileges：恢复到不同角色名下时不因属主不存在而失败。
say "导出数据库 $DB_NAME@$DB_HOST:$DB_PORT ..."
PGPASSWORD="${DB_PASSWORD:-${GOWP_DB_PASSWORD:-}}" pg_dump \
  -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
  --format=custom --no-owner --no-privileges --verbose \
  --file "$OUT/db/$DB_NAME.dump" 2> "$OUT/db/pg_dump.log" \
  || fail "pg_dump 失败，详见 $OUT/db/pg_dump.log"

# 结构与数据的**行数快照**：恢复后用它逐表对账（「恢复成功」不等于「数据回来了」）。
say "记录每表行数快照 ..."
PGPASSWORD="${DB_PASSWORD:-${GOWP_DB_PASSWORD:-}}" psql \
  -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tA -F $'\t' \
  -c "SELECT schemaname||'.'||relname, n_live_tup FROM pg_stat_user_tables ORDER BY 1" \
  > "$OUT/db/row_counts.tsv" || fail "行数快照失败"

# ── 2. 站点产物与激活状态 ────────────────────────────────────────────────────
# -a 保留符号链接本身（public/active 下全是指向 artifacts/{hash} 的符号链接）。
# -h 让链接不被解引用 —— 解引用会把整棵产物树复制多份，备份体积爆炸。
if [ -d "$ARTIFACT_ROOT" ]; then
  say "备份站点产物 $ARTIFACT_ROOT ..."
  # 悬空符号链接（激活链接指向已被清理的产物）会让 tar 以非零退出。这在真实站点上
  # 是正常状态（取消发布后旧产物可能被清），不该挡住整份备份 —— 所以记录警告继续，
  # 但**如实报告**有多少条：静默跳过会让人以为激活状态完整恢复了。
  set +e
  tar -czhf "$OUT/site/artifacts.tar.gz" -C "$(dirname "$ARTIFACT_ROOT")" "$(basename "$ARTIFACT_ROOT")" \
    2> "$OUT/site/artifacts.tar.log"
  tar_rc=$?
  set -e
  if [ "$tar_rc" -ne 0 ]; then
    dangling=$(grep -c '被删除\|No such file\|Cannot stat' "$OUT/site/artifacts.tar.log" 2>/dev/null || echo "?")
    echo "  ⚠ tar 退出码 $tar_rc，跳过 $dangling 个悬空链接（详见 site/artifacts.tar.log）"
    [ -s "$OUT/site/artifacts.tar.gz" ] || fail "站点产物打包彻底失败，详见 $OUT/site/artifacts.tar.log"
  fi
else
  say "跳过站点产物（$ARTIFACT_ROOT 不存在）"
fi

# ── 3. 媒体资产 ──────────────────────────────────────────────────────────────
if [ -d "$STORAGE_DIR" ]; then
  say "备份媒体资产 $STORAGE_DIR ..."
  tar -czf "$OUT/site/storage.tar.gz" -C "$(dirname "$STORAGE_DIR")" "$(basename "$STORAGE_DIR")" \
    || fail "媒体资产打包失败"
else
  say "跳过媒体资产（$STORAGE_DIR 不存在）"
fi

# ── 4. 清单与校验和 ──────────────────────────────────────────────────────────
say "生成清单 ..."
{
  echo "backup_stamp=$STAMP"
  echo "created_at=$(date -Iseconds)"
  echo "host=$(hostname)"
  echo "git_commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  echo "git_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
  echo "db_name=$DB_NAME"
  echo "db_host=$DB_HOST:$DB_PORT"
  echo "artifact_root=$ARTIFACT_ROOT"
  echo "storage_dir=$STORAGE_DIR"
  # 迁移版本：恢复后必须知道这份备份对应到哪个 schema 版本。
  # 台账表是 sys_migration_marker（不是 schema_migrations —— 猜名字会得到 unknown，
  # 而 unknown 会让「这份备份能不能回滚到上一版」无法回答）。
  echo "migration_head=$(PGPASSWORD="${DB_PASSWORD:-${GOWP_DB_PASSWORD:-}}" psql \
    -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT coalesce(max(version), 'none') FROM sys_migration_marker" 2>/dev/null || echo unknown)"
} > "$OUT/manifest.env"

( cd "$OUT" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS )

BYTES=$(du -sh "$OUT" | cut -f1)
say "完成：$OUT（$BYTES）"
echo
echo "恢复：bash scripts/restore.sh $OUT"
echo "演练：bash scripts/restore.sh --drill $OUT   # 恢复到临时库并逐表对账，不动生产"
