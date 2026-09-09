// workbench/palette.js — 组件库清单与「插入默认内容」（无 DOM 依赖的纯数据/纯函数模块）。
//
// 为什么独立成文件：组件库数据与「插入时补齐的最小内容」是纯数据，抽出来后可被
// 浏览器 ES module 消费，也能被 node 直接 import 求值 —— 「组件库条目插入后必须
// 可编译」这条不变量因此可以被自动验证（internal/builder/defaults_contract_test.go
// 用 node 求值本模块，再交给 Go 侧 Validate 校验）。
//
// 与 Go 侧的关系：Validate 规则是唯一权威（本文件不改任何校验规则）。
// DEFAULT_CONTENT 只负责「插入时不留空壳」，让默认节点天然满足最小校验。

/** 深拷贝（palette 数据全部为 JSON 可序列化值，用 JSON 往返最省事）。 */
function deepClone(v) {
    return v === undefined ? undefined : JSON.parse(JSON.stringify(v));
}

/** 组件库：仅提供可直接通过 AST 校验的默认节点。 */
export const paletteItems = [
    { type: 'core.container', label: '容器', hint: '布局容器', props: { tag: 'section', layout: { engine: 'flex', flex: { direction: 'column', gap: '16px' } }, box: { padding: { desktop: '32px' } } } },
    { type: 'core.heading', label: '标题', hint: '文字标题', props: { text: '新标题', tag: 'h2' } },
    { type: 'core.text', label: '文本', hint: '正文段落', props: { mode: 'plaintext', plainTag: 'p', text: '在这里输入正文内容。' } },
    { type: 'core.button', label: '按钮', hint: '行动按钮', props: { text: '了解更多', action: 'internal', value: '/' } },
    { type: 'core.image', label: '图片', hint: '外部图片', props: { src: 'https://placehold.co/1200x800/png', alt: '图片占位符', objectFit: 'cover', width: '100%' } },
    { type: 'core.gallery', label: '图集', hint: '图片网格 / 轮播', props: { mode: 'grid', items: [{ url: 'https://placehold.co/1200x800/png', alt: '图集占位图' }], grid: { columns: { desktop: 3 } }, aspectRatio: '16:9', objectFit: 'cover', radius: '8px' } },
    { type: 'core.divider', label: '分隔线', hint: '内容分隔', props: { style: 'solid', weight: '1px' } },
    { type: 'core.spacer', label: '间隔', hint: '留白空间', props: { height: { desktop: '32px' } } },
    { type: 'core.slider', label: '轮播', hint: '多屏滑动（可嵌套）', props: { perView: { desktop: 1 }, autoplay: 0, showArrows: true, showDots: true, gap: '16px' } },
    { type: 'core.list', label: '列表', hint: '图标/序号/圆点列表', props: { style: 'icon', items: [{ icon: 'check', text: '列表项内容' }] } },
    { type: 'core.infobox', label: '信息框', hint: '图标+标题+文本', props: { icon: 'shield', title: '信息框标题', text: '一句话描述你的服务或卖点。', align: 'center' } },
    { type: 'core.social_buttons', label: '社交图标', hint: '社交平台图标组', props: { color: 'brand', size: '40px', shape: 'circle', items: [{ platform: 'facebook', url: 'https://facebook.com' }, { platform: 'x', url: 'https://x.com' }, { platform: 'instagram', url: 'https://instagram.com' }] } },
    { type: 'core.video', label: '视频', hint: '外链嵌入/本地 MP4', props: { url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ', controls: true, ratio: '16:9' } },
    { type: 'core.nav', label: '导航菜单', hint: '站点菜单（支持二级）', props: { items: [{ label: '首页', url: '/' }, { label: '产品', url: '/shop', children: [{ label: '一次性', url: '/shop/disposable' }, { label: '换弹', url: '/shop/pods' }] }, { label: '关于我们', url: '/about' }], orientation: 'horizontal', gap: '24px', color: '#3B3C40', hoverColor: '#D93425', itemPadding: '8px 0', mobileCollapse: true } },
    { type: 'core.languages', label: '语言切换', hint: '多语言站点切换链接', props: { orientation: 'horizontal', gap: '16px' } },
    { type: 'core.tabs', label: '页签', hint: '多面板切换', props: { tabs: [{ label: '页签一' }] } },
    { type: 'core.accordion', label: '手风琴', hint: '折叠展开', props: { items: [{ title: '折叠项一', open: true }] } },
    { type: 'core.marquee', label: '跑马灯', hint: '无缝滚动内容', props: { speed: 12, direction: 'left', gap: '24px' } },
    { type: 'core.counter', label: '计数器', hint: '数字统计', props: { start: 0, end: 100, suffix: '+' } },
    { type: 'core.table', label: '表格', hint: '数据表格', props: { caption: '数据表格', headers: ['列一', '列二'], rows: [['A', 'B'], ['C', 'D']], striped: true, bordered: true } },
    { type: 'core.card', label: '卡片', hint: '标题+正文+按钮', props: { title: '卡片标题', text: '卡片正文内容。', buttonText: '了解更多', buttonLink: '/' } },
    { type: 'core.faq', label: '常见问题', hint: '问答折叠', props: { items: [{ question: '常见问题一？', answer: '这里是回答内容。', open: true }, { question: '常见问题二？', answer: '这里是回答内容。' }] } },
    { type: 'core.quote', label: '引用', hint: '引用块', props: { text: '引用一段有力量的话。', author: '作者名', align: 'left' } },
    { type: 'core.countdown', label: '倒计时', hint: '营销倒计时', props: { targetDate: '2030-01-01 00:00:00', showDays: true } },
    { type: 'core.icon', label: '图标', hint: '通用 SVG 图标', props: { iconName: 'star', size: '24px' } },
    { type: 'core.badge', label: '徽章', hint: '文本徽章', props: { text: '新品', variant: 'solid' } },
    { type: 'core.progress', label: '进度条', hint: '数据进度', props: { value: 60, max: 100, label: '完成度' } },
    { type: 'core.rating', label: '评分', hint: '星形评分', props: { value: 4.5, max: 5 } },
    { type: 'core.form', label: '表单', hint: '联系/订阅表单', props: { fields: [{ type: 'text', label: '姓名', name: 'name', required: true }, { type: 'email', label: '邮箱', name: 'email', required: true }], submitLabel: '提交' } }
];

/** 组件库分组：基础组件大分类平铺（细分类留给进阶组件，当前无进阶内容）；
 *  「区块」概念归全局块（页眉/页脚/区块）。 */
export const paletteGroups = [
    { key: 'basic', title: '基础组件', types: ['core.container', 'core.heading', 'core.text', 'core.button', 'core.image', 'core.gallery', 'core.divider', 'core.spacer', 'core.slider', 'core.list', 'core.infobox', 'core.social_buttons', 'core.video', 'core.nav', 'core.languages', 'core.tabs', 'core.accordion', 'core.marquee', 'core.counter', 'core.table', 'core.card', 'core.faq', 'core.quote', 'core.countdown', 'core.icon', 'core.badge', 'core.progress', 'core.rating', 'core.form'] }
];

/**
 * DEFAULT_CONTENT 组件库默认内容：插入时自动补齐，避免「空壳组件」拖入即编译失败。
 *
 * 两类必需内容（Go 侧 Validate 的权威规则，本表只是「插入时就把内容填好」）：
 *   A 类 结构型 children —— tabs 面板 / accordion 折叠项 / slider slide / marquee 内容，
 *      没有子节点直接编译失败，故 children 恒生成。
 *   B 类 数组型 props —— faq 问答 / form 字段 / gallery 图集，数组为空即编译失败，
 *      故 fallbackProps 在缺失或空数组时补一条（palette 自带示例时保留示例）。
 *   C 类 单值 props —— heading/text/button/image/badge/quote/countdown/video/container
 *      的必需标量（文本、地址、语义标签等），同样走 fallbackProps 兜底。
 *
 * 字段语义：
 *   children      —— 默认子树模板（不含 id，插入时按 allocId 分配；深拷贝后实例化）。
 *   fallbackProps —— 键级兜底：目标 props 缺失该键（或空串/空数组/空对象）时填充，
 *                    已有值一律保留。
 */
export const DEFAULT_CONTENT = {
    // ---- A 类：结构型（children 必需）----
    'core.tabs': {
        // 标签数必须与面板数一致（Validate 强约束），故兜底标签与默认面板一一对应。
        fallbackProps: { tabs: [{ label: '页签一' }] },
        children: [{
            type: 'core.container', name: '页签面板',
            props: { tag: 'div', layout: { engine: 'flex', flex: { direction: 'column', gap: '8px' } }, box: { padding: { desktop: '12px' } } },
            children: [{ type: 'core.text', name: '面板文本', props: { mode: 'plaintext', plainTag: 'p', text: '页签面板内容，点击编辑。' } }]
        }]
    },
    'core.accordion': {
        fallbackProps: { items: [{ title: '折叠项一', open: true }] },
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
        // 跑马灯会把子节点渲染两份（无缝循环），故默认内容用无状态的行内文本，
        // 不用容器：避免同一 DOM id 出现两次。
        children: [{ type: 'core.text', name: '跑马灯内容', props: { mode: 'plaintext', plainTag: 'span', text: '跑马灯内容，点击编辑。' } }]
    },

    // ---- B 类：数组型 props（至少一项）----
    'core.faq': { fallbackProps: { items: [{ question: '新问题？', answer: '在这里填写答案。', open: true }] } },
    'core.form': { fallbackProps: { fields: [{ type: 'text', label: '姓名', name: 'name', required: true }], submitLabel: '提交' } },
    'core.gallery': { fallbackProps: { items: [{ url: 'https://placehold.co/1200x800/png', alt: '图集占位图' }] } },

    // ---- C 类：单值 props 兜底（palette 已提供，这里防止其他插入来源留空）----
    'core.container': { fallbackProps: { tag: 'section', layout: { engine: 'flex', flex: { direction: 'column', gap: '16px' } } } },
    'core.heading': { fallbackProps: { text: '新标题', tag: 'h2' } },
    'core.text': { fallbackProps: { mode: 'plaintext', plainTag: 'p', text: '在这里输入正文内容。' } },
    'core.button': { fallbackProps: { text: '了解更多', action: 'internal', value: '/' } },
    'core.image': { fallbackProps: { src: 'https://placehold.co/1200x800/png', alt: '图片占位符' } },
    'core.badge': { fallbackProps: { text: '新品' } },
    'core.quote': { fallbackProps: { text: '引用一段有力量的话。' } },
    'core.countdown': { fallbackProps: { targetDate: '2030-01-01 00:00:00' } },
    'core.video': { fallbackProps: { url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ' } }
};

/**
 * ALIGN_ARRAY 结构型组件的「内容数组」prop 键：该数组长度必须与子节点数量严格一致
 * （tabs 标签数 = 面板数；accordion 标题数 = 折叠项数）。插入时以默认子节点数量为准
 * 裁剪/补齐数组，杜绝「标签比面板多」这类必然编译失败的状态。
 */
const ALIGN_ARRAY = { 'core.tabs': 'tabs', 'core.accordion': 'items' };

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
    if (def.fallbackProps) props = mergeMissing(props, def.fallbackProps);
    // 结构型：内容数组与默认子节点数量对齐（多则裁剪，少则用兜底项补齐）。
    var alignKey = ALIGN_ARRAY[item.type];
    if (alignKey && def.children && def.children.length) {
        var list = Array.isArray(props[alignKey]) ? props[alignKey].slice(0, def.children.length) : [];
        var fill = (def.fallbackProps && def.fallbackProps[alignKey]) || [];
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

/**
 * ALIGN_ENTRY 结构型组件「内容数组条目」的文案字段与默认前缀：
 * 检查器点「+ 添加」时按当前序号生成「页签2」「折叠项2」，与既有条目文案不重复
 * （Validate 要求每个标签/标题非空）。
 */
const ALIGN_ENTRY = {
    'core.tabs': { field: 'label', prefix: '页签' },
    'core.accordion': { field: 'title', prefix: '折叠项' }
};

/** alignKeyOf 结构型组件的内容数组 prop 键（tabs→tabs，accordion→items；非结构型返回 ''）。 */
export function alignKeyOf(type) {
    return ALIGN_ARRAY[type] || '';
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
 * buildDefaultAlignEntry 结构型组件内容数组的默认条目：DEFAULT_CONTENT 的
 * fallbackProps[alignKey] 第一条深拷贝，并按序号覆写文案字段（页签2 / 折叠项2…）。
 */
export function buildDefaultAlignEntry(type, index) {
    var key = ALIGN_ARRAY[type];
    if (!key) return null;
    var fill = ((DEFAULT_CONTENT[type] || {}).fallbackProps || {})[key] || [];
    if (!fill.length) return null;
    var at = Math.abs(Number(index) || 0);
    var entry = deepClone(fill[at % fill.length]);
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return null;
    var conf = ALIGN_ENTRY[type];
    if (conf && conf.field) entry[conf.field] = conf.prefix + (at + 1);
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
    var key = ALIGN_ARRAY[type];
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
 * 两条方向共用同一份 DEFAULT_CONTENT 与 buildDefaultAlignEntry，新标签/标题的文案
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
    var key = ALIGN_ARRAY[type];
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
