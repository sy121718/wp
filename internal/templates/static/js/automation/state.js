// automation/state.js — 画布的图模型与命令栈（纯数据，不碰 DOM）。
//
// ## 为什么把图模型单独一层
//
// 画布要做的事（拖位置、连边、插节点、删节点、撤销、保存）**全是对同一份图数据的改写**。
// 把这层挤进渲染或事件代码里，第一个后果就是「同一个动作有两份实现」：拖拽路径改一次、
// 菜单路径改一次，两者迟早不一致（老画布的「侧栏下拉改边」与「表单页改边」正是如此）。
//
// 这里只导出纯函数式操作：每个动作返回新状态，**不就地改入参**。撤销栈因此只需要存快照，
// 而不需要为每种操作写一个逆操作（写逆操作是 bug 的温床：删节点的逆操作要恢复它所有的边）。
//
// ## 出边形态与出口名表
//
// v2 图定义的出边是 `out: {出口名 → 目标 key}`（见 service/automation_definition.go 的
// nodeOutlets）。本文件不复制那张表：出口名由服务端经 meta.nodeTypes 注入，
// 前端只按「这个节点类型有哪些出口」渲染圆钮。

/** 出口名表缺省值（服务端没注入时的兜底，与服务端 nodeOutlets 一致）。 */
var FALLBACK_OUTLETS = {
    trigger: ['next'],
    delay: ['next'],
    email: ['next'],
    tag: ['next'],
    webhook: ['next'],
    filter: ['yes', 'no'],
    list_status_branch: ['not_in_list', 'active', 'non_active'],
    end: []
};

/** 没有出口的类型（`end`）：不画圆钮，也不允许连出。 */
var TERMINAL_TYPES = { end: true };

/**
 * createState 造一份画布状态。
 *
 * @param {object} opts
 *   opts.nodes     节点数组（每项 {key,type,params,out,x,y}）
 *   opts.entry     入口 key
 *   opts.nodeTypes 服务端注入的类型表 [{value,label,hint,outlets,disabled}]
 */
export function createState(opts) {
    var meta = opts || {};
    return {
        entry: meta.entry || 'n1',
        nodes: (meta.nodes || []).map(normalizeNode),
        nodeTypes: meta.nodeTypes || [],
        // 命令栈：past 是快照数组（含当前态之前的每一步），future 是重做栈。
        past: [],
        future: [],
        // 上一次 push 的时间戳：同类操作 300ms 内合并（拖拽会连续产生几十次 move）。
        lastStamp: 0,
        lastKind: ''
    };
}

/** normalizeNode 把服务端 / 存量数据规整成画布用的形状（不改入参）。 */
function normalizeNode(n) {
    var out = {};
    var src = n.out || {};
    // 服务端下发的是 v1 投影（next / yes / no）——画布内部一律用 out。
    Object.keys(src).forEach(function (k) { out[k] = src[k]; });
    if (!Object.keys(out).length) {
        if (n.next) { out.next = n.next; }
        if (n.yes) { out.yes = n.yes; }
        if (n.no) { out.no = n.no; }
    }
    return {
        key: String(n.key || ''),
        type: String(n.type || ''),
        params: shallow(n.params),
        out: out,
        x: typeof n.x === 'number' ? n.x : 0,
        y: typeof n.y === 'number' ? n.y : 0
    };
}

function shallow(v) {
    var dst = {};
    if (!v || typeof v !== 'object') { return dst; }
    Object.keys(v).forEach(function (k) { dst[k] = v[k]; });
    return dst;
}

/** outletsOf 该节点类型有哪些出口名（服务端注入优先，缺省回落到与后端一致的表）。 */
export function outletsOf(state, nodeType) {
    for (var i = 0; i < state.nodeTypes.length; i++) {
        var t = state.nodeTypes[i];
        if (t.value === nodeType && t.outlets && t.outlets.length) { return t.outlets; }
    }
    return FALLBACK_OUTLETS[nodeType] || [];
}

/** isTerminal 该类型是否没有出口。 */
export function isTerminal(nodeType) {
    return !!TERMINAL_TYPES[nodeType];
}

/** outletLabel 出口名在界面上的说法（与出口名本身同形，便于对照服务端报错）。 */
export function outletLabel(name) {
    return name || '';
}

/** nodeByKey 按 key 找节点（找不到返回 null）。 */
export function nodeByKey(state, key) {
    for (var i = 0; i < state.nodes.length; i++) {
        if (state.nodes[i].key === key) { return state.nodes[i]; }
    }
    return null;
}

