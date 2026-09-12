// workbench/methods/controls/misc.js — schema 字段分发、手写面板编排与 slot 填充（从 methods/inspector.js 提取）。

import { componentSchemas, controlLabel, optionLabel } from '../../core.js';
import { get, set, commit, heading, field, checkbox, segmentedField } from './base.js';
import { colorControl } from './color.js';
import { cornersControl } from './corners.js';
import { unitInput, dimensionsField, dimensionControl, marginControl, spacingControl, boxSpacingControl, rtextControl } from './spacing.js';
import { mediaControl, mediaListControl, galleryItemsPanel, carouselFirstEagerControl } from './media.js';
import { richTextField, typographyPanel, classesControl } from './text.js';
import { listPanel, infoboxPanel, socialPanel, bindRepeaterPanel, marqueePanel, navPanel, faqPanel } from './repeater.js';

// schema 控件 → 表单字段（key 即 props 顶层键，与后端 JSON 序列化一致）。
export function schemaField(ctx, ctl) {
    // hidden 控件不渲染（如 image 的外部地址字段，由媒体控件内嵌输入承担）。
    if (ctl.hidden) return;
    // 条件字段：固定宽度仅在「宽度模式 = 固定宽度」时出现。
    if (ctl.key === 'advanced.widthValue' && (get(ctx, 'props.advanced.widthMode') || 'auto') !== 'fixed') return;
    // 背景自定义定位 / 自定义尺寸仅在选了 custom 时出现。
    if (ctl.key === 'visual.bgPositionXY' && get(ctx, 'props.visual.bgPosition') !== 'custom') return;
    if (ctl.key === 'visual.bgSizeValue' && get(ctx, 'props.visual.bgSize') !== 'custom') return;
    // 容器定位偏移仅在非 static 时出现；抽屉项仅在 drawer 时出现。
    var posType = get(ctx, 'props.position.type') || 'static';
    if (/^position\.(top|right|bottom|left)$/.test(ctl.key) && posType === 'static') return;
    if (/^position\.(drawerSide|drawerOverlay|drawerTriggerId)$/.test(ctl.key) && posType !== 'drawer') return;
    // 四角圆角：首个角一并渲染四角，其余跳过（联动锁 + 单位）。
    // 兼容两套命名：组件级 radiusTL/TR/BR/BL 与通用层 advanced.radius.topLeft/…。
    if (/(^|\.)radiusTL$/.test(ctl.key)) {
        cornersControl(ctx, ctl.label || '圆角', ctl, 'props.' + ctl.key.replace(/TL$/, ''),
            [['TL', '左上'], ['TR', '右上'], ['BR', '右下'], ['BL', '左下']]);
        return;
    }
    if (/(^|\.)radius\.topLeft$/.test(ctl.key)) {
        cornersControl(ctx, ctl.label || '圆角', ctl, 'props.' + ctl.key.replace(/topLeft$/, ''),
            [['topLeft', '左上'], ['topRight', '右上'], ['bottomRight', '右下'], ['bottomLeft', '左下']]);
        return;
    }
    if (/(^|\.)radius(TR|BR|BL)$/.test(ctl.key) || /(^|\.)radius\.(topRight|bottomRight|bottomLeft)$/.test(ctl.key)) return;
    // 按钮/容器自带外观字段（含主题变量回退），跳过通用层的重复外观项。
    if ((ctx.node.type === 'core.button' || ctx.node.type === 'core.container') &&
        /^advanced\.(border\.|radius\.|shadow$)/.test(ctl.key)) return;
    // 显示名优先用 ct tag 声明的中文标签，其次字段名映射表。
    var label = ctl.label || controlLabel(ctl.key);
    var path = 'props.' + ctl.key;
    // 富文本字段（ct:"richtext"）：Trix 编辑器。
    // core.text 的「纯文本」模式是唯一例外（mode=plaintext 时回退多行输入）。
    if (ctl.kind === 'richtext') {
        if (isPlainTextMode(ctx)) { field(ctx, label, path, 'textarea'); return; }
        richTextField(ctx, label, path);
        return;
    }
    // 显式控件类型（ct tag 声明）：媒体/颜色/数值单位/四向边距。
    if (ctl.kind === 'media') { mediaControl(ctx, label, path, ctl); return; }
    if (ctl.kind === 'color') { colorControl(ctx, label, path, ctl); return; }
    if (ctl.kind === 'dimension') { dimensionControl(ctx, label, path, ctl); return; }
    if (ctl.kind === 'margin') { marginControl(ctx, label, path, ctl); return; }
    if (ctl.kind === 'spacing') { spacingControl(ctx, label, path, ctl); return; }
    if (ctl.kind === 'boxspacing') { boxSpacingControl(ctx, label, path, ctl); return; }
    if (ctl.kind === 'classes') { classesControl(ctx, label, path, ctl); return; }
    if (ctl.kind === 'rtext') { rtextControl(ctx, label, path, ctl); return; }
    if (ctl.kind === 'mediaList') { mediaListControl(ctx, label, path, ctl); return; }
    // 集合字段下拉（内置组件的集合绑定）：选项来自后端字段白名单。
    if (ctl.kind === 'collectionfield') { collectionFieldControl(ctx, label, path); return; }
    // 内容字段绑定（heading/text/image 的 binding.field）：item.* 取当前卡片项，<type>.* 取页面内容。
    if (ctl.kind === 'bindingfield') { bindingFieldControl(ctx, label, path); return; }
    if (ctl.kind === 'select') {
        // 选项归一为 [value, label]：ct tag 已声明中文标签时优先。
        var opts = (ctl.options || []).map(function (o) {
            var value = (o && typeof o === 'object') ? o.value : o;
            var text = (o && typeof o === 'object' && o.label) ? o.label : optionLabel(value);
            return [value, text || (value === '' ? '（无）' : value)];
        });
        // 短枚举(≤6 项)用分段按钮组(WP 式节省空间),长列表保留下拉。
        if (opts.length <= 6) {
            segmentedField(ctx, label, path, ctl, (ctx.node.type === 'core.text' && ctl.key === 'mode') ? modeAfter : null);
            return;
        }
        var choices = opts;
        if (ctl.default) choices.unshift(['', '（默认）']);
        // text.mode 切换后重建面板，切换富文本/纯文本编辑形态；
        // 集合源切换后同样重建 —— 字段下拉的选项依赖它。
        var afterSel = null;
        if (ctx.node.type === 'core.text' && ctl.key === 'mode') afterSel = modeAfter;
        if (ctl.key === 'collectionSource') afterSel = function (c) { if (c.self.syncInspector) c.self.syncInspector(); };
        field(ctx, label, path, 'select', choices, afterSel);
    } else if (ctl.kind === 'cssdecls') {
        // 按端样式覆盖：只写样式/布局/动画属性（内容字段不参与按端）。
        var cw = document.createElement('div'); cw.className = 'wb-field';
        var cl = document.createElement('label'); cl.textContent = label; cw.appendChild(cl);
        var ta = document.createElement('textarea'); ta.rows = 3;
        ta.placeholder = '只写样式/布局/动画属性，分号分隔，如 font-size:16px; padding:12px 16px; display:none';
        ta.value = get(ctx, path) == null ? '' : String(get(ctx, path));
        ta.addEventListener('change', function () { commit(ctx, path, ta.value.trim()); });
        cw.appendChild(ta);
        var quick = document.createElement('div'); quick.className = 'wb-inline-btns';
        [['字号', 'font-size:16px'], ['内距', 'padding:12px 16px'], ['宽度', 'width:100%'], ['隐藏', 'display:none']].forEach(function (pair) {
            var qb = document.createElement('button'); qb.type = 'button';
            qb.className = 'wb-btn wb-btn-sm wb-btn-secondary'; qb.textContent = pair[0];
            qb.title = '插入 ' + pair[1];
            qb.addEventListener('click', function () {
                var sep = ta.value.trim() === '' || /;\s*$/.test(ta.value) ? '' : '; ';
                ta.value = ta.value.trim() + sep + pair[1];
                ta.dispatchEvent(new Event('change', { bubbles: true }));
            });
            quick.appendChild(qb);
        });
        cw.appendChild(quick);
        ctx.panel.appendChild(cw);
        return;
    } else if (ctl.kind === 'text') {
        field(ctx, label, path, 'textarea');
    } else if (ctl.kind === 'int' || ctl.kind === 'slider') {
        // slider 声明额外并联一个滑块（拖动调参）；int 仍是纯数字输入。
        field(ctx, label, path, 'number', null, null, {
            min: ctl.min, max: ctl.max, step: ctl.step || 1, range: ctl.kind === 'slider'
        });
    } else if (ctl.kind === 'number') {
        field(ctx, label, path, 'number', null, null, { min: ctl.min, max: ctl.max, step: ctl.step || 0.1 });
    } else if (ctl.kind === 'bool') {
        checkbox(ctx, label, path);
    } else {
        // string / safe / url / regex：文本输入。
        field(ctx, label, path, 'input');
    }
}

