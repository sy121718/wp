#!/usr/bin/env bash
# 写表单的字段名对账：模板里的 name="X" 必须出现在目标 DTO 的字段表里。
#
# 为什么需要这道门禁：字段名写错的表现是**静默丢弃** —— 模板写 name="sort"、
# DTO 叫 sortOrder，两边都不报错，服务端收到一个它不认识的键（严格白名单下整条 400，
# 宽松绑定下**静默忽略**），用户看到「保存成功但没改」。
#
# 数据来源：`cmd/write-contracts` 生成物（internal/templates/static/js/ui/write-contracts.json）。
# 它按真实处理器与显式 binder 固定「METHOD+PATH → 输入协议 → DTO 字段表」。
# form/multipart 按 form tag，JSON 按 json tag；不折叠点号/下标、不保留端点豁免。
#
# 判据：每个提交入口单独核对原生 name 与必填项，包含外部 form 归属与 template 边界。
# 动态端点、未知 binder、额外参数选择与 JSON 对象提供器明确报未检查，不冒充已覆盖。
# 退出码 0 = 本次字段静态对账通过；1 = 错误或未检查。可传模板相对路径前缀限定范围。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

CONTRACTS="internal/templates/static/js/ui/write-contracts.json"
if [[ ! -f "$CONTRACTS" ]]; then
  printf '找不到写端点契约文件 %s —— 先跑 go run ./cmd/write-contracts\n' "$CONTRACTS" >&2
  exit 1
fi

# 默认扫三个模板属主（应用 / 公共 UI / 工具）；传模块前缀时是**定向过滤**，
# 扫描根不变，只把匹配该前缀的模板纳入对账。
if [[ -n "${1:-}" ]]; then
  python3 scripts/write-fields-scan.py "$CONTRACTS" --module "$1"
else
  python3 scripts/write-fields-scan.py "$CONTRACTS"
fi
