#!/usr/bin/env bash
# 写表单的字段名对账：模板里的 name="X" 必须出现在目标 DTO 的字段表里。
#
# 为什么需要这道门禁：字段名写错的表现是**静默丢弃** —— 模板写 name="sort"、
# DTO 叫 sortOrder，两边都不报错，服务端收到一个它不认识的键（严格白名单下整条 400，
# 宽松绑定下**静默忽略**），用户看到「保存成功但没改」。
#
# 数据来源：`cmd/write-contracts` 生成物（internal/templates/static/js/ui/write-contracts.json）。
# 它把「端点 → DTO → 字段表」这条链固定下来，DTO 一侧由 form tag 决定
#（批次 2 已给全部写 DTO 补齐，99 条存量登记已清零）。
#
# 判据：
#   1. 每个写表单的字段名必须在它 data-write-url 对应 DTO 的字段表里；
#   2. DTO 上标了 binding:"required" 的字段必须出现在表单里。
#
# 已知豁免（写进豁免表，必须逐条写理由）：
#   · 端点不在契约文件里（如极少数用路径参数而非请求体的写端点）—— 逐条登记。
#
# 退出码 0 = 通过；1 = 有对不上的字段。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

CONTRACTS="internal/templates/static/js/ui/write-contracts.json"
if [[ ! -f "$CONTRACTS" ]]; then
  echo "✗ 找不到写端点契约文件 $CONTRACTS —— 先跑 go run ./cmd/write-contracts" >&2
  exit 1
fi

python3 - "$CONTRACTS" <<'PY'
import json, os, re, sys

contracts = json.load(open(sys.argv[1], encoding='utf-8'))
root = 'internal/templates/admin'

# 不在契约里的端点。每条都要写理由 —— 这里都是「形状分派型」写端点：
# handler 用裸 JSON map（map[string]json.RawMessage）按**顶层键**决定走哪条分支，
# 而不是 `binding.JSON[T]` 绑一个固定 DTO，所以契约生成器取不到字段表。
# 它们的字段契约由各自的 handler 测试与 feature chain 守，不由本门禁守。
EXEMPT_ENDPOINTS = {
    # 裸 JSON map + 形状分派（单档 / 批档并集）：字段表在 handler 里按分支判。
    '/api/order/status': '裸 JSON map：单档/批档按顶层键分派（order_write.go）',
    '/api/order/cancel': '同上',
    '/api/order/note': '同上',
    '/api/order/refund': '同上',
    '/api/order/coupon/delete': '同上',
    '/api/navigation/create': '裸 JSON map：action=addSource 形状分派',
    '/api/navigation/update': '同上',
    '/api/navigation/delete': '同上',
    '/api/mail/contact/save': '裸 JSON map：含 tags 数组与 consent 分支',
    '/api/mail/contact/delete': '同上',
    '/api/customer/status': '裸 JSON map：批量/单档形状分派',
    '/api/customer/bulk-status': '同上',
    '/api/customer/unlock': '同上',
    '/api/membership/assign/set': '裸 JSON map：手动分配与解锁两档',
    '/api/membership/assign/unlock': '同上',
    '/api/i18n/save': '裸 JSON map：单条/批量词条写入',
    '/api/contenttemplate/delete': '仅路径/单 id，无请求体字段',
    # 泛型写辅助函数：binding 包在 helper[T] 里，类型实参在调用处的函数字面量中，
    # 生成器已能追（dtoFromGenericHelperCall），但下面这些的 helper 签名更绕，暂未覆盖。
    '/api/product/attribute/set-values': '泛型辅助：逐值行由前端的 data-write-rows 组装',
    '/api/product/bundle/set': '泛型辅助：成员表由 data-write-rows + builder 组装',
    '/api/product/variant/save': '泛型辅助：变体行由 builder 组装',
    '/api/product/detail-template/update-url': '泛型辅助',
    '/api/product/detail-template/reapply-preset': '泛型辅助',
    '/api/product/detail-template/rollback-document': '泛型辅助',
}

violations = 0
checked_forms = 0