/** 集合元数据缓存（GET /api/content/collections，一次会话只拉一次）。 */
var collectionSchemas = null;

/** loadCollections 拉取集合源与字段白名单，失败降级为空列表（不阻断编辑）。 */
function loadCollections(cb) {
    if (collectionSchemas) { cb(collectionSchemas); return; }
    var done = function (list) { collectionSchemas = list; cb(list); };
    try {
        fetch('/api/content/collections', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
            .then(function (r) { return r.json(); })
            .then(function (j) { done((j && j.data && j.data.collections) || []); })
            .catch(function () { done([]); });
    } catch (e) { done([]); }
}

/**
 * collectionFieldControl 集合字段下拉：选项 = 后端字段白名单（按当前节点的
 * 「内容集合」过滤），并额外提供 slug（链接字段常用）。白名单只有一处来源，
 * 组件侧与工作台共用同一份 —— 手填字段名会绕过它，所以这里用下拉而非输入框。
 */
function collectionFieldControl(ctx, label, path) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var cap = document.createElement('label'); cap.textContent = label; wrap.appendChild(cap);
    ctx.panel.appendChild(wrap);

    var source = get(ctx, 'props.collectionSource') || '';
    if (!source) {
        var hint = document.createElement('div');
        hint.className = 'wb-hint';
        hint.textContent = '先选「内容集合」，再挑字段';
        wrap.appendChild(hint);
        return;
    }
    var holder = document.createElement('div');
    holder.className = 'wb-hint';
    holder.textContent = '字段载入中…';
    wrap.appendChild(holder);

    loadCollections(function (list) {
        var schema = null;
        list.forEach(function (c) { if (c.Source === source) schema = c; });
        var options = [['', '（不显示）'], ['slug', 'slug（链接用）']];
        if (schema) {
            (schema.Fields || []).forEach(function (f) { options.push([f, f]); });
        }
        holder.textContent = '';
        holder.className = '';
        holder.appendChild(window.WBUI.select.create(options, get(ctx, path) || '', {
            key: path, label: label,
            onChange: function (v) { commit(ctx, path, v); }
        }).root);
    });
}

