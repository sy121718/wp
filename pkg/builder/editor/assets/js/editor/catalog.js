// editor/catalog.js — 组件库算法内核（无 DOM 依赖）。
//
// 组件库数据（paletteSpec / alignedRepeaters / 扩展默认内容）由应用在启动前
// 通过 configureCatalog 显式注入：生成契约只由完整应用注册产生一份，工具不内置
// 任何应用的组件目录、默认内容或业务语义。
//
// 本文件保留自动化不了的通用部分：排序、默认子树实例化、内容数组 ↔ children 对齐。
// 默认子树只放通用结构型组件；应用扩展通过 extendDefaultContent 追加。

let contracts = { items: {}, alignedRepeaters: {} };

/** 深拷贝（目录数据全部为 JSON 可序列化值）。 */
function deepClone(v) {
    return v === undefined ? undefined : JSON.parse(JSON.stringify(v));
}

/** paletteGroupOrder 分组顺序与标题（人工：编辑体验决策）。 */
export const paletteGroupOrder = [
    { key: 'basic', title: '基础组件' }
];

/** paletteTypeOrder 组内显示顺序（人工）；未列出的按类型名字典序追加。 */
export const paletteTypeOrder = [
    'core.container', 'core.heading', 'core.text', 'core.button', 'core.image',
    'core.gallery', 'core.divider', 'core.shapedivider', 'core.spacer', 'core.slider',
    'core.list', 'core.infobox', 'core.social_buttons', 'core.video', 'core.nav',
    'core.languages', 'core.tabs', 'core.accordion', 'core.marquee', 'core.counter',
    'core.table', 'core.card', 'core.faq', 'core.quote',
    'core.countdown', 'core.icon', 'core.badge', 'core.breadcrumb', 'core.progress',
    'core.loader', 'core.rating', 'core.form'
];

export let paletteItems = [];
export let paletteGroups = [];

/** configureCatalog 注入完整应用注册结果（生成契约的消费点，唯一来源在应用）。 */
export function configureCatalog(next) {
    next = next || {};
    contracts = {
        items: next.items || {},
        alignedRepeaters: next.alignedRepeaters || {}
    };
    recompute();
}

function recompute() {
    var listed = paletteTypeOrder.filter(function (t) { return !!contracts.items[t]; });
    var rest = Object.keys(contracts.items).filter(function (t) { return listed.indexOf(t) < 0; });
    rest.sort();
    paletteItems = listed.concat(rest).map(function (type) {
        var meta = contracts.items[type];
        return {
            type: type,
            label: meta.displayName,
            hint: meta.hint || '',
            requiredEntityTypes: (meta.requiredEntityTypes || []).slice(),
            props: deepClone(meta.defaultProps) || {}
        };
    });
    paletteGroups = paletteGroupOrder.map(function (group) {
        return {
            key: group.key,
            title: group.title,
            types: paletteItems.filter(function (item) {
                return contracts.items[item.type] && contracts.items[item.type].category === group.key;
            }).map(function (item) { return item.type; })
        };
    }).filter(function (group) { return group.types.length > 0; });
}

/** paletteSpecItems 返回应用注入的原始组件元数据（只读消费）。 */
export function paletteSpecItems() {
    return contracts.items;
}

/** paletteTypes 组件库全部类型。 */
export function paletteTypes() {
    return paletteItems.map(function (item) { return item.type; });
}

/** defaultPropsOf 组件的默认 Props（唯一来源：应用注入的组件声明）。 */
export function defaultPropsOf(type) {
    var meta = contracts.items[type];
    return (meta && meta.defaultProps) || {};
}

/** 默认插入的上下文限制来自组件声明；不推断列表或显式选品的业务语义。 */
export function paletteUnavailableReason(item, context) {
    var required = item && item.requiredEntityTypes || [];
    if (!required.length) return '';
    var target = context && context.target && context.target.type || context && context.saveBase;
    if ((target === 'template' || target === 'instance') && !context.noEntity && context.entityId &&
        required.every(function (type) { return type === context.entityType; })) return '';
    return '需要对应内容模板的实体上下文';
}

