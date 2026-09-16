#!/usr/bin/env bash
# 多端硬规则守卫（审计 UI-015）—— CI 与本地共用的清单步骤。
#
# 模式由 SKY_CSS_GUARD 控制（off / warn / error，默认 warn）：
#   warn  —— 打印全量违规清单，退出码 0（本批口径：先亮清单，整改是后续批次）；
#   error —— 有未豁免违规则退出码 1（构建路径上的产物守卫同时也会拦住构建）。
#
# 与 static checks 其它脚本并列执行：纯文本扫描，不需要数据库。
set -euo pipefail
cd "$(dirname "$0")/.."
exec go run ./scripts/multidevice-css-check