/**
 * bindingFieldControl 内容字段绑定下拉：两类来源同一份白名单接口 ——
 *   item.<字段>  当前卡片项（在 cardstack 集合卡里生效，见 core.ItemScope）
 *   <类型>.<字段> 页面内容（contenttemplate/presentation 路径）
 * 前缀写错会绕过白名单，所以这里不给输入框。
 */
function bindingFieldControl(ctx, label, path) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var cap = document.createElement('label'); cap.textContent = label; wrap.appendChild(cap);
    ctx.panel.appendChild(wrap);
    var holder = document.createElement('div');
    holder.className = 'wb-hint';
    holder.textContent = '字段载入中…';
    wrap.appendChild(holder);

    loadCollections(function (list) {
        var options = [['', '（不使用）']];
        (list || []).forEach(function (c) {
            var type = String(c.Source || '').replace('content:', '');
            var fields = (c.Fields || []).slice();
            if (fields.indexOf('slug') < 0) fields.push('slug');
            fields.forEach(function (f) {
                options.push(['item.' + f, '当前卡片项 · ' + f]);
            });
            fields.forEach(function (f) {
                options.push([type + '.' + f, (c.Label || type) + ' · ' + f]);
            });
        });
        holder.textContent = '';
        holder.className = '';
        holder.appendChild(window.WBUI.select.create(options, get(ctx, path) || '', {
            key: path, label: label,
            onChange: function (v) { commit(ctx, path, v); }
        }).root);
    });
}

// isPlainTextMode core.text 的「纯文本」模式：正文不用 Trix（保留多行输入）。
// mode 切换后经 modeAfter 重建检查器，重新决定编辑形态。
export function isPlainTextMode(ctx) {
    return ctx.node.type === 'core.text' && (get(ctx, 'props.mode') || 'richtext') === 'plaintext';
}

