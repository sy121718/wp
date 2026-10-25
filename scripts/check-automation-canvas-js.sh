#!/usr/bin/env bash
# check-automation-canvas-js.sh — 画布前端模块的逻辑门禁（node 直接跑，无浏览器）。
#
# 为什么需要它：画布是最容易「看起来没问题」的一块 —— 布局算错只表现为节点叠在一起、
# 撤销栈写错只表现为按 Ctrl+Z 没反应，两者都不会让 Go 测试变红，也不会在页面上报错。
# 把 state / layout 写成**纯函数**（不碰 DOM）就是为了能在 node 里直接验它们。
#
# 判据分四组：
#   1. layout：覆盖全部节点、同层不重叠、子节点右移、已有坐标不被覆盖、结果确定；
#   2. state：建/删/连/断/插/撤销重做/toDefinition 的形状；
#   3. render：连线与端口是纯字符串/DOM 片段，不读全局 document；
#   4. 结构：六个模块都在，且**没有任何模块**使用 location.reload（保存后不刷新）。
#
# node 缺失时**跳过并明确打印**（不阻断）：CI 的 Go job 没有 node；跳过的输出可被检索。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

DIR="internal/templates/static/js/automation"

if ! command -v node >/dev/null 2>&1; then
  echo "skip: node 未安装，画布 JS 逻辑检查跳过"
  exit 0
fi

fail=0

# ── 结构：六个模块都在 ──
for f in state.js layout.js render.js menu.js index.js graph.js; do
  if [[ ! -f "$DIR/$f" ]]; then
    echo "✗ 缺少画布模块 $DIR/$f" >&2
    fail=1
  fi
done

# ── 结构：保存后不刷新（注释里提到不算：注释正是在说明「为什么不再 reload」） ──
if grep -rn 'location\.reload' "$DIR" | grep -v ':[0-9]*: *//' | grep -v ':[0-9]*: *\*' >/dev/null 2>&1; then
  echo "✗ 画布仍有 location.reload：保存成功只该更新状态栏并把新 id 写回地址栏" >&2
  grep -rn 'location\.reload' "$DIR" | grep -v ':[0-9]*: *//' | grep -v ':[0-9]*: *\*' >&2
  fail=1
fi

# ── 结构：保存端点只出现一次（不许有第二份实现） ──
save_hits=$(grep -rn "'/api/mail/automation/save'" "$DIR" | wc -l | tr -d ' ')
layout_hits=$(grep -rn "'/api/mail/automation/layout'" "$DIR" | wc -l | tr -d ' ')
if [[ "$save_hits" != "1" ]]; then
  echo "✗ /api/mail/automation/save 在画布模块里出现 $save_hits 次（期望 1）" >&2
  fail=1
fi
if [[ "$layout_hits" != "1" ]]; then
  echo "✗ /api/mail/automation/layout 在画布模块里出现 $layout_hits 次（期望 1）" >&2
  fail=1
fi

# ── 逻辑：node 跑纯函数 ──
if ! node --input-type=module -e '
const dir = "./internal/templates/static/js/automation/";
const layout = await import(dir + "layout.js");
const state = await import(dir + "state.js");
const render = await import(dir + "render.js");

function assert(cond, msg) {
  if (!cond) { console.error("✗ " + msg); process.exit(1); }
}

/* 1. layout：覆盖全部节点、同层不重叠、子节点右移 */
const nodes = [
  { key: "n1", type: "trigger", out: { next: "n2" } },
  { key: "n2", type: "filter", out: { yes: "n3", no: "n4" } },
  { key: "n3", type: "end", out: {} },
  { key: "n4", type: "end", out: {} }
];
const p = layout.layout(nodes, "n1", {});
assert(Object.keys(p).sort().join(",") === "n1,n2,n3,n4", "layout 漏节点: " + Object.keys(p));
assert(!(p.n2.x === p.n3.x && p.n2.y === p.n3.y), "n2 与 n3 重叠");
assert(!(p.n3.x === p.n4.x && p.n3.y === p.n4.y), "同层两个 end 重叠");
assert(p.n3.x > p.n2.x, "子节点未右移");

/* 1b. layout：已有坐标不被覆盖 */
const keep = { n2: { x: 999, y: 777 } };
const p2 = layout.layout(nodes, "n1", keep);
assert(p2.n2.x === 999 && p2.n2.y === 777, "已有坐标被覆盖了");
assert(typeof p2.n1.x === "number", "新节点没排位");

/* 1c. layout：确定性（同一输入两次结果一致） */
const p3 = layout.layout(nodes, "n1", {});
assert(JSON.stringify(p2) === JSON.stringify(layout.layout(nodes, "n1", keep)), "layout 不确定");

