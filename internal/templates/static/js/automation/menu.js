// automation/menu.js — palette 与出口菜单（浮层 DOM，不碰图数据）。
//
// ## 为什么 palette 是一个共享浮层而不是左栏常驻
//
// 建节点有两个入口（左栏点击、出口 `+` 圆钮、边上插入），三处都要「选类型」。左栏常驻
// 列表只服务第一种，另外两种要么各写一份列表（三份真源），要么借用左栏（用户点开圆钮
// 却要移鼠标到左栏，且不知道选完会不会连上）。共享浮层把「选哪个类型」和「用它做什
// 么」解耦：调用方给 onPick，浮层只管选。
//
// 类型表由服务端注入（meta.nodeTypes），前端不硬编码第二份 —— 两处名字对不上的表现是
// 「palette 里选得到，保存时被拒」。

/**
 * openPalette 在 (x, y) 打开类型浮层。
 *
 * @param {object} opts
 *   opts.types        [{value,label,hint,disabled,disabledReason}]
 *   opts.x, opts.y    屏幕坐标（浮层左上角）
 *   opts.anchor       触发元素（Esc 关闭后焦点回它）
 *   opts.title        浮层标题文案
 *   opts.onPick(type) 选中回调
 * @returns {function} close 关闭函数（幂等）
 */
export function openPalette(opts) {
    var o = opts || {};
    closePalette();

    var layer = document.createElement('div');
    layer.className = 'auto-palette-pop';
    layer.setAttribute('role', 'menu');
    layer.setAttribute('data-auto-palette', '1');
    if (o.title) {
        var head = document.createElement('p');
        head.className = 'auto-palette-title';
        head.textContent = o.title;
        layer.appendChild(head);
    }

    var buttons = [];
    (o.types || []).forEach(function (t) {
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'auto-palette-item';
        btn.setAttribute('role', 'menuitem');
        btn.setAttribute('data-type', t.value);
        btn.disabled = !!t.disabled;
        if (t.disabled) {
            // 禁用要说明**为什么**：只把一个选项变灰，用户会以为是坏了。
            btn.title = t.disabledReason || '';
            btn.setAttribute('aria-disabled', 'true');
        }
        var label = document.createElement('span');
        label.className = 'auto-palette-label';
        label.textContent = t.label || t.value;
        btn.appendChild(label);
        if (t.hint) {
            var hint = document.createElement('span');
            hint.className = 'auto-palette-hint';
            hint.textContent = t.hint;
            btn.appendChild(hint);
        }
        btn.addEventListener('click', function () {
            close();
            if (o.onPick) { o.onPick(t.value); }
        });
        buttons.push(btn);
        layer.appendChild(btn);
    });

    // 定位：先挂上用 fixed 定位，再按视口边界夹一次（浮层被右/下边界裁掉就没法点了）。
    layer.style.position = 'fixed';
    layer.style.left = Math.max(8, o.x || 8) + 'px';
    layer.style.top = Math.max(8, o.y || 8) + 'px';
    document.body.appendChild(layer);
    var rect = layer.getBoundingClientRect();
    if (rect.right > window.innerWidth - 8) {
        layer.style.left = Math.max(8, window.innerWidth - rect.width - 8) + 'px';
    }
    if (rect.bottom > window.innerHeight - 8) {
        layer.style.top = Math.max(8, window.innerHeight - rect.height - 8) + 'px';
    }

    // 键盘：Esc 关、上下键在可选项间移动（浮层是 role=menu，要有基本的键盘可达性）。
    function onKey(ev) {
        if (ev.key === 'Escape') {
            ev.preventDefault();
            close();
            return;
        }
        if (ev.key !== 'ArrowDown' && ev.key !== 'ArrowUp') { return; }
        ev.preventDefault();
        var enabled = buttons.filter(function (b) { return !b.disabled; });
        if (!enabled.length) { return; }
        var idx = enabled.indexOf(document.activeElement);
        var next = ev.key === 'ArrowDown'
            ? (idx + 1) % enabled.length
            : (idx <= 0 ? enabled.length - 1 : idx - 1);
        enabled[next].focus();
    }

    function onDocDown(ev) {
        if (layer.contains(ev.target)) { return; }
        close();
    }

    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDocDown, true);

    var closed = false;
    function close() {
        if (closed) { return; }
        closed = true;
        document.removeEventListener('keydown', onKey);
        document.removeEventListener('pointerdown', onDocDown, true);
        if (layer.parentNode) { layer.parentNode.removeChild(layer); }
        if (o.anchor && o.anchor.focus) { try { o.anchor.focus(); } catch (e) { /* 焦点回不去不影响功能 */ } }
    }

    var first = buttons.filter(function (b) { return !b.disabled; })[0];
    if (first) { first.focus(); }
    return close;
}

/** closePalette 关掉任何已打开的 palette（重复调用安全）。 */
export function closePalette() {
    var existing = document.querySelectorAll('[data-auto-palette="1"]');
    for (var i = 0; i < existing.length; i++) {
        if (existing[i].parentNode) { existing[i].parentNode.removeChild(existing[i]); }
    }
}

/**
 * openEdgeMenu 在一条边的中点打开 `[+]` / `[×]` 菜单。
 *
 * 两个动作都作用在这条边上：`+` 在中间插入节点（source → new → target），`×` 断开它。
 * 画布上除了这两个动作没有别的「改边」入口 —— 老画布的侧栏下拉只能改**已有**的边，
 * 新建第一条边仍然要靠用户自己填节点 key。
 *
 * @param {object} opts {x, y, onInsert(), onDelete(), anchor}
 */
export function openEdgeMenu(opts) {
    var o = opts || {};
    closePalette();

    var layer = document.createElement('div');
    layer.className = 'auto-edge-pop';
    layer.setAttribute('role', 'menu');
    layer.setAttribute('data-auto-palette', '1');
    layer.style.position = 'fixed';
    layer.style.left = Math.max(8, o.x || 8) + 'px';
    layer.style.top = Math.max(8, o.y || 8) + 'px';

    function mk(text, title, cls, fn) {
        var b = document.createElement('button');
        b.type = 'button';
        b.className = 'auto-edge-btn ' + (cls || '');
        b.textContent = text;
        b.title = title || text;
        b.setAttribute('role', 'menuitem');
        b.addEventListener('click', function () {
            close();
            fn();
        });
        return b;
    }

    layer.appendChild(mk('+', o.insertTitle || '+', 'auto-edge-insert', o.onInsert || function () {}));
    layer.appendChild(mk('×', o.deleteTitle || '×', 'auto-edge-delete', o.onDelete || function () {}));
    document.body.appendChild(layer);

    function onDocDown(ev) {
        if (layer.contains(ev.target)) { return; }
        close();
    }
    function onKey(ev) {
        if (ev.key === 'Escape') { ev.preventDefault(); close(); }
    }
    document.addEventListener('pointerdown', onDocDown, true);
    document.addEventListener('keydown', onKey);

    var closed = false;
    function close() {
        if (closed) { return; }
        closed = true;
        document.removeEventListener('pointerdown', onDocDown, true);
        document.removeEventListener('keydown', onKey);
        if (layer.parentNode) { layer.parentNode.removeChild(layer); }
    }
    var first = layer.querySelector('button');
    if (first) { first.focus(); }
    return close;
}