// mode 切换(richtext/plaintext)后重建检查器以切换编辑形态。
export function modeAfter(ctx) {
    ctx.self.syncInspector();
}

// container 布局面板（嵌套结构未走 ct 声明，字段路径与编译端手写对齐）。
export function containerLayout(ctx) {
    field(ctx, '语义标签', 'props.tag', 'segment', [['div', '容器'], ['section', '区块'], ['article', '文章'], ['header', '页头'], ['footer', '页脚'], ['main', '主体']]);
    heading(ctx, 'Flex 布局');
    field(ctx, '排列方向', 'props.layout.flex.direction', 'segment', [['column', '纵向'], ['row', '横向']], function () { set(ctx, 'props.layout.engine', 'flex'); });
    field(ctx, '主轴分布', 'props.layout.flex.justify', 'segment', [['flex-start', '起始'], ['center', '居中'], ['flex-end', '末端'], ['space-between', '两端'], ['space-around', '环绕'], ['space-evenly', '均分']]);
    field(ctx, '交叉对齐', 'props.layout.flex.align', 'segment', [['stretch', '拉伸'], ['center', '居中'], ['flex-start', '起始'], ['flex-end', '末端']]);
    checkbox(ctx, '允许换行', 'props.layout.flex.wrap');
    unitInput(ctx, '组件间距', 'props.layout.flex.gap');
    heading(ctx, '尺寸与留白');
    dimensionsField(ctx, '内边距（三端）', 'props.box.padding');
    dimensionsField(ctx, '外边距（三端）', 'props.box.margin');
    unitInput(ctx, '最小高度', 'props.box.minHeight');
}
// container 视觉面板。
// 容器背景/边框/圆角/阴影/布局/动效均已统一到 schema 驱动的折叠区块，无需手写面板。

// divider 嵌入元素面板（Inset 嵌套结构未走 ct 顶层声明）。
export function dividerInset(ctx) {
    heading(ctx, '嵌入元素');
    field(ctx, '嵌入类型', 'props.inset.kind', 'select', [['none', '无'], ['text', '文本'], ['icon', '图标']]);
    field(ctx, '嵌入文本', 'props.inset.text', 'input');
    field(ctx, '图标样式', 'props.inset.iconName', 'select', [['star', '星形'], ['diamond', '菱形'], ['dot', '圆点']]);
    field(ctx, '嵌入位置', 'props.inset.position', 'segment', [['center', '居中'], ['left', '靠左'], ['right', '靠右']]);
    unitInput(ctx, '两侧留白', 'props.inset.spacing');
}

// divider 嵌入文本样式。
export function dividerInsetStyle(ctx) {
    heading(ctx, '嵌入文本样式');
    field(ctx, '字号', 'props.inset.fontSize', 'input');
    field(ctx, '字重', 'props.inset.fontWeight', 'select', [['', '（默认）'], ['400', '常规'], ['500', '中等'], ['600', '半粗'], ['700', '粗体']]);
    field(ctx, '颜色', 'props.inset.color', 'input');
}

// slider 轮播面板：每屏显示数（三端）+ 开关组。
export function sliderPanel(ctx) {
    heading(ctx, '每屏显示数');
    [['desktop', '桌面'], ['tablet', '平板'], ['mobile', '手机']].forEach(function (d) {
        var seg = document.createElement('div'); seg.className = 'wb-field wb-field-seg';
        var cap = document.createElement('label'); cap.textContent = d[1]; seg.appendChild(cap);
        var inner = document.createElement('div'); inner.className = 'wb-seg';
        var cur = get(ctx, 'props.perView.' + d[0]) || 1;
        [1, 2, 3, 4].forEach(function (n) {
            var b = document.createElement('button'); b.type = 'button'; b.className = 'wb-seg-btn';
            b.textContent = n;
            if (cur === n) b.classList.add('is-active');
            b.addEventListener('click', function () {
                commit(ctx, 'props.perView.' + d[0], n);
                inner.querySelectorAll('.wb-seg-btn').forEach(function (x) { x.classList.toggle('is-active', x === b); });
            });
            inner.appendChild(b);
        });
        seg.appendChild(inner); ctx.panel.appendChild(seg);
    });
    // 自动播放/箭头/圆点/间距已由 schema 驱动渲染（ct tag），此处只处理每屏显示数。
}

