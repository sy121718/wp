#!/usr/bin/env bash
# 基线产物静态门禁（无需数据库）：
#   1. schema/baseline/manifest.txt 的两段各自是当前注册表的前缀 ——
#      段外新增版本按设计不进基线，不算过期；
#   2. init.sql 登记的摘要与版本与产物内容自洽 —— 手改 SQL 不重新生成会被抓住。
# 「基线＋增量 == 历史全量」的终态等价要真库，归 scripts/baseline-verify.sh，
# 不进无依赖组（它建两个临时库，约两分钟）。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
exec bash scripts/baseline-generate.sh --check --static
