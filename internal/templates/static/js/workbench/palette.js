import { alignedRepeaters, paletteSpec } from './generated-contracts.js';

// workbench/palette.js — 组件库清单与「插入默认内容」（无 DOM 依赖的纯数据/纯函数模块）。
//
// 单一真源（审计 REG-005）：组件库条目的**数据**（显示名 / 说明 / 分组 / 插入时的
// 默认 Props）全部来自组件 Go 声明（core.AtomSpec 的 DisplayName / Hint /
// PaletteCategory / DefaultProps，或自定义组件的 Palette() 方法），经
// `go run ./cmd/workbench-contracts` 生成 ./generated-contracts.js 注入本模块。
// **新增组件只改 Go**：生成物刷新后即出现在组件库，不需要动本文件。
//
// 本文件只保留两类**自动化不了**的人工信息：
//   1. 排序表（paletteGroupOrder 分组顺序与标题、paletteTypeOrder 组内顺序）——
//      排序是编辑体验决策，Go 侧声明里没有也不该有；
//   2. 结构型组件的默认子树（DEFAULT_CONTENT.children）—— 它是节点树而非 Props，
//      插入时要按 allocId 分配节点 ID，属于前端构造逻辑。
//      注意：子树里**不含任何 Props 默认值**，那些走 defaultPropsOf（Go 侧真源）。
//
// 图标映射：当前组件库条目只渲染显示名 + 说明（见 methods/canvas.js 的 makeItem），
// 不存在图标表 —— 所以这里不预先建一张没人用的表。将来要加图标时在本文件加一张
// type → 图标名 的映射（那是真正需要人工的部分），而不是回到手写条目。
//
// 为什么独立成文件：组件库数据与「插入时补齐的最小内容」是纯数据，抽出来后可被
// 浏览器 ES module 消费，也能被 node 直接 import 求值 —— 「组件库条目插入后必须
// 可编译」这条不变量因此可以被自动验证（internal/builder/defaults_contract_test.go
// 用 node 求值本模块，再交给 Go 侧 Validate 校验）。
//
// 与 Go 侧的关系：Validate 规则是唯一权威（本文件不改任何校验规则）。
// 默认 Props / 默认子树只负责「插入时不留空壳」，让默认节点天然满足最小校验。

/** 深拷贝（palette 数据全部为 JSON 可序列化值，用 JSON 往返最省事）。 */
function deepClone(v) {
    return v === undefined ? undefined : JSON.parse(JSON.stringify(v));
}

/** 分组顺序与标题（人工：排序是编辑体验决策，Go 侧声明只有分组键）。 */
export const paletteGroupOrder = [
    { key: 'basic', title: '基础组件' }
];

/**
 * 组内显示顺序（人工）。未列入本表的组件（新增组件还没来得及排序）按类型名
 * 字典序追加到末尾 —— 保证「新增组件无需改 JS 即出现在组件库」，
 * 排序只是可选的体验微调。
 */
export const paletteTypeOrder = [
    'core.container', 'core.heading', 'core.text', 'core.button', 'core.image',
    'core.gallery', 'core.divider', 'core.shapedivider', 'core.spacer', 'core.slider',
    'core.list', 'core.infobox', 'core.social_buttons', 'core.video', 'core.nav',
    'core.languages', 'core.tabs', 'core.accordion', 'core.marquee', 'core.counter',
    'core.table', 'core.card', 'core.cardstack', 'core.faq', 'core.quote',
    'core.countdown', 'core.icon', 'core.badge', 'core.breadcrumb', 'core.progress',
    'core.loader', 'core.rating', 'core.form', 'core.product', 'core.productCard',
    'core.productList', 'core.productSelector', 'core.addToCart', 'core.cartIcon',
    'core.orderList', 'core.searchResults', 'core.userForms'
];

/** 组件库全部类型：排序表在前的先出，其余按字典序追加（新组件自动可见）。 */
export function paletteTypes() {
    var items = paletteSpec.items || {};
    var listed = paletteTypeOrder.filter(function (t) { return !!items[t]; });
    var rest = Object.keys(items).filter(function (t) { return listed.indexOf(t) < 0; });
    rest.sort();
    return listed.concat(rest);
}

/** paletteItemFor 把 Go 侧元数据转成组件库条目（字段名与消费方对齐）。 */
function paletteItemFor(type) {
    var meta = (paletteSpec.items || {})[type];
    if (!meta) return null;
    return {
        type: type,
        label: meta.displayName,
        hint: meta.hint || '',
        props: deepClone(meta.defaultProps) || {}
    };
}

/** 组件库条目（含可编译的默认 Props，来源见文件头）。 */
export const paletteItems = paletteTypes().map(paletteItemFor).filter(function (item) { return !!item; });

/** 组件库分组：分组顺序来自 paletteGroupOrder，组内顺序沿用 paletteItems 的顺序。 */
export const paletteGroups = paletteGroupOrder.map(function (group) {
    var items = paletteSpec.items || {};
    return {
        key: group.key,
        title: group.title,
        types: paletteItems.filter(function (item) {
            return items[item.type] && items[item.type].category === group.key;
        }).map(function (item) { return item.type; })
    };
}).filter(function (group) { return group.types.length > 0; });

/** defaultPropsOf 组件的默认 Props（唯一来源：Go 侧组件声明）。 */
export function defaultPropsOf(type) {
    var meta = (paletteSpec.items || {})[type];
    return (meta && meta.defaultProps) || {};
}