// interactionPanel 动效分组（效果基本库，全组件：AdvancedProps.Interaction）。
// 入场 17 种 / 滚动触发 / 时长档位 / 延迟 / 悬浮 5 种 / 循环 4 种。
export function interactionPanel(ctx) {
    // 路径适配：原子组件用 advanced.interaction，容器用自己的 interaction（无 Advanced 层）。
    var IA = (ctx.node.type === 'core.container') ? 'props.interaction' : 'props.advanced.interaction';
    field(ctx, '入场动画', IA + '.entrance', 'select', [
        ['', '无'],
        ['fade-in', '淡入'], ['fade-up', '淡入·上'], ['fade-down', '淡入·下'],
        ['fade-left', '淡入·右移'], ['fade-right', '淡入·左移'],
        ['zoom-in', '缩放进入'], ['zoom-out', '缩放退出'],
        ['slide-up', '上滑'], ['slide-down', '下滑'],
        ['slide-left', '右滑'], ['slide-right', '左滑'],
        ['flip-x', '翻转 X'], ['flip-y', '翻转 Y'],
        ['blur-in', '模糊入场'], ['bounce-in', '弹跳入场'], ['rotate-in', '旋转入场']
    ]);
    field(ctx, '时长', IA + '.entranceDuration', 'select', [
        ['', '标准 0.6s'], ['fast', '快 0.3s'], ['slow', '慢 1s']
    ]);
    field(ctx, '延迟(s)', IA + '.entranceDelay', 'number');
    field(ctx, '滚动触发', IA + '.scrollReveal', 'select', [
        ['', '加载即播'], ['reveal', '滚动到视口时']
    ]);
    field(ctx, '悬浮效果', IA + '.hoverEffect', 'select', [
        ['', '无'], ['lift', '上浮'], ['scale', '放大'], ['glow', '发光'],
        ['shadow', '阴影加深'], ['underline', '下划线生长']
    ]);
    field(ctx, '循环动画', IA + '.loopEffect', 'select', [
        ['', '无'], ['pulse', '脉冲'], ['float', '漂浮'],
        ['glow', '呼吸发光'], ['spin', '旋转']
    ]);
}

// ---- 增强字段填充：服务端 slot → 既有控件函数（复用旧面板实现，零重写）----
// 说明：这些函数曾被误删（与旧渲染主流程同段），缺失时 slot 不填充、
// 手写面板不出现，且异常被 fetch 的 catch 吞成「面板加载失败」。
export function inlineSlots(ctx, keyA, keyB) {
    var a = ctx.panel.querySelector('[data-wb-slot="' + keyA + '"]');
    var b = ctx.panel.querySelector('[data-wb-slot="' + keyB + '"]');
    if (!a || !b || a.parentNode !== b.parentNode) return;
    var row = document.createElement('div');
    row.className = 'wb-inline-row';
    a.parentNode.insertBefore(row, a);
    row.appendChild(a); row.appendChild(b);
}

export function fillInspectorSlots(ctx) {
    var slots = ctx.panel.querySelectorAll('[data-wb-slot]');
    if (!slots.length) return;
    var list = componentSchemas[ctx.node.type] || [];
    var byKey = {};
    list.forEach(function (c) { byKey[c.key] = c; });
    Array.prototype.forEach.call(slots, function (slot) {
        var key = slot.getAttribute('data-wb-slot');
        var ctl = byKey[key];
        if (!ctl) return;
        var label = ctl.label || controlLabel(key);
        var path = 'props.' + key;
        var prev = ctx.panel;
        ctx.panel = slot;   // 控件函数统一 appendChild 到 panel，这里临时指向 slot
        try {
            if (/(^|\.)radiusTL$/.test(key)) {
                cornersControl(ctx, label, ctl, 'props.' + key.replace(/TL$/, ''),
                    [['TL', '左上'], ['TR', '右上'], ['BR', '右下'], ['BL', '左下']]);
            } else if (/(^|\.)radius\.topLeft$/.test(key)) {
                cornersControl(ctx, label, ctl, 'props.' + key.replace(/topLeft$/, ''),
                    [['topLeft', '左上'], ['topRight', '右上'], ['bottomRight', '右下'], ['bottomLeft', '左下']]);
            } else if (ctl.kind === 'color') {
                colorControl(ctx, label, path, ctl);
            } else if (ctl.kind === 'spacing' || ctl.kind === 'margin') {
                spacingControl(ctx, label, path, ctl);
            } else if (ctl.kind === 'boxspacing') {
                boxSpacingControl(ctx, label, path, ctl);
            } else if (ctl.kind === 'rtext') {
                rtextControl(ctx, label, path, ctl);
            } else if (ctl.kind === 'richtext') {
                // 富文本字段：服务端只输出 data-wb-slot 占位（slot 名 = 字段 key），
                // 这里用既有 Trix 控件就地填充；core.text 的 plaintext 模式回退多行输入。
                if (isPlainTextMode(ctx)) { field(ctx, label, path, 'textarea'); }
                else { richTextField(ctx, label, path); }
            } else if (ctl.kind === 'classes') {
                classesControl(ctx, label, path, ctl);
            } else if (ctl.kind === 'media') {
                mediaControl(ctx, label, path, ctl);
            } else if (ctl.kind === 'mediaList') {
                mediaListControl(ctx, label, path, ctl);
            } else if (ctl.kind === 'dimension') {
                dimensionControl(ctx, label, path, ctl);
            } else if (ctl.kind === 'collectionfield') {
                // 集合字段下拉：选项来自后端字段白名单（同一个控件函数，两条渲染路径共用）。
                collectionFieldControl(ctx, label, path);
            } else if (ctl.kind === 'bindingfield') {
                bindingFieldControl(ctx, label, path);
            } else {
                field(ctx, label, path, 'input');
            }
        } finally {
            ctx.panel = prev;
        }
    });
}

