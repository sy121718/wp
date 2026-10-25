// automation/layout.js — 分层自动布局（纯函数）。
//
// 输入一份图的节点与入口，输出 `{key: {x,y}}`。
//
// ## 两条硬约束
//
//  1. **不覆盖已有坐标**：作者手工摆的位置是劳动成果，自动布局只在「新节点还没有位置」
//     或显式「重新布局」时生效。genuinely 每一次保存都重排的画布，用户会放弃摆位。
//  2. **确定性**：同一份图永远得到同一份坐标（同层按 key 排序）。否则每次打开画布看到的
//     图都不一样，而这个 bug 只在多人协作或刷新时才显形。
//
// ## 分层规则
//
// 从入口 BFS：层号 = 到入口的最短跳数；同层节点按 key 字典序纵向排开（y += 120）。
// 环与不可达节点都会被兜住：环里的节点在 BFS 里已经访问过就跳过；不可达节点放进
// 最后一层（它们在图校验里会被拒，但画布不该因为一份坏图就不渲染）。

/** COL_W 一层横向宽度、ROW_H 同层纵向间距（与 CSS 里节点尺寸配套）。 */
export var COL_W = 220;
export var ROW_H = 120;
export var START_X = 40;
export var START_Y = 40;

/**
 * layout 计算节点坐标。
 *
 * @param {Array}  nodes 节点数组（每项至少 {key, out}；v1 的 next/yes/no 也认）
 * @param {string} entry 入口 key
 * @param {object} existing 已有坐标 {key: {x,y}}；**这些键的位置不被覆盖**
 * @returns {object} {key: {x, y}}（含所有节点）
 */
export function layout(nodes, entry, existing) {
    var pos = {};
    var keep = existing || {};
    var list = nodes || [];

    var byKey = {};
    for (var i = 0; i < list.length; i++) {
        byKey[list[i].key] = list[i];
    }

    // 1) BFS 分层：层号来自最短跳数。
    var depth = {};
    var queue = [];
    if (entry && byKey[entry]) {
        depth[entry] = 0;
        queue.push(entry);
    }
    while (queue.length) {
        var cur = queue.shift();
        var targets = edgesOf(byKey[cur]);
        for (var t = 0; t < targets.length; t++) {
            var to = targets[t];
            if (!byKey[to] || depth[to] !== undefined) { continue; }
            depth[to] = depth[cur] + 1;
            queue.push(to);
        }
    }

    // 2) 不可达节点（含整张图没有入口的情况）放进最后一层。
    var maxDepth = 0;
    for (var k in depth) {
        if (depth[k] > maxDepth) { maxDepth = depth[k]; }
    }
    for (var j = 0; j < list.length; j++) {
        if (depth[list[j].key] === undefined) {
            depth[list[j].key] = maxDepth + 1;
        }
    }

    // 3) 按（层, key）排序后逐层摆位 —— 排序让结果确定，不依赖输入数组顺序。
    var keys = Object.keys(depth).sort(function (a, b) {
        if (depth[a] !== depth[b]) { return depth[a] - depth[b]; }
        return a < b ? -1 : (a > b ? 1 : 0);
    });
    var rowInLayer = {};
    for (var s = 0; s < keys.length; s++) {
        var key = keys[s];
        var layer = depth[key];
        var row = rowInLayer[layer] || 0;
        rowInLayer[layer] = row + 1;

        if (keep[key] && typeof keep[key].x === 'number' && typeof keep[key].y === 'number') {
            pos[key] = { x: keep[key].x, y: keep[key].y };
            continue;
        }
        pos[key] = { x: START_X + layer * COL_W, y: START_Y + row * ROW_H };
    }
    return pos;
}

/** edgesOf 一个节点的出边目标（v2 的 out 优先，v1 的扁平字段兜底）。 */
function edgesOf(node) {
    if (!node) { return []; }
    var out = [];
    if (node.out && typeof node.out === 'object') {
        Object.keys(node.out).forEach(function (k) {
            if (node.out[k]) { out.push(node.out[k]); }
        });
    }
    if (!out.length) {
        if (node.next) { out.push(node.next); }
        if (node.yes) { out.push(node.yes); }
        if (node.no) { out.push(node.no); }
    }
    return out;
}
