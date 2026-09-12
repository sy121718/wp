// workbench/methods/controls/repeater.js — 各组件 repeater 手写面板（从 methods/inspector.js 提取）。

import { alignMutation } from '../../core.js';
import { alignedRepeaters } from '../../generated-contracts.js';
import { commit, get, set, heading } from './base.js';
import { iconPopupPicker, richTextField } from './text.js';

// list 列表面板：样式 + 项 repeater。
export function listPanel(ctx) {
    heading(ctx, '列表项');
    // 列表样式（图标/序号/圆点）已由 schema 驱动（ct tag）渲染。
    if (!Array.isArray(get(ctx, 'props.items'))) set(ctx, 'props.items', []);
    var items = get(ctx, 'props.items');
    function save() { commit(ctx, 'props.items', items); }
    items.forEach(function (item, idx) {
        var row = document.createElement('div'); row.className = 'wb-repeater-row';
        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
        if (get(ctx, 'props.style') === 'icon') {
            // 弹层图标选择：点击图标按钮弹出 SVG 网格，点选回填。
            var ipo = iconPopupPicker(ctx, '图标', item.icon || 'check', function (v) {
                item.icon = v;
                save();
                ipo.refresh(v);
            }, { allowEmpty: true });
            var ipoWrap = document.createElement('div');
            ipoWrap.className = 'wb-icon-pop-wrap';
            ipoWrap.appendChild(ipo.btn);
            ipoWrap.appendChild(ipo.pop);
            mid.appendChild(ipoWrap);
        }
        var text = document.createElement('input'); text.type = 'text'; text.placeholder = '列表项文本'; text.value = item.text || '';
        text.addEventListener('change', function () { item.text = text.value; save(); });
        mid.appendChild(text);
        var link = document.createElement('input'); link.type = 'text'; link.placeholder = '链接（可选）'; link.value = item.link || '';
        link.addEventListener('change', function () { item.link = link.value; save(); });
        mid.appendChild(link);
        var del = document.createElement('button'); del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕';
        del.addEventListener('click', function () { items.splice(idx, 1); save(); ctx.self.syncInspector(); });
        row.appendChild(mid); row.appendChild(del);
        ctx.panel.appendChild(row);
    });
    var add = document.createElement('button'); add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
    add.textContent = '+ 添加列表项';
    add.addEventListener('click', function () { items.push({ icon: 'check', text: '新列表项', link: '' }); save(); ctx.self.syncInspector(); });
    ctx.panel.appendChild(add);
}

// infobox 信息框面板：仅图标选择（其余字段 schema 驱动）。
export function infoboxPanel(ctx) {
    heading(ctx, '图标 / 图片');
    // 弹层选择：内联网格会把上千个图标全部铺开、占满面板；
    // 改为「点击当前图标 → 弹出选择器」（含搜索 + 分类切换），选中回填。
    var cur = get(ctx, 'props.icon') == null ? '' : String(get(ctx, 'props.icon'));
    var ipo = iconPopupPicker(ctx, '图标', cur, function (v) {
        commit(ctx, 'props.icon', v);
        ipo.refresh(v);
    }, { allowEmpty: true });
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var cap = document.createElement('label'); cap.textContent = '图标'; wrap.appendChild(cap);
    var box = document.createElement('div'); box.className = 'wb-icon-pop-wrap';
    box.appendChild(ipo.btn); box.appendChild(ipo.pop);
    wrap.appendChild(box);
    ctx.panel.appendChild(wrap);
}

