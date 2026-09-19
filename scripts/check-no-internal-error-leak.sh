#!/usr/bin/env bash
# check-no-internal-error-leak.sh — 禁止后台 handler 直出内部错误（审计 CQ-009）。
#
# 为什么需要一条脚本而不是靠人记：这个模式写起来太顺手了 ——
# `c.String(500, err.Error())` 一行搞定，评审时也「看起来没问题」。
# 但它把表名、SQL 片段、路径直接铺在页面上，而后台不是可信边界。
#
# 判据只有一条：后台 handler 里出现「输出调用的实参里带 err.Error()」。
# 命中即失败，并打印出来让人改用 shell.PageError(c, scene, err)（页面）、
# 模块自己的归口助手（JSON，如 admin 的 internal/module/admin/inbound/http/admin_err.go）
# 或 pkg/response.ErrorAuto —— 详情记日志，对外只给模块 enums 的文案。
#
# **三种形态一起判**（2026-09 收口，实测每一层都是漏过的）：
#   ① 响应写入：任何 `*.ErrorWithMessage(` / `c.String(` 的实参里带 .Error()；
#   ② 重定向 query：`?err="+url.QueryEscape(err.Error())` —— 页面会把它渲染出来，等同直出；
#   ③ 模板数据：`data.Errors = []string{"…：" + err.Error()}`。
# 形态 ① 是「接收方字面量」漏过的教训（r.ErrorWithMessage 这种别名整批漏过）；
# 形态 ②③ 是「判据只认响应写入」漏过的教训 —— 三种都堵上，判据才算按**形状**而不是按字面量。
#
# 豁免按 **文件 + 形态** 记账（见 EXEMPT）：只放行某文件的某一种形态，
# 该文件里将来出现别的形态照样会被抓住（整文件豁免会把它藏起来）。
#
# 扫描范围：各模块的 inbound/http（后台页面与后台 JSON 都在这里）。
#
# 2026-09 修正①：旧版把目标写死成 internal/module/dashboard/inbound/http —— dashboard 模块
# 早已并入 workbench，该目录不存在，脚本从那时起就一直以 exit 2 失败（等于这条门禁没在跑）。
# 改为动态枚举；一个都没枚举到也当失败（目录结构变化时要有人知道）。
#
# 2026-09 修正②（本批）：旧判据只认 `c.String(` 与 `response.ErrorWithMessage(` 两种**接收方字面量**，
# admin 的 74 处 `r.ErrorWithMessage(..., err.Error())`（response 的别名）因此整批漏过 ——
# 门禁一直是绿的，泄漏一直在。现在判据是「任何 `*.ErrorWithMessage(` / `c.String(` 的行里
# 出现 `.Error()`」，接收方叫什么名字都拦得住，嵌套参数（如 navigationErrorStatus(err)）
# 也不再因为正则跨不过括号而漏。
# 这是纯文本启发式：它宁可多报（同一行里恰好有别的 .Error() 调用）也不放过 ——
# 多报的一行看一眼就能确认，漏报的那一行要等泄漏发生。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

mapfile -t TARGETS < <(find "$ROOT/internal/module" -type d -path '*/inbound/http' | sort)
if [ "${#TARGETS[@]}" -eq 0 ]; then
  echo "没有枚举到任何后台 handler 目录（internal/module/*/inbound/http）：目录结构变了？" >&2
  exit 2
fi

# 形态 ① 的接收方不限：c.String( 或任意 x.ErrorWithMessage( / x.ParamError(。
#
# ParamError 是 2026-09 第三批补进来的（读侧回显批的交接说明实测）：它和 ErrorWithMessage 是
# pkg/response 的同一族出口，但名字不在判据里 —— 于是 `response.ParamError(c, err.Error())`
# 这类调用一直在盲区里（block 的四个 JSON handler 就是）。判据是**形状**不是字面量：
# 同一族的出口漏一个，就等于那一族都没管住。
RESPONSE_CALL_RE='([A-Za-z_][A-Za-z0-9_]*\.(ErrorWithMessage|ParamError)|c\.String)\('
# 形态 ②：重定向 / 查询串 / 拼 URL 的地方出现 .Error()（页面会把 err= 渲染出来）。
SHAPE2_RE='(\?err=|QueryEscape\(|Redirect\(|RedirectWith)'
# 形态 ③：模板数据的错误列表里出现 .Error()。
SHAPE3_RE='(Errors|Errors:)'