// ---- 组件手写面板（repeater 等）+ 操作按钮：服务端片段不含这部分 ----
// 这些面板必须落在对应折叠分组内（服务端按 section 渲染 <details>）。
export function sectionFor(ctx, key, title) {
    var el = ctx.panel.querySelector('[data-wb-section="' + key + '"]');
    if (el) return el;
    var d = document.createElement('details');
    d.className = 'wb-inspector-group';
    d.dataset.wbSection = key;
    var s = document.createElement('summary');
    s.className = 'wb-inspector-section';
    s.textContent = title;
    d.appendChild(s);
    ctx.panel.appendChild(d);
    return d;
}

export function into(ctx, target, fn) {
    var prev = ctx.panel;
    ctx.panel = target;
    try { fn(ctx); } finally { ctx.panel = prev; }
}

export function renderInspectorExtras(ctx) {
    if (ctx.self.tab === 'style') {
        if (ctx.node.type === 'core.divider') into(ctx, sectionFor(ctx, 'style', '基础'), dividerInsetStyle);
        if (ctx.node.type === 'core.heading' || ctx.node.type === 'core.text') {
            into(ctx, sectionFor(ctx, 'style', '基础'), function () { typographyPanel(ctx, 'props.typography'); });
        }
        into(ctx, sectionFor(ctx, 'motion', '动效'), interactionPanel);
    } else {
        into(ctx, sectionFor(ctx, 'content', '内容'), function () {
            if (ctx.node.type === 'core.container') containerLayout(ctx);
            if (ctx.node.type === 'core.divider') dividerInset(ctx);
            if (ctx.node.type === 'core.slider') sliderPanel(ctx);
            if (ctx.node.type === 'core.list') listPanel(ctx);
            if (ctx.node.type === 'core.infobox') infoboxPanel(ctx);
            if (ctx.node.type === 'core.faq') faqPanel(ctx);
            if (ctx.node.type === 'core.social_buttons') socialPanel(ctx);
            if (ctx.node.type === 'core.nav') navPanel(ctx);
            bindRepeaterPanel(ctx);
            if (ctx.node.type === 'core.marquee') marqueePanel(ctx);
            if (ctx.node.type === 'core.gallery') {
                heading(ctx, '图片列表');
                galleryItemsPanel(ctx, 'props.items');
                if (get(ctx, 'props.mode') === 'carousel') carouselFirstEagerControl(ctx);
            }
        });
    }
    var actions = document.createElement('div');
    actions.className = 'wb-inspector-actions';
    [['复制组件', ctx.self.copyNode.bind(ctx.self)], ['粘贴样式', ctx.self.pasteStyle.bind(ctx.self)]].forEach(function (pair) {
        var button = document.createElement('button');
        button.type = 'button';
        button.className = 'wb-btn wb-btn-secondary wb-btn-sm';
        button.textContent = pair[0];
        button.addEventListener('click', pair[1]);
        actions.appendChild(button);
    });
    ctx.panel.appendChild(actions);
}
