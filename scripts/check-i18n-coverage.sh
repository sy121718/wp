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
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TPL_DIR="$ROOT/internal/templates/admin"
BASELINE_FILE="$ROOT/scripts/i18n-coverage-baseline.txt"
LIST_FILE="${I18N_COVERAGE_LIST:-/tmp/i18n-coverage-lines.txt}"

if [ ! -d "$TPL_DIR" ]; then
  echo "找不到后台模板目录：$TPL_DIR" >&2
  exit 2
fi

# 1) 剔除注释后逐行判断：含中日韩字符且不含 t() 调用 → 记为未 key 化。
#    -p 保留行号（便于直接跳到问题行）。
python3 - "$TPL_DIR" "$LIST_FILE" <<'PYEOF'
import glob, os, re, sys
tpl_dir, out_path = sys.argv[1], sys.argv[2]
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
t_call = re.compile(r'(?:\.\["t"\]|\btr)\(')
rows = []
# **必须递归**：admin/ 已按后端模块分子目录（admin/<模块>/x.html，根下只剩 layout / login /
# dashboard 三个壳页面）。写成 `admin/*.html` 会静默缩水成「只查 3 个文件」—— 门禁照样绿，
# 但它已经不再守任何东西（2026-09-23 实测：分目录后基线仍是 0，而真实水位不是）。
for path in sorted(glob.glob(os.path.join(tpl_dir, '**', '*.html'), recursive=True)):
    src = open(path, encoding='utf-8').read()
    src = jet_comment.sub(lambda m: re.sub(r'[^\n]', ' ', m.group(0)), src)
    src = html_comment.sub(lambda m: re.sub(r'[^\n]', ' ', m.group(0)), src)
    src = script_block.sub(lambda m: re.sub(r'[^\n]', ' ', m.group(0)), src)
    for i, line in enumerate(src.split('\n'), 1):
        if not cjk.search(line):
            continue
        if t_call.search(line):
            continue
        if lang_self_name.search(line):
            continue
        rows.append('%s:%d:%s' % (os.path.relpath(path, os.path.dirname(tpl_dir.rstrip('/'))), i, line.strip()[:120]))
with open(out_path, 'w', encoding='utf-8') as f:
    f.write('\n'.join(rows) + ('\n' if rows else ''))
print(len(rows))
PYEOF

CURRENT=$(python3 -c "import sys; print(sum(1 for _ in open(sys.argv[1], encoding='utf-8')))" "$LIST_FILE")
echo "后台模板未 key 化中文行数：$CURRENT"

if [ ! -f "$BASELINE_FILE" ]; then
  echo "$CURRENT" > "$BASELINE_FILE"
  echo "已写入基线：$BASELINE_FILE"
  exit 0
fi

BASE=$(tr -d '[:space:]' < "$BASELINE_FILE")
if [ "$CURRENT" -gt "$BASE" ]; then
  echo "" >&2
  echo "✗ 新增了硬编码文案：$BASE → $CURRENT" >&2
  echo "  请改用 {{ .[\"t\"](\"admin.<模块>.<语义>\", \"中文兜底\") }}，并同批 seed 词条。" >&2
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
