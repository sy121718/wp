#!/usr/bin/env bash
# 写表单的端点存在性：模板里每个 data-write-url 都必须在运行时路由表里注册过。
#
# 为什么需要这道门禁：模板写错端点路径的表现是**静默的 404** —— 用户点保存，
# 基座照常发请求，服务端回 404（HTML 或 JSON），界面上只有一句泛化的「保存失败」。
# 而正确的失效模式应该是「改模板时就红」。真实场景：改了路由分组前缀
#（`/api/product/brand/create` → `/api/product/brands/create`），模板没跟着改。
#
# 数据来源：运行时路由表（`WP_DUMP_ROUTES=1 go test ./internal/routers -run TestDumpRoutesForAudit`）。
# 不用静态扫描 —— 路由是 `SetupXxxRoutes(permission.NewRouteGroup(api), db)` 这种跨包
# 传递注册的，静态拼前缀必然出错。
#
# 需要数据库（装配路由表），拿不到时明确跳过而不是静默通过。
#
# 退出码 0 = 通过；1 = 有未注册的端点；0 且打印「跳过」= 环境缺库。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

TPL_DIR="internal/templates/admin"

# 收集模板里用到的写端点（可能是带 {{…}} 的动态段，单独处理）。
collect_endpoints() {
  python3 - "$TPL_DIR" <<'PY'
import os, re, sys
root = sys.argv[1]
found = {}
for dirpath, _, files in os.walk(root):
    for name in files:
        if not (name.endswith('.html') or name.endswith('.jet')):
            continue
        path = os.path.join(dirpath, name)
        src = open(path, encoding='utf-8').read()
        # 剥 Jet 注释
        out = []
        while True:
            i = src.find('{*')
            if i < 0:
                out.append(src); break
            out.append(src[:i])
            j = src.find('*}', i)
            if j < 0: break
            src = src[j+2:]
        text = ''.join(out)
        for m in re.finditer(r'data-write-url="([^"]*)"', text):
            url = m.group(1)
            found.setdefault(url, set()).add(os.path.relpath(path, root))
for url in sorted(found):
    print(url + '\t' + ' '.join(sorted(found[url])))
PY
}

mapfile -t DECLARED < <(collect_endpoints)
if [[ ${#DECLARED[@]} -eq 0 ]]; then
  echo "✗ 一个 data-write-url 都没扫到 —— 扫描范围写错了" >&2
  exit 1
fi

# 拿运行时路由表。缺库时明确跳过（不是静默通过）。
routes_raw="$(WP_DUMP_ROUTES=1 go test ./internal/routers/ -run TestDumpRoutesForAudit -count=1 -v 2>/dev/null || true)"
mapfile -t ROUTES < <(printf '%s\n' "$routes_raw" | grep -E '^[A-Z]+ /api' | awk '{print $2}' | sort -u)
if [[ ${#ROUTES[@]} -eq 0 ]]; then
  echo "· 拿不到运行时路由表（需要数据库装配）—— 跳过端点存在性检查"
  exit 0
fi

violations=0
# 静态端点（不含 {{…}}）逐个精确比对；动态段（含 {{…}}）按前缀比对。
for row in "${DECLARED[@]}"; do
  url="${row%%$'\t'*}"
  where="${row#*$'\t'}"
  if [[ "$url" == *'{{'* ]]; then
    # 动态端点：把 {{…}} 当成通配，用前缀匹配到某条已注册路由。
    prefix="${url%%'{{'*}"
    hit=0
    for r in "${ROUTES[@]}"; do
      if [[ "$r" == "$prefix"* ]]; then hit=1; break; fi
    done
    if [[ $hit -eq 0 ]]; then
      printf '✗ %s：动态端点 %s 的前缀 %s 没有匹配到任何已注册路由\n' "$where" "$url" "$prefix" >&2
      violations=$((violations + 1))
    fi
    continue
  fi
  found=0
  for r in "${ROUTES[@]}"; do
    if [[ "$r" == "$url" ]]; then found=1; break; fi
  done
  if [[ $found -eq 0 ]]; then
    printf '✗ %s：data-write-url="%s" 没有在路由表里注册（点了只会 404）\n' "$where" "$url" >&2
    violations=$((violations + 1))
  fi
done

if [[ $violations -gt 0 ]]; then
  echo
  echo "写表单契约见 internal/templates/CLAUDE.md「写操作」。" >&2
  exit 1
fi
printf '✓ 写表单端点全部已注册（%d 个端点 / 路由表 %d 条）\n' "${#DECLARED[@]}" "${#ROUTES[@]}"
