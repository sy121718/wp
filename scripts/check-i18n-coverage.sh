#!/usr/bin/env bash
# check-i18n-coverage.sh — 后台模板硬编码文案的覆盖率门禁（审计 I18N-001）。
#
# 为什么是「固化基线 + 禁止新增」而不是「必须为零」：
#
#   52 个后台模板、数千条中文文案。一次性全量 key 化的风险（漏配 seed、译文缺失、
#   渲染中断）远大于分批推进；而一个要求「立刻归零」的门禁只有两种结局 ——
#   被绕过，或者被关掉。所以这里把当前水位固化成基线：
#
#   · 新增硬编码 → 退出码 1，CI 失败；
#   · 抽取文案（改成 {{ .["t"]("key", "中文兜底") }}）→ 计数下降，脚本提示下调基线。
#
# 计数口径：**行**。一行里出现未被 t() 包住的中日韩字符即记一行。
# 注释（Jet 的 {* *} 与 HTML 的 <!-- -->）整段剔除 —— 注释不是给访客看的。
# t() 调用内的中文是**兜底文案**（key 命中失败时用），不算未 key 化。
#
# **扫描范围**（2026-09 扩展）：internal/templates/**admin**/ 与 **fragments**/ 下的 .html，
# 两个目录**合并**计一个数字。为什么合并而不是各设一条基线：两者是同一批给人看的文案
# （片段是 HTMX 局部刷新，admin 是整页），分设两条会让「这边抽掉 3 行、那边新增 3 行」
# 在两条线上互相掩盖；而分别下调基线的动作又是同一个流程，没有需要区分的信息。
# 只收 .html：fragments/*.jet 的渲染 data 是 struct，取词走 struct 字段而不是 .["t"]，
# 套用本脚本的行判据会把它误算成硬编码（口径见 internal/templates/CLAUDE.md）。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TPL_ROOT="$ROOT/internal/templates"
BASELINE_FILE="$ROOT/scripts/i18n-coverage-baseline.txt"
LIST_FILE="${I18N_COVERAGE_LIST:-/tmp/i18n-coverage-lines.txt}"

for d in "$TPL_ROOT/admin" "$TPL_ROOT/fragments"; do
  if [ ! -d "$d" ]; then
    echo "找不到模板目录：$d" >&2
    exit 2
  fi
done