/**
 * DEFAULT_CONTENT 组件库默认内容：插入时自动补齐，避免「空壳组件」拖入即编译失败。
 *
 * 本表**只放结构型组件的默认子树**（A 类）。数组型与单值 Props 的兜底不在这里 ——
 * 它们来自 Go 侧组件声明的默认 Props（defaultPropsOf），本文件不再留第二份副本。
 *   A 类 结构型 children —— tabs 面板 / accordion 折叠项 / slider slide / marquee 内容，
 *      没有子节点直接编译失败，故 children 恒生成。
 *   B/C 类（数组型 props / 单值 props）—— 由 Go 侧 DefaultProps 提供，见 defaultPropsOf。
 *
 * 字段语义：
 *   children —— 默认子树模板（不含 id，插入时按 allocId 分配；深拷贝后实例化）。
 */
export const DEFAULT_CONTENT = {
    // ---- A 类：结构型（children 必需）----
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
    'core.cardstack': {
        // 卡片可选子节点：有子节点即内容卡，删空后自动退回 1~N 数字占位卡。
        // 默认给两张真内容卡，插入即可看出几何效果与内容排版。
        children: [
            {
                type: 'core.container', name: '卡片一',
                props: { tag: 'div', layout: { engine: 'flex', flex: { direction: 'column', gap: '8px' } } },
                children: [
                    { type: 'core.heading', name: '卡片标题', props: { text: '第一张卡', tag: 'h3' } },
                    { type: 'core.text', name: '卡片正文', props: { mode: 'plaintext', plainTag: 'p', text: '点击卡片可放大到屏幕中央，点遮罩收起。' } }
                ]
            },
            {
                type: 'core.container', name: '卡片二',
                props: { tag: 'div', layout: { engine: 'flex', flex: { direction: 'column', gap: '8px' } } },
                children: [
                    { type: 'core.heading', name: '卡片标题', props: { text: '第二张卡', tag: 'h3' } },
                    { type: 'core.text', name: '卡片正文', props: { mode: 'plaintext', plainTag: 'p', text: '悬停看展开，改成滚动堆叠则随滚动逐张粘住。' } }
                ]
            }
        ]
    },
    'core.marquee': {
        // 跑马灯会把子节点渲染两份（无缝循环），故默认内容用无状态的行内文本，
        // 不用容器：避免同一 DOM id 出现两次。
        children: [{ type: 'core.text', name: '跑马灯内容', props: { mode: 'plaintext', plainTag: 'span', text: '跑马灯内容，点击编辑。' } }]
    }
};

/** 空值判定：undefined / null / 空串 / 空数组 / 空对象 视为「没内容」。 */
function isEmptyValue(v) {
    if (v === undefined || v === null || v === '') return true;
    if (Array.isArray(v)) return v.length === 0;
    if (typeof v === 'object') return Object.keys(v).length === 0;
    return false;
}

/** mergeMissing 键级兜底合并：目标缺失的键才用兜底值填充，已有值保留。 */
function mergeMissing(target, fallback) {
    var out = (target && typeof target === 'object' && !Array.isArray(target)) ? target : {};
    Object.keys(fallback || {}).forEach(function (k) {
        if (isEmptyValue(out[k])) out[k] = deepClone(fallback[k]);
    });
    return out;
}

/** instantiate 把默认子树模板实例化：深拷贝 + 递归分配节点 ID。 */
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

/**
 * buildInsertNode 由组件库条目构造「插入即合法」的节点。
 *
 * item     —— paletteItems 条目（或插件组件条目）：{ type, label, props }。
 * allocId  —— ID 分配器 (base) => id，调用方保证与现有文档及本批节点都不冲突。
 * 返回     —— { id, type, name, props, children? }；item 非法时返回 null。
 *
 * 注意：本函数只做「默认内容补齐」，不参与任何校验；生成结果必须能被 Go 侧
 * Validate 接受，由 internal/builder 的契约测试守住。
 */
export function buildInsertNode(item, allocId) {
    if (!item || !item.type || typeof allocId !== 'function') return null;
    var def = DEFAULT_CONTENT[item.type] || {};
    var props = deepClone(item.props) || {};
    props = mergeMissing(props, defaultPropsOf(item.type));
    // 结构型：内容数组与默认子节点数量对齐（多则裁剪，少则用兜底项补齐）。
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

/** alignKeyOf 结构型组件的内容数组 prop 键（tabs→tabs，accordion→items；非结构型返回 ''）。 */
export function alignKeyOf(type) {
    return alignedRepeaters[type] ? alignedRepeaters[type].alignKey : '';
}

/**
 * buildDefaultChild 结构型组件的默认子节点：取 DEFAULT_CONTENT[type].children 的第
 * index 个模板实例化（模板多于一个时按索引循环取用），ID 由 allocId 分配。
 *
 * 与 buildInsertNode 同源（同一个 DEFAULT_CONTENT / 同一个 instantiate）——
 * 检查器「加标签」补出来的面板与组件库拖入时的默认面板逐字节一致，不存在第二份默认内容。
 */
export function buildDefaultChild(type, index, allocId) {
    var tpls = (DEFAULT_CONTENT[type] || {}).children || [];
    if (!tpls.length || typeof allocId !== 'function') return null;
    var at = Math.abs(Number(index) || 0) % tpls.length;
    return instantiate(tpls[at], allocId);
}

/**
 * buildDefaultAlignEntry 结构型组件内容数组的默认条目：Go 侧默认 Props 里该数组的
 * 第一条深拷贝，并按序号覆写文案字段（页签2 / 折叠项2…）。
 */
export function buildDefaultAlignEntry(type, index) {
    var key = alignKeyOf(type);
    if (!key) return null;
    var fill = defaultPropsOf(type)[key] || [];
    if (!fill.length) return null;
    var at = Math.abs(Number(index) || 0);
    var entry = deepClone(fill[at % fill.length]);
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return null;
    var conf = alignedRepeaters[type];
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