/** DEFAULT_CONTENT 通用结构型组件的默认子树（不含应用扩展）。 */
export const DEFAULT_CONTENT = {
    'core.tabs': {
        children: [{
            type: 'core.container', name: '页签面板',
            props: { tag: 'div', layout: { engine: 'flex', flex: { direction: 'column', gap: '8px' } }, box: { padding: { desktop: '12px' } } },
            children: [{ type: 'core.text', name: '面板文本', props: { mode: 'plaintext', plainTag: 'p', text: '页签面板内容，点击编辑。' } }]
        }]
    },
    'core.accordion': {
        children: [{
            type: 'core.container', name: '折叠项内容',
            props: { tag: 'div', layout: { engine: 'flex', flex: { direction: 'column', gap: '8px' } } },
            children: [{ type: 'core.text', name: '折叠项文本', props: { mode: 'plaintext', plainTag: 'p', text: '折叠项内容，点击编辑。' } }]
        }]
    },
    'core.slider': {
        children: [{
            type: 'core.container', name: 'Slide',
            props: { tag: 'div', layout: { engine: 'flex', flex: { direction: 'column', gap: '12px' } }, box: { padding: { desktop: '24px' } } },
            children: [{ type: 'core.text', name: 'Slide 文本', props: { mode: 'plaintext', plainTag: 'p', text: '第一屏内容，点击编辑。' } }]
        }]
    },
    'core.marquee': {
        children: [{ type: 'core.text', name: '跑马灯内容', props: { mode: 'plaintext', plainTag: 'span', text: '跑马灯内容，点击编辑。' } }]
    }
};

/** extendDefaultContent 应用扩展追加结构型默认内容（同名不覆盖工具默认）。 */
export function extendDefaultContent(map) {
    Object.keys(map || {}).forEach(function (type) {
        if (!DEFAULT_CONTENT[type]) DEFAULT_CONTENT[type] = map[type];
    });
}

/** 空值判定。 */
function isEmptyValue(v) {
    if (v === undefined || v === null || v === '') return true;
    if (Array.isArray(v)) return v.length === 0;
    if (typeof v === 'object') return Object.keys(v).length === 0;
    return false;
}

/** 键级兜底合并：目标缺失的键才用兜底值。 */
function mergeMissing(target, fallback) {
    var out = (target && typeof target === 'object' && !Array.isArray(target)) ? target : {};
    Object.keys(fallback || {}).forEach(function (k) {
        if (isEmptyValue(out[k])) out[k] = deepClone(fallback[k]);
    });
    return out;
}

/** instantiate 默认子树实例化：深拷贝 + 递归分配节点 ID。 */
function instantiate(template, allocId) {
    var node = {
        id: allocId(String(template.type || 'node').split('.').pop()),
        type: template.type,
        name: template.name || template.type,
        props: deepClone(template.props) || {}
    };
    if (template.children && template.children.length) {
        node.children = template.children.map(function (child) { return instantiate(child, allocId); });
    }
    return node;
}

/** buildInsertNode 由组件库条目构造「插入即合法」的节点。 */
export function buildInsertNode(item, allocId) {
    if (!item || !item.type || typeof allocId !== 'function') return null;
    var def = DEFAULT_CONTENT[item.type] || {};
    var props = deepClone(item.props) || {};
    props = mergeMissing(props, defaultPropsOf(item.type));
    var alignKey = alignKeyOf(item.type);
    if (alignKey && def.children && def.children.length) {
        var list = Array.isArray(props[alignKey]) ? props[alignKey].slice(0, def.children.length) : [];
        var fill = defaultPropsOf(item.type)[alignKey] || [];
        while (list.length < def.children.length) {
            list.push(deepClone(fill[list.length % Math.max(fill.length, 1)] || {}));
        }
        props[alignKey] = list;
    }
    var node = {
        id: allocId(String(item.type).split('.').pop() || 'node'),
        type: item.type,
        name: item.label || item.type,
        props: props
    };
    if (def.children && def.children.length) {
        node.children = def.children.map(function (tpl) { return instantiate(tpl, allocId); });
    }
    return node;
}