// faq 常见问题面板：问题 / 答案（富文本）/ 默认展开。
// Items 为数组字段且无 ct tag（schema 反射不递归数组），故答案与展开态在此手写。
export function faqPanel(ctx) {
    heading(ctx, '常见问题');
    if (!Array.isArray(get(ctx, 'props.items'))) set(ctx, 'props.items', []);
    var items = get(ctx, 'props.items');
    function save() { commit(ctx, 'props.items', items); }
    items.forEach(function (it, idx) {
        var row = document.createElement('div'); row.className = 'wb-repeater-row';
        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
        var q = document.createElement('input'); q.type = 'text'; q.placeholder = '问题' + (idx + 1);
        q.value = it.question || '';
        q.addEventListener('change', function () { it.question = q.value; save(); });
        mid.appendChild(q);
        var openLabel = document.createElement('label'); openLabel.className = 'wb-check-field';
        var openCk = document.createElement('input'); openCk.type = 'checkbox'; openCk.checked = !!it.open;
        openCk.addEventListener('change', function () { it.open = openCk.checked; save(); });
        openLabel.appendChild(openCk); openLabel.appendChild(document.createTextNode('默认展开'));
        mid.appendChild(openLabel);
        row.appendChild(mid);
        var del = document.createElement('button'); del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕';
        del.addEventListener('click', function () { items.splice(idx, 1); save(); ctx.self.syncInspector(); });
        row.appendChild(del);
        ctx.panel.appendChild(row);
        // 答案：富文本（Trix），另起一块（编辑器需要横向空间，不塞进 repeater 行）。
        var prev = ctx.panel;
        var ansBox = document.createElement('div'); ansBox.className = 'wb-faq-answer-field';
        ctx.panel.appendChild(ansBox);
        ctx.panel = ansBox;
        try { richTextField(ctx, '答案' + (idx + 1), 'props.items.' + idx + '.answer'); } finally { ctx.panel = prev; }
    });
    var add = document.createElement('button'); add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
    add.textContent = '+ 添加常见问题';
    add.addEventListener('click', function () { items.push({ question: '新问题', answer: '', open: false }); save(); ctx.self.syncInspector(); });
    ctx.panel.appendChild(add);
}

// social_buttons 面板：平台 repeater。
export function socialPanel(ctx) {
    if (!Array.isArray(get(ctx, 'props.items'))) set(ctx, 'props.items', []);
    var items = get(ctx, 'props.items');
    function save() { commit(ctx, 'props.items', items); }
    var platforms = ['facebook', 'x', 'instagram', 'youtube', 'tiktok', 'telegram', 'whatsapp', 'pinterest', 'linkedin'];
    var labels = { facebook: 'Facebook', x: 'X', instagram: 'Instagram', youtube: 'YouTube', tiktok: 'TikTok', telegram: 'Telegram', whatsapp: 'WhatsApp', pinterest: 'Pinterest', linkedin: 'LinkedIn' };
    items.forEach(function (item, idx) {
        var row = document.createElement('div'); row.className = 'wb-repeater-row';
        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
        var platChoices = platforms.map(function (pl) { return [pl, labels[pl]]; });
        var sel = window.WBUI.select.create(platChoices, item.platform || 'facebook', {
            key: 'props.items.' + idx + '.platform', label: '社交平台',
            onChange: function (v) { item.platform = v; save(); }
        }).root;
        var link = document.createElement('input'); link.type = 'text'; link.placeholder = '链接地址'; link.value = item.url || '';
        link.addEventListener('change', function () { item.url = link.value; save(); });
        mid.appendChild(sel); mid.appendChild(link);
        var del = document.createElement('button'); del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕';
        del.addEventListener('click', function () { items.splice(idx, 1); save(); ctx.self.syncInspector(); });
        row.appendChild(mid); row.appendChild(del);
        ctx.panel.appendChild(row);
    });
    var add = document.createElement('button'); add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
    add.textContent = '+ 添加平台';
    add.addEventListener('click', function () { items.push({ platform: 'facebook', url: '' }); save(); ctx.self.syncInspector(); });
    ctx.panel.appendChild(add);
}