# 1) 剔除注释后逐行判断：含中日韩字符且不含 t() 调用 → 记为未 key 化。
#    -p 保留行号（便于直接跳到问题行）。
python3 - "$TPL_ROOT" "$LIST_FILE" <<'PYEOF'
import glob, os, re, sys
tpl_root, out_path = sys.argv[1], sys.argv[2]
cjk = re.compile('[一-鿿぀-ヿ]')
jet_comment = re.compile(r'\{\*.*?\*\}', re.S)
html_comment = re.compile(r'<!--.*?-->', re.S)
# <script> 整段剔除：JS 里的中文分两类，都不是「模板文案未 key 化」——
#   · JS 注释（脚本原本只剥 Jet / HTML 注释，剥不到 script 里的 // 与 /* */）；
#   · 兜底文案（msgOf(el, 'msgNetwork', '网络异常')：真文案由服务端按请求语言
#     渲染进 data-msg-* 属性，字符串只是属性缺失时的原文兜底，与 t(key, 中文兜底) 同性质）。
# 不剥会让门禁数字虚高，而虚高的数字会让人不再相信这个门禁。
script_block = re.compile(r'<script\b.*?</script>', re.S)
# 语言自称（简体中文 / 繁體中文 / 日本語 / English / 한국어）在语言下拉里**刻意不翻译**：
# 语言名按自称显示，否则用户用看不懂的语言看到自己的语言名。整行豁免。
lang_self_name = re.compile('简体中文|繁體中文|日本語|한국어|English')
# 取词调用的两种写法都要认：
#   .["t"]("key", "兜底")            —— 直接调用
#   {{tr := .["t"]}} 然后 tr("key","兜底") —— range 内取词的唯一写法
#     （Jet 不支持在 range 内直接写 := 赋值，所以模板会先取变量再调）
# 只认第一种会让已经 key 化的行继续被计成硬编码 —— 门禁数字虚高，
# 而虚高的数字会让人低估覆盖率，进而怀疑整套机制没生效。
# 取词调用的判据按**形状**给，不按变量名列举：
#   `("点分 key", "兜底文案"` —— 点分 key 后面紧跟逗号与第二个字符串参数，是取词调用的特征。
# 为什么不用变量名白名单：局部变量名是任意的 —— `product_attribute_group_form.html` 用的是
# `{{gfTr := .["t"]}}` 之后 `gfTr("admin.x", "中文")`，白名单认不出 gfTr，那 4 行就被误算成
# 「硬编码」（实测踩过）；而每加一个新变量名都要回来改正则，迟早再漏一次。
t_call = re.compile(r'\(\s*"[a-z][a-zA-Z0-9_.]*\.[a-zA-Z0-9_.]+"\s*,\s*"')
# 兜底形态：不带兜底文案的取词调用（t("key") / .["t"]("key")）。
t_call_bare = re.compile(r'(?:\.\["t"\]|\btr)\(')
rows = []
# **admin/ 必须递归**：admin/ 已按后端模块分子目录（admin/<模块>/x.html，根下只剩 layout / login /
# dashboard 三个壳页面）。写成 `admin/*.html` 会静默缩水成「只查 3 个文件」—— 门禁照样绿，
# 但它已经不再守任何东西（2026-09-23 实测：分目录后基线仍是 0，而真实水位不是）。
# **fragments/ 不递归**：片段是平铺的（没有子目录），写成 `**/*.html` 反而会把将来
# 误放进子目录的东西悄悄纳入统计。两种形态都写明，避免后人「顺手统一」改坏其一。
patterns = [
    os.path.join(tpl_root, 'admin', '**', '*.html'),
    os.path.join(tpl_root, 'fragments', '*.html'),
]
paths = sorted(set(p for pat in patterns for p in glob.glob(pat, recursive=True)))
for path in paths:
    src = open(path, encoding='utf-8').read()
    src = jet_comment.sub(lambda m: re.sub(r'[^\n]', ' ', m.group(0)), src)
    src = html_comment.sub(lambda m: re.sub(r'[^\n]', ' ', m.group(0)), src)
    src = script_block.sub(lambda m: re.sub(r'[^\n]', ' ', m.group(0)), src)
    for i, line in enumerate(src.split('\n'), 1):
        if not cjk.search(line):
            continue
        if t_call.search(line) or t_call_bare.search(line):
            continue
        if lang_self_name.search(line):
            continue
        rows.append('%s:%d:%s' % (os.path.relpath(path, tpl_root), i, line.strip()[:120]))
with open(out_path, 'w', encoding='utf-8') as f:
    f.write('\n'.join(rows) + ('\n' if rows else ''))
print(len(rows))
PYEOF

CURRENT=$(python3 -c "import sys; print(sum(1 for _ in open(sys.argv[1], encoding='utf-8')))" "$LIST_FILE")
echo "模板未 key 化中文行数（admin + fragments 合并）：$CURRENT"

if [ ! -f "$BASELINE_FILE" ]; then
  echo "$CURRENT" > "$BASELINE_FILE"
  echo "已写入基线：$BASELINE_FILE"
  exit 0
fi

BASE=$(tr -d '[:space:]' < "$BASELINE_FILE")
if [ "$CURRENT" -gt "$BASE" ]; then
  echo "" >&2
  echo "✗ 新增了硬编码文案：$BASE → $CURRENT" >&2
  echo "  后台页面改用 {{ .[\"t\"](\"admin.<模块>.<语义>\", \"中文兜底\") }}；" >&2
  echo "  HTMX 片段改用 {{tr := .[\"t\"]}} 后 tr(\"workbench.<面>.<语义>\", \"中文兜底\")，" >&2
  echo "  并**在渲染入口注入 t**（片段不经 shell.Prepare；漏注入不是报错而是整片空文案）。" >&2
  echo "  两种形态都要同批 seed 中英词条。" >&2
  echo "  新出现的行（前 20 条，完整清单见 $LIST_FILE）：" >&2
  tail -20 "$LIST_FILE" >&2
  exit 1
fi
if [ "$CURRENT" -lt "$BASE" ]; then
  echo "✓ 抽取了 $((BASE - CURRENT)) 行，请把基线下调为 $CURRENT："
  echo "    echo $CURRENT > \"$BASELINE_FILE\""
  exit 0
fi
echo "✓ 与基线一致（$BASE）"