/** alignedRepeatersOf 返回应用注入的结构型对齐声明（只读消费）。 */
export function alignedRepeatersOf() {
    return contracts.alignedRepeaters;
}

/** alignKeyOf 结构型组件的内容数组 prop 键（非结构型返回 ''）。 */
export function alignKeyOf(type) {
    return contracts.alignedRepeaters[type] ? contracts.alignedRepeaters[type].alignKey : '';
}

/** buildDefaultChild 结构型组件的默认子节点。 */
export function buildDefaultChild(type, index, allocId) {
    var tpls = (DEFAULT_CONTENT[type] || {}).children || [];
    if (!tpls.length || typeof allocId !== 'function') return null;
    var at = Math.abs(Number(index) || 0) % tpls.length;
    return instantiate(tpls[at], allocId);
}

/** buildDefaultAlignEntry 结构型组件内容数组的默认条目。 */
export function buildDefaultAlignEntry(type, index) {
    var key = alignKeyOf(type);
    if (!key) return null;
    var fill = defaultPropsOf(type)[key] || [];
    if (!fill.length) return null;
    var at = Math.abs(Number(index) || 0);
    var entry = deepClone(fill[at % fill.length]);
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return null;
    var conf = contracts.alignedRepeaters[type];
    if (conf && conf.field) entry[conf.field] = conf.noun + (at + 1);
    return entry;
}
/**
 * alignMutation 结构型组件「内容数组 ↔ children」的双向同步（纯函数，无 DOM）。
 *
 * 为什么放在这里：tabs/accordion 的 Validate 要求「标签数 = 面板数」，而检查器过去只改
 * props 数组、不动 children，于是「加/删标签」必然把文档推入编译失败状态。把同步逻辑
 * 写成无 DOM 纯函数后，浏览器（methods/controls/repeater.js）与契约测试（node 求值本模块）
 * 走的是同一份实现，不会出现「测试里一份、浏览器里另一份」的漂移。
 *
 * 参数：
 *   type     'core.tabs' | 'core.accordion'（其他类型返回 null）
 *   list     当前内容数组（props.tabs / props.items）
 *   children 当前子节点数组（node.children）
 *   action   {op:'add'} | {op:'remove',index} | {op:'move',index,to}
 *   allocId  ID 分配器（仅 add 需要；与 canvas.js 的 makeIdAllocator 同语义）
 * 返回：
 *   { key, list, children } 新的数组（不修改入参）；非法操作返回 null。
 *
 * 不变式：在 list 与 children 长度一致的前提下，任何 add/remove/move 后两者仍一致。
 */
export function alignMutation(type, list, children, action, allocId) {
    var key = alignKeyOf(type);
    if (!key) return null;
    var nextList = Array.isArray(list) ? list.slice() : [];
    var nextKids = Array.isArray(children) ? children.slice() : [];
    var op = (action && action.op) || '';
    var idx = Number(action && action.index);
    if (!isFinite(idx)) idx = 0;
    if (op === 'add') {
        // 省略 index 即追加到末尾（检查器「+ 添加」按钮的行为）。
        var at = (action && action.index === undefined) ? nextList.length : Math.min(Math.max(idx, 0), nextList.length);
        var entry = buildDefaultAlignEntry(type, at);
        var child = buildDefaultChild(type, at, allocId);
        if (!entry || !child) return null;
        nextList.splice(at, 0, entry);
        nextKids.splice(at, 0, child);
    } else if (op === 'remove') {
        if (idx < 0 || idx >= nextList.length) return null;
        nextList.splice(idx, 1);
        // 子节点数不足（历史脏数据）时只删标签：不误删别的位置的面板。
        if (idx < nextKids.length) nextKids.splice(idx, 1);
    } else if (op === 'move') {
        var to = Number(action && action.to);
        if (!(idx >= 0 && idx < nextList.length) || !(to >= 0 && to < nextList.length) || idx === to) return null;
        var moved = nextList.splice(idx, 1)[0];
        nextList.splice(to, 0, moved);
        if (idx < nextKids.length) {
            var movedKid = nextKids.splice(idx, 1)[0];
            if (movedKid) nextKids.splice(Math.min(to, nextKids.length), 0, movedKid);
        }
    } else {
        return null;
    }
    return { key: key, list: nextList, children: nextKids };
}