// ---------------- tabs / accordion：标签数组 ↔ children 双向同步 ----------------
//
// 背景：tabs「标签数需与面板数一致」、accordion「标题数需与内容数一致」是 Go 侧硬校验
// （tabs.go:58 / accordion.go:60）。检查器过去只改 props 数组、不动 children，于是
// 「加一个标签」立刻把文档推入编译失败状态。此处把三种操作全部收敛到 palette.js 的
// alignMutation（无 DOM 纯函数，契约测试用 node 求值的是同一份实现）：
//   加标签    → 同步实例化一个默认面板（DEFAULT_CONTENT 模板，与组件库拖入同源）
//   删标签    → 同步删除同位置的面板
//   上移/下移 → 标签与面板一起换位（渲染顺序由 children 决定，必须同步）
//
// applyAligned 落地一次对齐结果。顺序关键：先 snapshot（记录改动前的整文档），
// 再同时写 props 与 children —— 一次撤销即可同时回退标签与面板，不会留下
// 「撤销后数量又不一致」的中间态。
export function applyAligned(ctx, next) {
    if (!next) return;
    ctx.self.snapshot();
    set(ctx, 'props.' + next.key, next.list);
    ctx.node.children = next.children;
    ctx.self.renderTree();
    ctx.self.renderUI();
    ctx.self.syncInspector();
    ctx.self.patchCanvas(ctx.node.id);
}

// 服务端负责全部结构，客户端只绑定行为。预期骨架缺失或契约错位必须报错。
// morph 会保留根节点，使用覆盖式委托，重复增强不会累积旧闭包。
export function bindRepeaterPanel(ctx) {
    var spec = alignedRepeaters[ctx.node.type];
    if (!spec) return false;
    var root = ctx.panel.querySelector('[data-wb-rep]');
    if (!root || root.getAttribute('data-wb-rep') !== ctx.node.type ||
        root.getAttribute('data-wb-rep-field') !== spec.field) {
        throw new Error('重复项面板契约不一致：' + ctx.node.type);
    }
    var path = 'props.' + spec.alignKey;
    function entries() {
        var list = get(ctx, path);
        if (list == null) return [];
        if (!Array.isArray(list)) throw new Error('重复项属性必须是数组：' + path);
        return list;
    }
    entries();
    function mutate(action) {
        var next = alignMutation(ctx.node.type, entries(), ctx.node.children || [], action, ctx.self.makeIdAllocator());
        if (!next) throw new Error('重复项操作无效：' + action.op);
        applyAligned(ctx, next);
    }
    root.onclick = function (e) {
        var btn = e.target && e.target.closest ? e.target.closest('[data-wb-rep-op]') : null;
        if (!btn || !root.contains(btn)) return;
        var op = btn.getAttribute('data-wb-rep-op');
        var idx = Number(btn.getAttribute('data-wb-rep-index'));
        if (op === 'add') { mutate({ op: 'add' }); return; }
        if (op === 'remove') { mutate({ op: 'remove', index: idx }); return; }
        if (op === 'move') {
            mutate({ op: 'move', index: idx, to: idx + Number(btn.getAttribute('data-wb-rep-to')) });
            return;
        }
        throw new Error('未知重复项操作：' + op);
    };
    root.onchange = function (e) {
        var t = e.target;
        if (!t || !t.getAttribute) return;
        var inputIndex = t.getAttribute('data-wb-rep-input');
        var extra = t.getAttribute('data-wb-rep-extra');
        if (inputIndex === null && !extra) return;
        if (extra && !(spec.extra || []).some(function (field) { return field.key === extra; })) {
            throw new Error('未知重复项字段：' + extra);
        }
        var idx = Number(inputIndex !== null ? inputIndex : t.getAttribute('data-wb-rep-index'));
        var list = entries();
        if (!Number.isInteger(idx) || idx < 0 || idx >= list.length) {
            throw new Error('重复项序号越界：' + idx);
        }
        // 先复制再交给 commit；原数组不能提前改，否则 snapshot 无法记录编辑前的值。
        var next = list.slice();
        next[idx] = Object.assign({}, list[idx]);
        next[idx][inputIndex !== null ? spec.field : extra] = inputIndex !== null ? t.value : t.checked;
        commit(ctx, path, next);
    };
    return true;
}

