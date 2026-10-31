#!/usr/bin/env bash
# 写表单的属性白名单（write-base.js 的契约）。
#
# 为什么需要这道门禁：写表单的契约是**一组合法写法**，而属性名写错的表现是「静默失效」——
# 基座不认识这个属性，于是那件事不会发生（不报错、不 500、日志干净）。本项目踩过的例子：
#
#   · `data-write-number` 是旧基座为 fetch+JSON 路径补的类型声明，随那条路径一起删了。
#     新基座上再写它不会报错，只是**什么都不做** —— 而写它的人以为自己声明了类型。
#   · `hx-swap="none"` 曾被逐个表单声明；现在由基座内置。留在模板里不报错，
#     但它暗示「这个表单靠 htmx 提交」，与事实相反。
#
# 判据（任一命中即失败）：
#   0. 两阶段，形态在前、语义在后（顺序即判据）：
#      · 形态阶段：对本目录**每个** form/button 无条件执行，不看它是不是写声明：
#        E4（未登记的裸 data-write-*，含拼错的裸 flag）与 E5a（已登记行为开关带值）。
#        写基座按 hasAttr 读存在性，这些属性写在孤立的 <button> 上同样「静默失效」，
#        所以不能先过作用域过滤 —— 过滤在前就等于把它们 continue 掉。
#      · 语义阶段：作用域＝form/button 上带值 data-write，或该标签自报 data-write-url。
#        只有这一阶段判 E1/E2/E3a/E3b/E5b，因为「已删属性名」「htmx 冲突」「mode 枚举」
#        都是相对写声明的语义：正常非写 form/button 没有这些属性，不进就不误报。
#   1. 写表单（有 data-write-url）上只允许白名单属性；出现已删属性即违规。
#   2. 写表单上禁止 hx-post / hx-swap / hx-target / hx-include —— 基座接 submit，
#      htmx 也接 submit，同时挂会一次点击发两个请求（写动作＝建两条记录）。
#   3. data-write 的值必须在枚举里；data-write-url 必须以 /api/ 开头。
#   4. data-write-* 裸属性必须是登记过的（整形白名单或行为开关）—— 拼错一个字母的表现与
#      「已删属性」一样：静默失效。form 与 button 同判，**不做 DOM 推断**（不猜哪个按钮归哪个表单）。
#   5. 行为开关（data-write-items / -latest）必须写成**裸属性**（带值显式报错，不剥值放过）
#      —— form 与 button 同判。E5b（mode 白名单）只对带 data-write 的 form 判：按钮的声明
#      继承/覆盖表单的 mode，孤立一行判不出真值，是准确收窄不是放宽。
#   6. 标签提取在**引号外**的 '>' 处结束，属性允许等号两侧空白与单/双引号。
#
# 标签提取与属性解析都先走同一份 Python 解析器（见 EXTRACT_PY）：
#   · 标签在**引号外**的 '>' 处结束 —— 属性值里写 "a > b" 不会截断标签；
#   · 属性允许等号两侧空白、单/双引号，标签内部的空白（含换行）归一成单个空格，
#     所以每行恰好「kind TAB tag」两段，无需 read -d 之类的分隔符 hack。
#   解析器只有这一份，测试（scripts/tests/test_write_attributes_scan.py）直接驱动本脚本跑夹具。
#
# 退出码 0 = 通过；1 = 有违规。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

TPL_DIR="internal/templates/admin"

violations=0
fail() { printf '✗ %s\n' "$1" >&2; violations=$((violations + 1)); }

# 已删除的属性（历史名 → 现在的写法）。出现在写表单上就是没迁完或写错了。
# 判据是**精确属性名**（data-write-flash 不牵连 data-write-flash-target）。
# shellcheck disable=SC2016
declare -A REMOVED=(
  [data-write-api]='data-write-url'
  [data-write-mode]='data-write'
  [data-write-number]='（已删：formdata 的 "3" 服务端直接绑进 int）'
  [data-write-float]='（已删：同上）'
  [data-write-bool]='（已删：同上）'
  [data-write-array]='（已删：a&a=b 直接绑成 []string）'
  [data-write-skip-empty]='（已删：基座永远删空值）'
  [data-write-flash]='data-write-reload'
  [data-write-flash-target]='（已删：结论经 sessionStorage 跨刷新）'
  [data-close-on-ok]='data-write-close'
  [data-reload-on-ok]='data-write-reload'
  [data-write-html]='data-write-json-wrap'
)

