#!/usr/bin/env bash
# 动效命名规范：入场一律 `<效果>-in[-<方向>]`。
#
# 为什么需要这道门禁：动效名同时是 **CSS 类名**（`sky-<名>`）与 **@keyframes 名**，
# 而写错的表现是「产物引用了不存在的关键帧」—— 页面上只表现为「不动」，不报错、
# 不 500、日志干净。仓库里既有 `fade-in-top-left` / `back-in-up` / `bounce-in-down`
# 这种规范名，也有过 `fade-up` / `slide-up` / `flip-x` / `zoom-out` 这种不带 `-in`
# 的例外 —— 两类混在一起时，写模板的人没法凭命名猜到该用哪个。
#
# 2026-11 已把 11 个例外统一（fade-up → fade-in-up、slide-up → slide-in-up、
# flip-x → flip-in-x、zoom-out → zoom-in-out），本门禁守「不再新增例外」。
#
# 判据：
#   1. internal/builder/core/groups.go 的 allowedEntrance 里，每个名字必须匹配
#      `<效果>-in`、`<效果>-in-<方向>`，或属于静态白名单（见下）。
#   2. 动效名必须在 builtin.css / animate.css 里有关键帧（反向核对）。
#
# 静态白名单：`blur-in` / `bounce-in` / `rotate-in` / `roll-in` / `zoom-in` 等
# 本身就是「-in 结尾」的形态，不需要方向后缀。
#
# 退出码 0 = 通过；1 = 有违规。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

GROUPS_GO="internal/builder/core/groups.go"
KF_DIR="internal/builder/core/keyframes"

violations=0
fail() { printf '✗ %s\n' "$1" >&2; violations=$((violations + 1)); }

# 从 allowedEntrance 抽出全部名字，逐个判命名与关键帧。
naming_rc=0
WB_GROUPS="$GROUPS_GO" WB_KF_DIR="$KF_DIR" python3 - <<'PY' || naming_rc=$?
import re, sys, os

groups = os.environ['WB_GROUPS']
kf_dir = os.environ['WB_KF_DIR']
src = open(groups, encoding='utf-8').read()
i = src.index('allowedEntrance = map[string]bool{')
j = src.index('\n\t}', i)
names = re.findall(r'"([a-z][a-z0-9-]*)":\s*true', src[i:j])

# 关键帧全集（两个共享词汇表文件）
kf = set()
for fn in ('builtin.css', 'animate.css'):
    text = open(os.path.join(kf_dir, fn), encoding='utf-8').read()
    kf.update(re.findall(r'@keyframes\s+(sky-[a-z0-9-]+)', text))

violations = 0

# 判据 1：命名必须带 -in（除了以 -in 结尾、或 -in-<方向> 的形态）。
# 允许的两种正则：
#   <效果>-in              例：fade-in / blur-in / zoom-in
#   <效果>-in-<方向>       例：fade-in-up / back-in-top-left / zoom-in-down
# 效果名可以是多词（Animate.css 的 light-speed-in-left / jack-in-the-box）——
# 判据只要求**含 -in**，且 -in 之后最多两段（方向词）。
ok_name = re.compile(r'^[a-z]+(-[a-z]+)*-in(-[a-z]+){0,2}$|^jack-in-the-box$')
for n in names:
    if not ok_name.match(n):
        print(f'✗ 动效名 "{n}" 不合规：入场名应为 <效果>-in 或 <效果>-in-<方向>'
              f'（例 fade-in / fade-in-up）', file=sys.stderr)
        violations += 1

# 判据 2：每个名字都必须有关键帧。
for n in names:
    if f'sky-{n}' not in kf:
        print(f'✗ 动效名 "{n}" 没有对应的 @keyframes sky-{n} —— '
              f'产物会引用一个不存在的动画，页面上只表现为「不动」', file=sys.stderr)
        violations += 1

if violations:
    print(f'\n共 {violations} 处违规（扫 {len(names)} 个入场动效）', file=sys.stderr)
    sys.exit(1)
print(f'✓ 动效命名合规（{len(names)} 个入场动效，全部带 -in 且有对应关键帧）')
PY
[[ $naming_rc -ne 0 ]] && violations=1

if [[ $violations -gt 0 ]]; then
  echo
  echo "动效词汇表见 internal/builder/core/groups.go 与 core/keyframes/。" >&2
  exit 1
fi
