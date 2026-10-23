#!/usr/bin/env bash
# restore.sh — 从 backup.sh 的备份恢复，并**验证**恢复结果。
#
# 核心约定：没演练过的备份等于没有。所以本脚本有两种模式，且都做对账：
#
#   bash scripts/restore.sh --drill <备份目录>   恢复演练（默认推荐）
#       建一个临时库 → 恢复进去 → 逐表对账行数 → 删掉临时库。
#       整个过程**不碰生产库**，可以随时跑、跑多少次都行。
#
#   bash scripts/restore.sh --to <库名> <备份目录>   恢复到指定库
#       用于灾难恢复：先手工建好目标库，脚本负责恢复 + 对账。
#
#   bash scripts/restore.sh --site <备份目录>       只恢复站点产物与媒体资产
#
# 为什么要有 --drill：备份文件能不能用，只有真正恢复一次才知道（pg_dump 成功不代表
# 能 restore 回去 —— 权限、扩展、版本差异都可能在恢复时才炸）。上线前必须跑一次。
#
# 退出码：0 = 恢复成功且对账一致；非 0 = 失败或对账不一致（不一致时会把差异打出来）。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

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
export PGPASSWORD="${DB_PASSWORD:-${GOWP_DB_PASSWORD:-$(yaml_db password)}}"

say() { printf '\033[1;34m›\033[0m %s\n' "$*"; }
ok() { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

command -v pg_restore >/dev/null 2>&1 || fail "缺少 pg_restore（PostgreSQL 客户端）"
command -v psql >/dev/null 2>&1 || fail "缺少 psql"

MODE=""
TARGET_DB=""
BACKUP=""

while [ $# -gt 0 ]; do
  case "$1" in
    --drill)  MODE=drill;  BACKUP="${2:-}"; shift 2 ;;
    --to)     MODE=to;     TARGET_DB="${2:-}"; BACKUP="${3:-}"; shift 3 ;;
    --site)   MODE=site;   BACKUP="${2:-}"; shift 2 ;;
    -h|--help)
      sed -n '2,26p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) fail "未知参数：$1（用 --help 看用法）" ;;
  esac
done

[ -n "$BACKUP" ] || fail "必须给备份目录（用 --help 看用法）"
[ -d "$BACKUP" ] || fail "备份目录不存在：$BACKUP"

# ── 先校验备份完整性（校验和不对就不要往下走）────────────────────────────────
say "校验备份校验和 ..."
if [ -f "$BACKUP/SHA256SUMS" ]; then
  ( cd "$BACKUP" && sha256sum --quiet -c SHA256SUMS ) || fail "备份校验和不匹配 —— 这份备份已损坏"
  ok "校验和一致"
else
  say "备份里没有 SHA256SUMS（旧格式？），跳过校验"
fi

if [ -f "$BACKUP/manifest.env" ]; then
  say "备份清单："
  sed 's/^/    /' "$BACKUP/manifest.env"
fi

DUMP="$BACKUP/db/$(sed -n 's/^db_name=//p' "$BACKUP/manifest.env" 2>/dev/null || echo wp).dump"
[ -f "$DUMP" ] || DUMP="$(find "$BACKUP/db" -name '*.dump' -print -quit 2>/dev/null)"
[ -n "${DUMP:-}" ] && [ -f "$DUMP" ] || fail "找不到 .dump 文件（备份不完整？）"

# ── 恢复站点资产（可与数据库恢复分开跑）──────────────────────────────────────
restore_site() {
  local tar_file="$1" dest_root="$2" label="$3"
  if [ ! -f "$tar_file" ]; then
    say "跳过 $label（备份里没有）"
    return 0
  fi
  say "恢复 $label → $dest_root ..."
  mkdir -p "$dest_root"
  # 解包到目标根的父目录，保持 tar 内的目录名（public/runtime、public/storage）。
  tar -xzf "$tar_file" -C "$(dirname "$dest_root")" || fail "$label 解包失败"
  ok "$label 已恢复"
}