# 仍在用的整形属性（基座认识；不属于类型声明）。
# data-write-host 保留：一批隐藏表单（单条删除 / 行内改状态）在列表页上各占一行，
# 它们的结论区是**共用**的 —— 模板把那个区放在 <div data-write-host> 里，
# 基座的 statusOf 先查表单内、再查 closest('[data-write-host]')。
# data-write-ids-scope 保留：ids 可能在**另一个容器**里（抽屉内容被克隆到 document 级，
# 勾选框留在列表的批量表单里），必须显式指回去。
#
# 失败/成功后的行为属性（基座 attr()/hasAttr() 直读，与整形无关）：
#   data-write-nav      成功后跳转目标，含 {id} 占位（write-base.js:661-666 消费）。
#                       裸写与带值**都合法**：裸写即「照当前地址重载当前页」，
#                       带值是「跳这条路径」。空白量与空串一律按裸处理（基座 attr 归一）。
#   data-write-confirm  提交前确认文案（attr 读取）。
#   data-write-close    成功后关闭的容器选择器（attr 读取）。
#   data-write-extra    逗号分隔的额外字段名（attr 读取）。
#   data-write-outcome  仅 bulk：receipt / review / update / add（attr 读取）。
# 登记进 ALLOWED_SHAPING 而不是 BEHAVIOR_FLAGS：它们允许带值，BEHAVIOR_FLAGS 那条
# 「带值即 E5a」只会误伤。
declare -A ALLOWED_SHAPING=(
  [data-write-nav]='成功后跳转目标（{id} 取响应 data.id）；裸写 = 重载当前页'
  [data-write-confirm]='提交前确认文案'
  [data-write-close]='成功后关闭的容器选择器'
  [data-write-extra]='额外字段名（逗号分隔）'
  [data-write-outcome]='bulk 结论档：receipt / review / update / add'
  [data-write-json]='行模板 → JSON（数组或单对象）'
  [data-write-json-single]='声明哪些 name 发单对象'
  [data-write-json-wrap]='裸 HTML → {html: …}'
  [data-write-lines]='多行文本 → 字符串数组'
  [data-write-keep-empty]='空串有语义的字段'
  [data-write-ids-scope]='ids 在另一个容器里'
  [data-write-host]='共用结论区的宿主容器'
  [data-write-builder]='存量兼容：页面级组装器'
  [data-write-rows]='存量兼容：行容器 → 数组'
  [data-write-object]='存量兼容：容器 → 单对象'
  # 裸属性开关（基座按 hasAttr 读存在性）：不是整形，但同样只准白名单。
  [data-write-reload]='失败/成功后是否需要刷新页面'
  [data-write-status]='结论区宿主（写作 data-write-status 或 data-write-status="id"）'

)

# 写表单上不该出现的 htmx 属性。
declare -A NO_HTMX=(
  [hx-post]='基座发 fetch；同挂会双提交'
  [hx-put]='同上'
  [hx-patch]='同上'
  [hx-delete]='同上'
  [hx-swap]='基座内置 hx-swap="none"'
  [hx-target]='写动作没有要换进 DOM 的内容'
  [hx-include]='基座从 closest("[data-write]") 收字段'
)