/** snapshot 当前图的一份深拷贝（进撤销栈用）。 */
export function snapshot(state) {
    return {
        entry: state.entry,
        nodes: state.nodes.map(function (n) {
            return {
                key: n.key, type: n.type, params: shallow(n.params),
                out: shallow(n.out), x: n.x, y: n.y
            };
        })
    };
}

/**
 * pushHistory 把**当前**状态压进撤销栈，然后清空重做栈。
 *
 * kind 用来做 300ms 合并：拖拽会在一秒内产生几十次 move，每一次都进栈的话
 * 「撤销一次」只挪回 8px，用户要按几十次才能撤销一次拖动。
 */
function pushHistory(state, kind) {
    var now = Date.now();
    if (kind && kind === state.lastKind && now - state.lastStamp < 300) {
        state.lastStamp = now;
        return;
    }
    state.past.push(snapshot(state));
    if (state.past.length > 100) { state.past.shift(); }
    state.future = [];
    state.lastStamp = now;
    state.lastKind = kind || '';
}

function replaceNodes(state, nodes) {
    var next = Object.assign({}, state, { nodes: nodes });
    return next;
}

/** moveNode 移动节点到 (x, y)（坐标夹在内容层内）。 */
export function moveNode(state, key, x, y) {
    pushHistory(state, 'move');
    var next = state.nodes.map(function (n) {
        if (n.key !== key) { return n; }
        return Object.assign({}, n, {
            x: Math.max(0, Math.round(x)),
            y: Math.max(0, Math.round(y))
        });
    });
    return replaceNodes(state, next);
}

/**
 * addNode 在 at 处建一个节点；给了 fromOutlet 就顺手连上那条边。
 *
 * @returns {state, key} 新节点的 key（调用方要拿它去选中）
 */
export function addNode(state, type, at, fromOutlet) {
    pushHistory(state, 'add');
    var key = freeKey(state, type);
    var node = {
        key: key, type: type, params: defaultParams(type), out: {},
        x: Math.max(0, Math.round(at && at.x || 0)),
        y: Math.max(0, Math.round(at && at.y || 0))
    };
    var nodes = state.nodes.concat([node]);
    var next = replaceNodes(state, nodes);
    if (fromOutlet && fromOutlet.from && fromOutlet.outlet) {
        next = connect(next, fromOutlet.from, fromOutlet.outlet, key);
    }
    return { state: next, key: key };
}

/** defaultParams 新节点的参数初值（够过校验的最小合法值）。 */
function defaultParams(type) {
    if (type === 'delay') { return { minutes: 60 }; }
    if (type === 'filter') { return { mode: 'all', conditions: [] }; }
    if (type === 'tag') { return { add: [] }; }
    if (type === 'ab_test') {
        return { variants: [{ id: 'v1', name: 'A', weight: 50 }, { id: 'v2', name: 'B', weight: 50 }] };
    }
    return {};
}

/** freeKey 生成一个没被占用的节点 key（与后端 freeKey 同形：n1 / n2 …）。 */
function freeKey(state, type) {
    var used = {};
    for (var i = 0; i < state.nodes.length; i++) { used[state.nodes[i].key] = true; }
    var base = type === 'end' ? 'end' : 'n';
    for (var n = 1; n < 1000; n++) {
        var candidate = base === 'end' ? (n === 1 ? 'end' : 'end' + n) : base + n;
        if (!used[candidate]) { return candidate; }
    }
    return base + Date.now();
}

/**
 * removeNode 删除节点**与它所有的边**。
 *
 * 只删节点不删边，会留下指向不存在节点的出边 —— 服务端校验会拒绝整张图，
 * 而用户看到的是「删了一个节点，保存却报某条边不存在」，那句话里提到的节点已经不在了。
 */
export function removeNode(state, key) {
    pushHistory(state, 'remove');
    var nodes = state.nodes
        .filter(function (n) { return n.key !== key; })
        .map(function (n) {
            var out = {};
            Object.keys(n.out).forEach(function (k) {
                if (n.out[k] !== key) { out[k] = n.out[k]; }
            });
            return Object.assign({}, n, { out: out });
        });
    return replaceNodes(state, nodes);
}

/**
 * connect 把 from 的某个出口连到 to。
 *
 * **一个出口只允许一个目标**（图是确定性的）：重连时直接覆盖，而不是追加。
 * 同一个目标被两条出口指向是合法的（两条分支汇合），这里不管。
 */
