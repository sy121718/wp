// workbench/methods/controls/repeater.js — 各组件 repeater 手写面板（从 methods/inspector.js 提取）。

import { wbDropdown, alignKeyOf, alignMutation } from '../../core.js';
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
        var sel = wbDropdown(platChoices, item.platform || 'facebook', {
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
function applyAligned(ctx, next) {
    if (!next) return;
    ctx.self.snapshot();
    set(ctx, 'props.' + next.key, next.list);
    ctx.node.children = next.children;
    ctx.self.renderTree();
    ctx.self.renderUI();
    ctx.self.syncInspector();
    ctx.self.patchCanvas(ctx.node.id);
}

// alignedRepeater tabs / accordion 共用的编辑面板（两者差异只有行内附加字段）。
function alignedRepeater(ctx, cfg) {
    var type = cfg.type;
    var key = alignKeyOf(type);
    if (!key) return;
    if (!Array.isArray(get(ctx, 'props.' + key))) set(ctx, 'props.' + key, []);
    var list = get(ctx, 'props.' + key);
    var kids = ctx.node.children || [];
    // mutate 统一走纯函数；allocId 用 canvas.js 的 makeIdAllocator（与当前文档已有
    // ID 去重，本批新增的面板容器 + 文本 ID 由同一分配器记账，不会自撞）。
    function mutate(action) {
        applyAligned(ctx, alignMutation(type, list, kids, action, ctx.self.makeIdAllocator()));
    }
    function rowButton(text, title, onClick, cls) {
        var b = document.createElement('button');
        b.type = 'button';
        b.className = cls || 'wb-btn wb-btn-sm wb-btn-ghost';
        b.textContent = text; b.title = title;
        b.addEventListener('click', onClick);
        return b;
    }
    list.forEach(function (entry, idx) {
        var row = document.createElement('div'); row.className = 'wb-repeater-row';
        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
        var input = document.createElement('input'); input.type = 'text';
        input.placeholder = cfg.noun + (idx + 1) + ' ' + cfg.fieldLabel;
        input.value = entry[cfg.field] || '';
        input.addEventListener('change', function () {
            entry[cfg.field] = input.value;
            commit(ctx, 'props.' + key, list);
        });
        mid.appendChild(input);
        if (cfg.extraField) cfg.extraField(entry, mid, function () { commit(ctx, 'props.' + key, list); });
        row.appendChild(mid);
        var acts = document.createElement('div'); acts.className = 'wb-repeater-acts';
        if (idx > 0) acts.appendChild(rowButton('↑', '上移（面板一起移动）', function () {
            mutate({ op: 'move', index: idx, to: idx - 1 });
        }));
        if (idx < list.length - 1) acts.appendChild(rowButton('↓', '下移（面板一起移动）', function () {
            mutate({ op: 'move', index: idx, to: idx + 1 });
        }));
        acts.appendChild(rowButton('✕', '删除该' + cfg.noun + '（同时删除对应面板）', function () {
            mutate({ op: 'remove', index: idx });
        }, 'wb-icon-btn'));
        row.appendChild(acts);
        ctx.panel.appendChild(row);
    });
    var add = document.createElement('button'); add.type = 'button';
    add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
    add.textContent = cfg.addText;
    add.addEventListener('click', function () { mutate({ op: 'add' }); });
    ctx.panel.appendChild(add);
    // 数量提示：一致时说明可整体调序；不一致时（历史脏数据 / 画布直接增删面板）
    // 显式提示，避免用户保存时才发现编译失败。
    var tip = document.createElement('p'); tip.className = 'wb-empty';
    if (list.length === kids.length) {
        tip.textContent = cfg.noun + '与面板数量一致（' + list.length + '）：↑ ↓ 可整体调序，面板内容在画布中编辑。';
    } else {
        tip.style.color = 'var(--c-danger, #d93425)';
        tip.textContent = '数量不一致（' + cfg.noun + ' ' + list.length + ' 个 / 面板 ' + kids.length +
            ' 个），保存会被校验拦下：点「+ 添加」补齐，或删除多余标签。';
    }
    ctx.panel.appendChild(tip);
}

// tabs 页签面板：标签 repeater，与 children 面板严格一一对应（加/删/调序同步子节点）。
export function tabsPanel(ctx) {
    alignedRepeater(ctx, {
        type: 'core.tabs',
        noun: '页签',
        field: 'label',
        fieldLabel: '标签',
        addText: '+ 添加页签（自动创建面板）'
    });
}

// accordion 面板：折叠项标题 repeater，与 children 内容严格一一对应。
export function accordionPanel(ctx) {
    alignedRepeater(ctx, {
        type: 'core.accordion',
        noun: '折叠项',
        field: 'title',
        fieldLabel: '标题',
        addText: '+ 添加折叠项（自动创建内容）',
        extraField: function (entry, mid, save) {
            var openCk = document.createElement('label'); openCk.className = 'wb-check-field';
            var ck = document.createElement('input'); ck.type = 'checkbox'; ck.checked = !!entry.open;
            ck.addEventListener('change', function () { entry.open = ck.checked; save(); });
            openCk.appendChild(ck); openCk.appendChild(document.createTextNode('默认展开'));
            mid.appendChild(openCk);
        }
    });
    // 同时只开一个 / 无边框已由 schema 驱动（ct tag）渲染。
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