# 行为开关（裸属性；基座按 hasAttr 读存在性，不读值）。
#
# 为什么要单独登记：这类属性同样会**静默失效** —— 拼错时基座当它不存在，提交照发，
# 只是失败时不出错误明细 / 迟到响应盖掉新响应。而门禁此前只认「已删属性」与
# 「整形属性」两本账，拼错的裸属性两本都不沾，于是绿灯。
#
# 登记口径不是放宽：每个属性显式声明「允许出现在哪些 mode 上」和「基座里消费它的
# 函数名」。作用域与基座一致：基座用 hasAttr(form, submitter, name) 合并表单与提交按钮，
# 所以这两个属性写在**任一处**都要判（不能要求按钮自己再带 data-write-url）。
# 文件末的反向判据核对的是**函数体里对这个属性字面量的消耗**（hasAttr 等调用），
# 函数名存在但没读该属性同样变红。
#
#   data-write-items  —— 失败时消费响应的 data.items 列明细。
#                        bulk 档本来就消费（itemsWanted 的 mode === 'bulk' 分支），
#                        但写在 bulk 上**不算错**：显式声明与基座语义一致，只是冗余；
#                        禁掉冗余声明是门禁越权，不是契约。
#   data-write-latest —— 同一表单可能并发提交时开：旧响应不得盖新响应。
#                        任何 mode 都合法（bulk 连点同样有此需求）。
#
# 值 = 基座里必须真实消费该属性字面量的函数名（空着 = 只要求属性名在基座里出现）。
declare -A BEHAVIOR_FLAGS=(
  [data-write-items]='itemsWanted'
  [data-write-latest]='latestMode'
)
declare -A BEHAVIOR_MODES=(
  [data-write-items]='create update delete bulk custom single'
  [data-write-latest]='create update delete bulk custom single'
)

# ── 单一解析器 ───────────────────────────────────────────────────────────────
# 两种入口，同一份规则（测试脚本直接给本文件喂夹具，不再抄一份判据）：
#   extract <file>   读模板 → stdout 每行「kind TAB tag」（kind ∈ form|button）
#   judge            读 stdin 的「kind TAB tag」→ stdout 每行违规，末行「BAD TAB n」
#
# 为什么不再用 [^>]* : '>' 出现在属性值里（data-write-confirm="a > b"）
# 会把标签从中间截断，后面的属性全部丢失 → 漏判。这里只在**引号外**认 '>'。
# 也不再靠 read -d / tr 拼行：标签内部空白（含换行）在这里就归一成单空格，
# 每行固定两段，「kind TAB tag」直接 while IFS TAB read 即可。
read -r -d '' EXTRACT_PY <<'PY'
import re, sys

TAG_START = re.compile(r'<(form|button)(?=[\s/>])', re.I)
WS = re.compile(r'\s+')
ATTR = re.compile(
    r'''([^\s"'<>/=]+)                     # 属性名
        (?:\s*=\s*
           (?: "([^"]*)"                   # 双引号值
             | '([^']*)'                   # 单引号值
             | ([^\s"'=<>]+)               # 裸值
           )
        )?''',
    re.X,
)

def strip_comments(src):
    """剥 Jet 注释 {* … *}：注释里解释历史，不当违规。"""
    out, i = [], 0
    while True:
        j = src.find('{*', i)
        if j < 0:
            out.append(src[i:]); break
        out.append(src[i:j])
        k = src.find('*}', j)
        if k < 0: break
        i = k + 2
    return ''.join(out)

def scan_tag(text, m):
    """从 '<form'/'<button' 起扫描到引号外的 '>'。
    返回 (空白归一后的标签, 结束位置)；模板在 Jet '{{…}}' 里被截断时返回 (None, len)。"""
    start, i, n = m.start(), m.end(), len(text)
    q = None
    while i < n:
        c = text[i]
        if q is not None:
            if c == q:
                q = None
        elif c == '"' or c == "'":
            q = c
        elif c == '>':
            return WS.sub(' ', text[start:i + 1]), i + 1
        elif text.startswith('{{', i):
            k = text.find('}}', i)
            if k < 0 or '>' in text[i:k]:
                return None, n
            i = k + 2
            continue
        i += 1
    return None, n

def attrs_of(kind, tag):
    """→ [(name, value, quoted)]；裸属性 value 为 None。HTML 属性名大小写不敏感。"""
    body = tag[1 + len(kind):]
    if body.endswith('>'):
        body = body[:-1]
    if body.endswith('/'):
        body = body[:-1]
    out = []
    for a in ATTR.finditer(body):
        name = a.group(1).lower()
        if a.group(2) is not None:
            out.append((name, a.group(2), True))
        elif a.group(3) is not None:
            out.append((name, a.group(3), True))
        elif a.group(4) is not None:
            out.append((name, a.group(4), False))
        else:
            out.append((name, None, False))
    return out