export function connect(state, from, outlet, to) {
    if (!from || !to || from === to) { return state; }
    var changed = false;
    var nodes = state.nodes.map(function (n) {
        if (n.key !== from) { return n; }
        changed = true;
        var out = shallow(n.out);
        out[outlet] = to;
        return Object.assign({}, n, { out: out });
    });
    if (!changed) { return state; }
    return replaceNodes(state, nodes);
}

/** disconnect 断开某个出口（该出口变回空）。 */
export function disconnect(state, from, outlet) {
    var changed = false;
    var nodes = state.nodes.map(function (n) {
        if (n.key !== from || !n.out[outlet]) { return n; }
        changed = true;
        var out = shallow(n.out);
        delete out[outlet];
        return Object.assign({}, n, { out: out });
    });
    if (!changed) { return state; }
    return replaceNodes(state, nodes);
}

/**
 * insertOnEdge 在一条已存在的边中间插入一个节点：source → new → target。
 *
 * 三段缺一段都会让图非法（新节点没有入边不会被拒绝，但它的出边必须指向 target，
 * 否则那一段分支的人就走到这里没了）。
 */
export function insertOnEdge(state, from, outlet, type) {
    var target = null;
    var src = nodeByKey(state, from);
    if (src) { target = src.out[outlet] || null; }
    if (!target) { return { state: state, key: '' }; }
    var mid = null;
    if (src) { mid = { x: (src.x || 0) + 220, y: src.y || 0 }; }
    var added = addNode(state, type, mid || { x: 0, y: 0 }, null);
    var next = connect(added.state, from, outlet, added.key);
    if (outletsOf(next, type).length) {
        next = connect(next, added.key, outletsOf(next, type)[0], target);
    }
    return { state: next, key: added.key };
}

/** setParams 改节点参数（侧栏属性面板用）。 */
export function setParams(state, key, params) {
    pushHistory(state, 'params');
    var nodes = state.nodes.map(function (n) {
        if (n.key !== key) { return n; }
        return Object.assign({}, n, { params: shallow(params) });
    });
    return replaceNodes(state, nodes);
}

/** setEntry 改入口节点。 */
export function setEntry(state, key) {
    pushHistory(state, 'entry');
    return Object.assign({}, state, { entry: key });
}

/** undo 撤销一步（弹栈 → 恢复快照，当前态进重做栈）。 */
export function undo(state) {
    if (!state.past.length) { return state; }
    var past = state.past.slice();
    var prev = past.pop();
    var future = state.future.concat([snapshot(state)]);
    var next = Object.assign({}, state, { past: past, future: future, lastKind: '', lastStamp: 0 });
    next.nodes = prev.nodes.map(normalizeNode);
    next.entry = prev.entry;
    return next;
}

/** redo 重做一步。 */
export function redo(state) {
    if (!state.future.length) { return state; }
    var future = state.future.slice();
    var nextSnap = future.pop();
    var past = state.past.concat([snapshot(state)]);
    var next = Object.assign({}, state, { past: past, future: future, lastKind: '', lastStamp: 0 });
    next.nodes = nextSnap.nodes.map(normalizeNode);
    next.entry = nextSnap.entry;
    return next;
}

/** canUndo / canRedo 给按钮的禁用态用。 */
export function canUndo(state) { return state.past.length > 0; }
export function canRedo(state) { return state.future.length > 0; }

/**
 * toDefinition 导出提交给服务端的定义（v2 形状）。
 *
 * 空出口不入 map：`{"next": ""}` 与「没有 next」在下游是同一种意思，进去只会让
 * 服务端多一次空串判断。
 */
export function toDefinition(state) {
    var nodes = state.nodes.map(function (n) {
        var out = {};
        Object.keys(n.out).forEach(function (k) {
            if (n.out[k]) { out[k] = n.out[k]; }
        });
        var item = { key: n.key, type: n.type, out: out, x: n.x, y: n.y };
        if (n.params && Object.keys(n.params).length) { item.params = n.params; }
        return item;
    });
    return { schema: 2, entry: state.entry, nodes: nodes };
}

/** dirtySinceDefinition 判断当前图与一份定义是否等价（决定「有未保存改动」提示）。 */
export function sameAsDefinition(state, def) {
    if (!def || !def.nodes) { return false; }
    var a = JSON.stringify(toDefinition(state));
    var b = JSON.stringify({ schema: 2, entry: def.entry, nodes: def.nodes });
    return a === b;
}