/* 1d. layout：不可达节点也要有位（坏图不该渲染不出） */
const orphan = [{ key: "a", out: {} }, { key: "b", out: {} }];
const p4 = layout.layout(orphan, "", {});
assert(Object.keys(p4).length === 2, "不可达节点漏排位");

/* 2. state：建节点 / 连边 / 删节点带边 / 撤销重做 */
let st = state.createState({ entry: "n1", nodes: [{ key: "n1", type: "trigger", out: {} }], nodeTypes: [] });
let added = state.addNode(st, "delay", { x: 100, y: 100 }, { from: "n1", outlet: "next" });
st = added.state;
assert(st.nodes.length === 2, "addNode 没加进去");
const n1 = state.nodeByKey(st, "n1");
assert(n1.out.next === added.key, "addNode 没连边: " + JSON.stringify(n1.out));

st = state.removeNode(st, added.key);
assert(st.nodes.length === 1, "removeNode 没删掉");
assert(!state.nodeByKey(st, "n1").out.next, "removeNode 留下了悬空边");

st = state.undo(st);
assert(st.nodes.length === 2, "undo 没恢复节点");
st = state.redo(st);
assert(st.nodes.length === 1, "redo 没重做");

/* 2b. insertOnEdge：三段都在 */
let s2 = state.createState({ entry: "n1", nodes: [
  { key: "n1", type: "trigger", out: { next: "n2" } },
  { key: "n2", type: "end", out: {} }
], nodeTypes: [] });
const ins = state.insertOnEdge(s2, "n1", "next", "delay");
assert(ins.state.nodes.length === 3, "insertOnEdge 没插入节点");
const mid = state.nodeByKey(ins.state, ins.key);
assert(mid.type === "delay", "插入的节点类型不对");
assert(state.nodeByKey(ins.state, "n1").out.next === ins.key, "source 没指向新节点");
assert(mid.out.next === "n2", "新节点没指向 target");

/* 2c. connect 覆盖同一个出口（一个出口只能有一个目标） */
let s3 = state.createState({ entry: "n1", nodes: [
  { key: "n1", type: "trigger", out: { next: "n2" } },
  { key: "n2", type: "end", out: {} },
  { key: "n3", type: "end", out: {} }
], nodeTypes: [] });
s3 = state.connect(s3, "n1", "next", "n3");
assert(state.nodeByKey(s3, "n1").out.next === "n3", "connect 没覆盖旧目标");

/* 2d. toDefinition 形状：schema 2 + 空出口不入 map */
const def = state.toDefinition(state.createState({ entry: "n1", nodes: [
  { key: "n1", type: "trigger", out: {} }
], nodeTypes: [] }));
assert(def.schema === 2, "toDefinition 的 schema 不是 2");
assert(Object.keys(def.nodes[0].out).length === 0, "空出口进了 map");
assert(def.entry === "n1", "toDefinition 丢了 entry");

/* 2e. 撤销栈合并：300ms 内的同类连续移动只进一帧 */
let s4 = state.createState({ entry: "n1", nodes: [{ key: "n1", type: "trigger", out: {} }], nodeTypes: [] });
for (let i = 1; i <= 10; i++) { s4 = state.moveNode(s4, "n1", i * 8, 0); }
assert(s4.past.length === 1, "连续拖拽应为 1 帧撤销点，实际 " + s4.past.length);

/* 3. render：连线是纯字符串，端口不碰全局 document */
const html = render.renderEdges(state.createState({ entry: "n1", nodes: [
  { key: "n1", type: "trigger", out: { next: "n2" } },
  { key: "n2", type: "end", out: {} }
], nodeTypes: [] }), {});
assert(html.indexOf("<path") > 0, "renderEdges 没产出 path");
assert(html.indexOf("auto-edge") > 0, "renderEdges 没标出边");

const frag = render.renderNodes(state.createState({ entry: "n1", nodes: [
  { key: "n1", type: "trigger", out: {} }
], nodeTypes: [] }), {});
assert(frag === null, "renderNodes 在没有 document 时应返回 null（纯函数不碰全局）");

console.log("ok: canvas js logic");
' ; then
  echo "✗ 画布 JS 逻辑检查未通过" >&2
  fail=1
fi

if (( fail != 0 )); then
  echo "✗ 画布门禁未通过" >&2
  exit 1
fi
echo "✓ 画布门禁通过（六个模块在位 / 无 location.reload / 保存端点各 1 处 / 纯函数逻辑）"