// marquee 面板。
export function marqueePanel(ctx) {
    // 速度/方向/间距/悬停暂停均已由 schema 驱动（ct tag）渲染。
    var tip = document.createElement('p'); tip.className = 'wb-empty';
    tip.textContent = '向跑马灯内部拖入任意组件（文本/Logo/卡片），内容将无缝滚动。';
    ctx.panel.appendChild(tip);
}
// counter 的起始/结束/小数位/前缀/后缀/时长已全部由 schema 驱动（ct tag）渲染。

// spacer 高度面板（Responsive 嵌套结构）。
// navPanel 导航菜单项编辑器（items 为嵌套数组，schema 不递归渲染）。
export function navPanel(ctx) {
    heading(ctx, '菜单项');
    if (!Array.isArray(get(ctx, 'props.items'))) set(ctx, 'props.items', []);
    var items = get(ctx, 'props.items');
    function save() { commit(ctx, 'props.items', items); }
    function itemRow(item, list, idx, isChild) {
        var row = document.createElement('div'); row.className = 'wb-repeater-row';
        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
        var lab = document.createElement('input'); lab.type = 'text';
        lab.placeholder = '菜单文字'; lab.value = item.label || '';
        lab.addEventListener('change', function () { item.label = lab.value; save(); });
        var url = document.createElement('input'); url.type = 'text';
        url.placeholder = '链接，如 /shop'; url.value = item.url || '';
        url.addEventListener('change', function () { item.url = url.value; save(); });
        mid.appendChild(lab); mid.appendChild(url);
        row.appendChild(mid);
        var acts = document.createElement('div'); acts.className = 'wb-repeater-acts';
        var tgt = document.createElement('button'); tgt.type = 'button';
        tgt.className = 'wb-btn wb-btn-sm' + (item.target === 'blank' ? ' wb-btn-primary' : ' wb-btn-secondary');
        tgt.textContent = '新窗口'; tgt.title = '切换打开方式';
        tgt.addEventListener('click', function () {
            item.target = item.target === 'blank' ? '' : 'blank';
            save(); ctx.self.syncInspector();
        });
        acts.appendChild(tgt);
        if (!isChild) {
            var addSub = document.createElement('button'); addSub.type = 'button';
            addSub.className = 'wb-btn wb-btn-sm wb-btn-secondary'; addSub.textContent = '+ 子项';
            addSub.addEventListener('click', function () {
                if (!Array.isArray(item.children)) item.children = [];
                item.children.push({ label: '子菜单项', url: '/' });
                save(); ctx.self.syncInspector();
            });
            acts.appendChild(addSub);
        }
        var del = document.createElement('button'); del.type = 'button';
        del.className = 'wb-btn wb-btn-sm wb-btn-ghost'; del.textContent = '✕';
        del.addEventListener('click', function () {
            list.splice(idx, 1); save(); ctx.self.syncInspector();
        });
        acts.appendChild(del);
        row.appendChild(acts);
        return row;
    }
    items.forEach(function (item, idx) {
        ctx.panel.appendChild(itemRow(item, items, idx, false));
        if (Array.isArray(item.children) && item.children.length) {
            var subWrap = document.createElement('div'); subWrap.className = 'wb-nav-sub-items';
            item.children.forEach(function (child, ci) {
                subWrap.appendChild(itemRow(child, item.children, ci, true));
            });
            ctx.panel.appendChild(subWrap);
        }
    });
    var add = document.createElement('button'); add.type = 'button';
    add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add'; add.textContent = '+ 添加菜单项';
    add.addEventListener('click', function () {
        items.push({ label: '新菜单项', url: '/' });
        save(); ctx.self.syncInspector();
    });
    ctx.panel.appendChild(add);
}