if [ "$MODE" = "site" ]; then
  restore_site "$BACKUP/site/artifacts.tar.gz" "public/runtime" "站点产物"
  restore_site "$BACKUP/site/storage.tar.gz" "public/storage" "媒体资产"
  # 符号链接是激活状态本身：解包后必须逐个确认指向的目标存在。
  say "核对激活状态（符号链接 → 产物目录）..."
  broken=0
  if [ -d public/runtime/public/active ]; then
    while IFS= read -r -d '' link; do
      if [ ! -e "$link" ]; then
        echo "    悬空链接：$link → $(readlink "$link")"
        broken=$((broken + 1))
      fi
    done < <(find public/runtime/public/active -type l -print0)
  fi
  [ "$broken" -eq 0 ] || fail "$broken 个激活链接指向不存在的产物 —— 站点会 404"
  ok "激活状态完整"
  exit 0
fi

# ── 决定目标库 ───────────────────────────────────────────────────────────────
if [ "$MODE" = "drill" ]; then
  TARGET_DB="wp_restore_drill_$(date +%s)"
  say "演练模式：恢复到临时库 $TARGET_DB（不碰生产库）"
  psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d postgres \
    -c "CREATE DATABASE \"$TARGET_DB\"" >/dev/null || fail "创建临时库失败"
  trap 'psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d postgres -c "DROP DATABASE IF EXISTS \"$TARGET_DB\"" >/dev/null 2>&1 || true' EXIT
elif [ "$MODE" = "to" ]; then
  [ -n "$TARGET_DB" ] || fail "--to 需要库名"
  say "恢复到库 $TARGET_DB ..."
else
  fail "必须指定 --drill 或 --to <库名>（用 --help 看用法）"
fi

# ── 恢复 ─────────────────────────────────────────────────────────────────────
# --no-owner/--no-privileges 与备份侧对称：目标库的角色可能与源不同。
say "pg_restore → $TARGET_DB ..."
pg_restore -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$TARGET_DB" \
  --no-owner --no-privileges --exit-on-error --single-transaction \
  "$DUMP" || fail "pg_restore 失败（--exit-on-error：任何一条语句失败即中止，不留半截库）"
ok "恢复完成"

# ── 对账（这一步才是「恢复成功」的判据）────────────────────────────────────
say "逐表对账行数（备份时 vs 现在）..."
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$TARGET_DB" -tA -F $'\t' \
  -c "ANALYZE; SELECT schemaname||'.'||relname, n_live_tup FROM pg_stat_user_tables ORDER BY 1" \
  > /tmp/restore_row_counts.tsv 2>/dev/null || fail "对账快照失败"

# 只比较**备份时已有**的表：恢复后可能因新迁移多出表，那是正常的。
python3 - "$BACKUP/db/row_counts.tsv" /tmp/restore_row_counts.tsv <<'PY' || exit 1
import sys

def load(path):
    d = {}
    try:
        with open(path, encoding='utf-8') as f:
            for line in f:
                parts = line.rstrip('\n').split('\t')
                if len(parts) == 2:
                    d[parts[0]] = int(parts[1] or 0)
    except FileNotFoundError:
        pass
    return d

before, after = load(sys.argv[1]), load(sys.argv[2])
if not before:
    print("  （备份没有行数快照，跳过对账）")
    sys.exit(0)

missing = [t for t in before if t not in after]
empty = [t for t, n in before.items() if n > 0 and after.get(t) == 0]

if missing:
    print(f"✗ {len(missing)} 张表恢复后不存在：")
    for t in missing[:20]:
        print(f"    {t}")
if empty:
    print(f"✗ {len(empty)} 张表备份时有数据、恢复后为空：")
    for t in empty[:20]:
        print(f"    {t}（备份 {before[t]} 行 → 现在 0 行）")
if missing or empty:
    sys.exit(1)
print(f"✓ {len(before)} 张表逐一对上（有数据的表都没有变空）")
PY

ok "恢复演练通过 —— 这份备份是可用的"
if [ "$MODE" = "drill" ]; then
  echo
  echo "临时库 $TARGET_DB 将在退出时删除。"
  echo "若要保留检查：DROP 掉 trap 前手工跑 psql -d $TARGET_DB"
fi