/**
 * alignFromChildren 结构型组件「children → 内容数组」方向的对齐（纯函数，无 DOM）。
 *
 * 与 alignMutation 对称，构成两条互补方向：
 *   alignMutation      —— 检查器方向：改标签数组（加/删/调序）→ 跟着改 children；
 *   alignFromChildren  —— 画布方向：画布直接改 children（拖入 / 删除 / 重排）→ 跟着改数组。
 * 两条方向共用同一份默认 Props（Go 侧声明）与 buildDefaultAlignEntry，新标签/标题的文案
 * 只有一个来源（「页签N / 折叠项N」），不存在第二份默认内容。
 *
 * 为什么需要它：tabs/accordion 的校验是「标签数 = 面板数」（tabs.go:66 / accordion.go:68），
 * 而画布上的删除面板、拖拽重排、拖入新面板、复制粘贴面板都只动 node.children，
 * 过去不同步 props 数组 —— 一删面板就「标签数与面板数不一致」编译失败。
 *
 * 参数：
 *   type     结构型组件类型（'core.tabs' | 'core.accordion'，其他类型返回 null）
 *   list     变更前的 props 内容数组（props.tabs / props.items）
 *   children 变更后的 children 数组（画布已经落地的状态）
 *   action   {op:'insert', index} | {op:'remove', index} | {op:'move', index, to}
 *            index / to 均为 children 数组下标
 * 返回：
 *   { key, list } —— 新的内容数组（不修改入参）；
 *   null          —— 本次不同步，调用方保持 props 原样。
 *
 * 不需要 allocId：本方向不新建子节点（子节点由画布 / 组件库负责构造），
 * 只按序号生成标签 / 标题文案。
 *
 * 安全边界（保守策略）：只在「变更前 list 与 children 数量一致」时才同步 ——
 *   insert：变更后 children 比 list 多 1
 *   remove：变更后 children 比 list 少 1
 *   move  ：变更前后数量相等
 * 数量本就不符（历史脏数据 / 旧版本遗留）时一律返回 null 且不动 props：猜一个位置
 * 去改只会把「标签数 ≠ 面板数」换成另一种错配，正确做法是让检查器（repeater.js 的
 * alignedRepeater）继续红字提示，由用户用「+ 添加 / ✕ 删除」把数量修齐。
 */
export function alignFromChildren(type, list, children, action) {
    var key = alignKeyOf(type);
    if (!key) return null;
    var cur = Array.isArray(list) ? list : [];
    var kids = Array.isArray(children) ? children : [];
    var op = (action && action.op) || '';
    var idx = Number(action && action.index);
    if (!isFinite(idx)) return null;
    if (op === 'insert') {
        // 变更前数量一致 → 现在 children 恰好多一个，新子节点的下标即 index。
        if (cur.length !== kids.length - 1) return null;
        if (idx < 0 || idx >= kids.length) return null; // 越界下标不猜，宁可不改
        var at = idx;
        var entry = buildDefaultAlignEntry(type, at);
        if (!entry) return null;
        var added = cur.slice();
        added.splice(Math.min(at, added.length), 0, entry);
        return { key: key, list: added };
    }
    if (op === 'remove') {
        // 变更前数量一致 → 现在 children 恰好少一个，被删子节点原来就在 index。
        if (cur.length !== kids.length + 1) return null;
        if (idx < 0 || idx >= cur.length) return null;
        var removed = cur.slice();
        removed.splice(idx, 1);
        return { key: key, list: removed };
    }
    if (op === 'move') {
        if (cur.length !== kids.length) return null;
        var to = Number(action && action.to);
        if (!(idx >= 0 && idx < cur.length) || !(to >= 0 && to < cur.length) || idx === to) return null;
        var moved = cur.slice();
        var item = moved.splice(idx, 1)[0];
        moved.splice(to, 0, item);
        return { key: key, list: moved };
    }
    return null;
}