def emit(text):
    pos = 0
    while True:
        m = TAG_START.search(text, pos)
        if not m:
            return
        tag, pos = scan_tag(text, m)
        if tag:
            print(m.group(1).lower() + TAB + tag)

ENUM = ('create', 'update', 'delete', 'bulk', 'custom', 'single')
TAB = chr(9)

def is_write_tag(kind, attrs):
    """**语义**阶段的作用域（E1/E2/E3a/E3b/E5b 判不判），形态阶段（E4/E5a）不看它。

    写声明的判据（write-base.js onSubmit）：**表单或提交按钮任一处**带值 data-write
    （hasAttr 合并两处），再要求两处合并后 attr('data-write-url') 非空。孤立一行看不到
    表单，所以这里只认「本标签带值 data-write」或「本标签自报 data-write-url」。

    只带 data-write-nav / -confirm 这类带值属性的普通非写标签不是写声明 —— 不进语义判据，
    避免「已删属性名/mode 枚举」这类相对写声明才有意义的判据误报。
    「带值」是必须的：裸 data-write 自己就是形态错误（E4），由形态阶段报。
    """
    own = {}
    for n, v, _q in attrs:
        own.setdefault(n, v)
    if 'data-write' in own:
        # 出现即算写声明，不要求带值：裸 data-write 是很明确的形态错误（形态阶段报 E4），
        # 但它同时**也是**一次写声明 —— 跳过语义阶段的具体表现是
        # 「<button data-write data-write-number>」只报 E4 不报 E1，然后有人把裸
        # data-write 补成 data-write="update" 就绿了，已删属性照样漏在页面上。
        return True
    if 'data-write-url' in own:
        # 自报 url 同样是写声明（即使漏了 mode，缺的是值不是声明）。
        return True
    return False

def judge(stream):
    bad = 0
    for raw in stream:
        raw = raw.rstrip('\n')
        if not raw:
            continue
        kind, sep, tag = raw.partition(TAB)
        if not sep:
            print('E0' + TAB + raw + TAB + TAB + TAB)
            bad += 1
            continue
        kind, tag = kind.strip().lower(), tag.strip()
        attrs = attrs_of(kind, tag)
        names = [n for n, _, _ in attrs]
        where = tag
        own = {}
        for n, v, _q in attrs:
            own.setdefault(n, v)
        # ── 形态阶段：无条件，先于任何作用域过滤 ──────────────────────────────
        # 未登记的裸 data-write-*（含拼错的裸 flag）与带值的行为开关，写在孤立 button 上
        # 也照样静默失效；一旦先过 is_write_tag，「无 url」的标签就被 continue 掉。
        for flag in BEHAVIOR_MODES.keys():
            if flag in names and own[flag] is not None:
                print('E5a' + TAB + where + TAB + flag + TAB + own[flag])
                bad += 1
        for name, val, _q in attrs:
            if val is not None:
                continue
            if name == 'data-write':
                # 裸 data-write 自己就是形态错误：基座按值取 mode（write-base.js 的
                # modeOf 读 data-write 的值），裸写得到空 mode，提交必然走到「未知 mode」
                # 分支。语义阶段还会并行报 E3a（缺 mode）—— 两条是**不同事实**：
                # 一条说「这个属性没值」，一条说「这次写动作没有 mode」。保留两条，
                # 因为修法不同（补值 vs 补 enum 内的值）；合成一条会丢掉其中之一。
                print('E4' + TAB + where + TAB + name + TAB)
                bad += 1
                continue
            if not name.startswith('data-write-'):
                continue
            if name == 'data-write-url':
                continue
            if name in ALLOWED_SHAPING or name in BEHAVIOR_FLAGS:
                continue
            print('E4' + TAB + where + TAB + name + TAB)
            bad += 1
        # ── 语义阶段：只对写声明判 ──────────────────────────────────────────
        # 已删属性名、htmx 冲突、mode 枚举、url 前缀都相对「这是一次写动作」才有意义。
        if not is_write_tag(kind, attrs):
            continue
        for rem in REMOVED.keys():
            if rem in names:
                print('E1' + TAB + where + TAB + rem + TAB + REMOVED[rem])
                bad += 1
        for hx in NO_HTMX.keys():
            if hx in names:
                print('E2' + TAB + where + TAB + hx + TAB + NO_HTMX[hx])
                bad += 1
        mode_here = WS.sub(' ', own.get('data-write') or '').strip()
        if kind == 'form':
            if mode_here not in ENUM:
                print('E3a' + TAB + where + TAB + mode_here + TAB)
                bad += 1
            url = WS.sub(' ', own.get('data-write-url') or '').strip()
            if not url.startswith('/api/'):
                print('E3b' + TAB + where + TAB + url + TAB)
                bad += 1
        # E5b：mode 白名单只对 form 判 —— 按钮声明继承/覆盖表单的 mode，孤立一行判不出真值。
        if kind == 'form':
            for flag in BEHAVIOR_MODES.keys():
                if flag not in names:
                    continue
                allowed = BEHAVIOR_MODES[flag]
                if mode_here not in allowed.split():
                    print('E5b' + TAB + where + TAB + flag + TAB + '|'.join(allowed.split()) + TAB + (mode_here or '未声明'))
                    bad += 1
    print('BAD' + TAB + str(bad))
    return bad