# —— 豁免清单（仓库相对路径 → 理由）——
#
# 只允许「点位不在当前批次授权范围内、且已经写在交接说明里」的既有泄漏临时挂在这里。
# 两条纪律：
#   · 例外必须写明理由与移交对象，不允许「因为难改」；
#   · 条目仍在命中，否则视为过期（泄漏被修好了 / 那几行被删了）—— 只增不减的豁免清单
#     与没有门禁等价，脚本会直接失败要求删掉它。
# 当前为空：**最后 3 条已随本批删除**。admin / navigation / inventory 货源页的批量超限此前
# 直传 shell.BulkIDs 的 err.Error()，靠「已知受控 + 值域里装不下驱动原文」的注释与豁免放行；
# 现在 shell.BulkIDs 返回带 sentinel 的类型（shell.ErrBulkIDsTooMany / *shell.BulkIDsError，
# 值域只有 Count/Max 两个整数），三个页面统一走 shell.BulkIDsFacingText —— 受控性成了类型事实，
# 豁免不再需要（清单只减不增，这也是它能收敛的原因）。
# 更早收敛的：navigation 的 5 处（Create/Update/Delete/List/Detail）与 shell.BulkIDs 的原豁免批，
# 消息统一走 internal/module/navigation/inbound/http/navigation_err.go 的归口助手。
# 回归用例：public/test/{admin,navigation,inventory,order,user,mail}/** 的批量超限路径用例，
# 以及 internal/web/shell/bulk_test.go 的类型与受控文案守卫。
declare -A EXEMPT=()

# 候选 = inbound/http 下所有 .Error() 行（logger 的 .Error(err, msg) 带参数，不会被 \\.Error\\(\\) 匹配到）。
CANDIDATES=$(grep -rn --include='*.go' -F '.Error()' "${TARGETS[@]}" \
  | grep -v '_test.go' \
  | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' || true)

BLOCKING=""
declare -A HIT_EXEMPT=()
if [ -n "$CANDIDATES" ]; then
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    rel="${line#"$ROOT"/}"
    file="${rel%%:*}"
    shapes=""
    if grep -qE "$RESPONSE_CALL_RE" <<< "$line"; then shapes+="1"; fi
    if grep -qE "$SHAPE2_RE" <<< "$line"; then shapes+="2"; fi
    if grep -qE "$SHAPE3_RE" <<< "$line"; then shapes+="3"; fi
    # 三种形态都不匹配 ⇒ 不是「送进响应」的写入（比较 / 包装 / 日志参数），交给评审人看，不在这里报。
    [ -z "$shapes" ] && continue
    ok=1
    for s in $(echo "$shapes" | fold -w1); do
      if [ -n "${EXEMPT[$file|$s]:-}" ]; then
        HIT_EXEMPT["$file|$s"]=1
      else
        ok=0
      fi
    done
    [ "$ok" -eq 1 ] && continue
    BLOCKING+="$line  [形态 $shapes]"$'\n'
  done <<< "$CANDIDATES"
fi

# 过期豁免：登记了却不再命中，说明该修好了或行没了，逼着清理清单。
for key in "${!EXEMPT[@]}"; do
  if [ -z "${HIT_EXEMPT[$key]:-}" ]; then
    echo "✗ 豁免清单里的 $key 已经不再命中（修好了 / 行被删了）—— 请把这条从 EXEMPT 删掉。" >&2
    exit 1
  fi
done

if [ -n "$BLOCKING" ]; then
  echo "✗ 后台 handler 直出了内部错误（会泄露表名 / SQL / 路径）：" >&2
  printf '%b' "$BLOCKING" >&2
  echo "" >&2
  echo "  改用 internal/web/shell 的 shell.PageError(c, scene, err)（页面）" >&2
  echo "  或模块级归口助手 / pkg/response.ErrorAuto（JSON）：详情记日志，对外只给 enums 文案。" >&2
  exit 1
fi

if [ "${#EXEMPT[@]}" -gt 0 ]; then
  echo "✓ 未发现新的直出内部错误（已扫描 ${#TARGETS[@]} 个 inbound/http 目录；${#EXEMPT[@]} 个已登记的豁免仍待接手）"
else
  echo "✓ 未发现直出内部错误的调用点（已扫描 ${#TARGETS[@]} 个 inbound/http 目录）"
fi