def strip_comments(src):
    out = []
    while True:
        i = src.find('{*')
        if i < 0:
            out.append(src)
            return ''.join(out)
        out.append(src[:i])
        j = src.find('*}', i)
        if j < 0:
            return ''.join(out)
        src = src[j + 2:]


def form_fields(block):
    """表单块里的具名控件。跳过 ids/ids[]（bulk 专用，不在 DTO 字段表里）。"""
    names = []
    for m in re.finditer(r'<input\b[^>]*>|<select\b[^>]*>|<textarea\b[^>]*>', block, re.S):
        tag = m.group(0)
        nm = re.search(r'\bname="([^"]+)"', tag)
        if not nm:
            continue
        name = nm.group(1)
        if '{{' in name:      # 动态名（如 {{f.Name}}）交给运行时
            continue
        if name in ('ids', 'ids[]', 'csrf_token'):
            continue
        names.append(name)
    return names


for dirpath, _, files in os.walk(root):
    for name in sorted(files):
        if not (name.endswith('.html') or name.endswith('.jet')):
            continue
        path = os.path.join(dirpath, name)
        rel = os.path.relpath(path, root)
        text = strip_comments(open(path, encoding='utf-8').read())

        for m in re.finditer(r'<form\b([^>]*)>', text, re.S):
            attrs = re.sub(r'\s+', ' ', m.group(1))
            url_m = re.search(r'\bdata-write-url="([^"]*)"', attrs)
            if not url_m:
                continue
            url = url_m.group(1)
            if '{{' in url:
                continue   # 动态端点：契约按静态路径索引，运行时才对得上
            if url not in contracts:
                if url in EXEMPT_ENDPOINTS:
                    continue
                print(f'✗ {rel}：端点 {url} 不在写端点契约里（先重跑 go run ./cmd/write-contracts；'
                      f'若它确实不是请求体型写端点，加进本脚本的 EXEMPT_ENDPOINTS 并写理由）')
                violations += 1
                continue

            end = text.find('</form>', m.end())
            block = text[m.start(): end if end >= 0 else len(text)]
            spec = contracts[url]
            allowed = {f['name'] for f in spec.get('fields', [])}
            required = {f['name'] for f in spec.get('fields', []) if f.get('required')}
            checked_forms += 1

            present = form_fields(block)
            offered = set(present)

            # data-write-builder：页面级组装器会把**页面字段名**映射成 DTO 字段
            #（membership 的 thresholdYuan → thresholdAmount、freeShipping+discountPercent
            # → entitlements 数组）。这种映射只有页面脚本知道，本门禁看不到 —— 跳过对账。
            #
            # 谁来守这些映射：内联脚本自己（ui/membership_write.js 有 membership_write.test.cjs；
            # ui/product_variant_write.js **当前没有测试**）。注释此前把后者写成
            # product_bundle_write.test.cjs —— 那个文件从来不存在（bundle 概念已下线），
            # 于是"由测试守"这句话对商品变体行一直是假的。缺的那条测试记在 B00 的待办里，
            # 不在这里假装它有。
            if 'data-write-builder=' in attrs:
                continue

            for fname in present:
                # 嵌套字段由 data-write-json 组装，可能不在顶层字段表里 —— 放行。
                if 'data-write-json=' in attrs and fname not in allowed:
                    continue
                if fname not in allowed:
                    print(f'✗ {rel}：字段 name="{fname}" 不在 {spec["dto"]} 的字段表里'
                          f'（端点 {url}）—— 服务端会静默丢掉它')
                    violations += 1

            for fname in sorted(required):
                if fname not in offered:
                    print(f'✗ {rel}：{spec["dto"]} 的必填字段 "{fname}" 没有出现在表单里'
                          f'（端点 {url}）—— 提交必然失败')
                    violations += 1

if violations:
    print()
    print('写表单契约见 internal/templates/CLAUDE.md「写操作」。', file=sys.stderr)
    sys.exit(1)
print(f'✓ 写表单字段名全部对得上（{checked_forms} 个表单 vs {len(contracts)} 条端点契约）')
PY