if __name__ == '__main__':
    if sys.argv[1] == 'extract':
        _src = open(sys.argv[2], encoding='utf-8').read()
        if _src.startswith('\ufeff'):
            _src = _src[1:]
        emit(strip_comments(_src))
    else:
        sys.exit(1 if judge(sys.stdin) > 0 else 0)
PY

# 五本账 → Python 字面量。格式串写死、字典名单独传参：
# 用 "$1" 当 printf 的格式串会让说明里的 %s 变成格式指令。
python_registry() {
  emit_dict() {
    local -n _d="$1"
    local k v
    printf "%s = {}\n" "$1"
    for k in "${!_d[@]}"; do
      v="${_d[$k]//$'\n'/ }"
      printf "%s['%s'] = '%s'\n" "$1" "$k" "$v"
    done
  }
  emit_dict REMOVED
  emit_dict NO_HTMX
  emit_dict ALLOWED_SHAPING
  emit_dict BEHAVIOR_FLAGS
  emit_dict BEHAVIOR_MODES
}

# 可选参数：judge（stdin 判据，测试用）或文件路径子串（只判该文件）。
TAB_LIT="$(printf '\t')"

FILTER=""
if [[ $# -ge 1 && "$1" != "judge" ]]; then FILTER="$1"; fi

PARSE_PY="$(mktemp)"
TAGFILE="$(mktemp)"
JUDGE_OUT="$(mktemp)"
ERRPY="$(mktemp)"
trap 'rm -f "$PARSE_PY" "$TAGFILE" "$JUDGE_OUT" "$ERRPY"' EXIT
{
  python_registry
  printf '%s\n' "$EXTRACT_PY"
} >"$PARSE_PY"

if [[ "${1:-}" == "judge" ]]; then
  python3 "$PARSE_PY" judge
  exit $?
fi

# 遍历模板：按 <form ...> 与 <button ...> 逐个判（属性可能写在任一处）。
while IFS= read -r file; do
  [[ -n "$FILTER" && "$file" != *"$FILTER"* ]] && continue
  python3 "$PARSE_PY" extract "$file" >"$TAGFILE" 2>"$ERRPY"
  if [[ -s "$ERRPY" ]]; then
    # 解析器自己的异常不能吞掉：否则门禁会「没输出=合规」地绿灯。
    fail "$file：标签提取失败 —— $(tr '\n' ' ' <"$ERRPY" | cut -c1-200)"
    continue
  fi
  # 判据输出落盘再读：管道右侧是子 shell，里面的 fail 计数不会回传。
  python3 "$PARSE_PY" judge <"$TAGFILE" >"$JUDGE_OUT" 2>"$ERRPY" || true
  if [[ -s "$ERRPY" ]]; then
    fail "$file：判据执行失败 —— $(tr '\n' ' ' <"$ERRPY" | cut -c1-200)"
    continue
  fi
  while IFS="$TAB_LIT" read -r code tag what extra; do
    [[ -z "$code" || "$code" == "BAD" ]] && continue
    case "$code" in
      E0) fail "$file：解析器收到的行没有 kind TAB 分隔（$tag）" ;;
      E1) fail "$file：写表单上出现已删除的 $what（现在应为 $extra）" ;;
      E2) fail "$file：写表单上出现 $what —— $extra" ;;
      E3a)
        if [[ -z "$what" ]]; then
          fail "$file：写表单有 data-write-url 但缺 data-write（必填）"
        else
          fail "$file：data-write=\"$what\" 不在枚举里（create/update/delete/bulk/custom/single）"
        fi ;;
      E3b) fail "$file：data-write-url=\"$what\" 必须以 /api/ 开头（写基座只对接 JSON 端点）" ;;
      E4) fail "$file：写表单上出现未登记的裸属性 $what（基座不读它 → 什么都不会发生；拼错还是新增？）" ;;
      E5a) fail "$file：$what 是行为开关，必须写成裸属性（当前带值 \"$extra\" —— 基座只读存在性，值被静默忽略）" ;;
      E5b) fail "$file：$what 只允许出现在 data-write=\"$extra\" 上（此处 mode=\"$what\"）" ;;
      *) fail "$file：$code $tag" ;;
    esac
  done <"$JUDGE_OUT"
done < <(find "$TPL_DIR" -type f \( -name '*.html' -o -name '*.jet' \) | sort)

if [[ $violations -gt 0 ]]; then
  echo
  echo "写表单契约见 internal/templates/CLAUDE.md「写操作」与 ui/write-base.js 文件头。" >&2
  exit 1
fi

BASE_JS="$ROOT/internal/templates/static/js/ui/write-base.js"
if [[ ! -f "$BASE_JS" ]]; then
  fail "写基座不存在：$BASE_JS"
else
  for attr in "${!ALLOWED_SHAPING[@]}"; do
    if ! grep -q "$attr" "$BASE_JS"; then
      fail "白名单里的 $attr 在 ui/write-base.js 里从未被读取 —— 属性写了不生效，模板却仍判合规"
    fi
  done

  # 行为开关单独按消费点核对（见下）：账上点名 base_literal 的函数体里必须出现属性字面量。
  for attr in "${!BEHAVIOR_FLAGS[@]}"; do
    fn="${BEHAVIOR_FLAGS[$attr]}"
    if [[ -n "$fn" ]]; then
      if ! python3 - "$BASE_JS" "$attr" "$fn" <<'PY'
import re, sys
src = open(sys.argv[1], encoding='utf-8').read()
attr, fn = sys.argv[2], sys.argv[3]
m = re.search(r'function\s+' + re.escape(fn) + r'\s*\([^)]*\)\s*\{', src)
if not m:
    print('missing function ' + fn); sys.exit(1)
i, depth = m.end(), 1
while i < len(src) and depth:
    depth += {'{': 1, '}': -1}.get(src[i], 0)
    i += 1
body = src[m.end():i]
pat = re.compile(r'''(?:hasAttr|attr|hasAttribute|getAttribute)\s*\([^)]*''' + re.escape(attr) + r'''[^)]*\)|['"]''' + re.escape(attr) + r'''['"]''')
sys.exit(0 if pat.search(body) else 1)
PY
      then
        fail "行为开关 $attr 登记为 $fn 消费，但 $fn 的函数体里没有消费该属性字面量 —— 登记不成立"
      fi
    elif ! grep -q "$attr" "$BASE_JS"; then
      fail "行为开关 $attr 在 ui/write-base.js 里从未被读取 —— 属性写了不生效"
    fi
  done
fi

printf '✓ 写表单属性合规（白名单 %d 个整形/开关属性均基座可读 / %d 个行为开关均基座消费 / %d 个已删属性 / %d 个禁用 htmx 属性）\n' \
  "${#ALLOWED_SHAPING[@]}" "${#BEHAVIOR_FLAGS[@]}" "${#REMOVED[@]}" "${#NO_HTMX[@]}"
