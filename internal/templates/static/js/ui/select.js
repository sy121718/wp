/* ui/select.js — 原始控件：<select> 的可控替身（渐进增强）。
 *
 * 为什么换掉原生下拉：原生 <select> 的弹层由浏览器/桌面环境提供，在 Linux + Wayland 的
 * Chrome 上是 GTK 弹窗——点击中部会瞬开瞬关（表现为「点中间直接选中、只有点边缘才展开」），
 * 且各平台表现不一致。换成 DOM 自绘后交互由我们控制，跨平台一致，也能在无头环境断言。
 *
 * 契约（不破坏既有用法）：
 *   - 原生 <select> 原样保留（视觉隐藏），表单提交、name/value、label[for] 全部照旧；
 *   - 键盘：Enter/Space/↑/↓ 展开与移动，Home/End 首尾，Enter 选中，Esc 收起，字母键前缀跳转；
 *   - 无障碍：combobox + listbox + aria-expanded/aria-selected；点 label 时焦点转给可见触发器；
 *   - 已有 data-wb-path 的 select（工作台自绘下拉）跳过，避免双重增强。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];
    var OPEN_CLASS = 'is-open';
    var openRoot = null;

    function closeAll(except) {
        if (openRoot && openRoot !== except) {
            var t = openRoot.querySelector('.wbs-trigger');
            var m = openRoot.querySelector('.wbs-menu');
            if (m) { m.hidden = true; }
            openRoot.classList.remove(OPEN_CLASS);
            if (t) { t.setAttribute('aria-expanded', 'false'); }
        }
        openRoot = except || null;
    }

    function labelTextFor(sel) {
        if (sel.id) {
            var lab = document.querySelector('label[for="' + sel.id + '"]');
            if (lab) { return lab.textContent.trim(); }
        }
        var wrap = sel.closest('label');
        if (wrap) {
            // label 里既有说明文字又有控件：取控件之前的文本节点。
            var t = '';
            for (var n = wrap.firstChild; n && n !== sel; n = n.nextSibling) {
                if (n.nodeType === 1 && n.tagName === 'SELECT') { break; }
                t += n.textContent || '';
            }
            return t.trim();
        }
        return sel.getAttribute('aria-label') || '';
    }

    function enhance(sel) {
        // markOnce：htmx 局部替换后重扫时不在同一元素上叠出第二套菜单。
        if (!WBUI.markOnce(sel, 'Select')) { return; }
        if (sel.hasAttribute('data-wb-path')) { return; }

        var root = document.createElement('div');
        root.className = 'wbs';
        sel.parentNode.insertBefore(root, sel);
        root.appendChild(sel);
        sel.classList.add('wbs-native');
        sel.setAttribute('tabindex', '-1');

        var trigger = document.createElement('button');
        trigger.type = 'button';
        trigger.className = 'wbs-trigger';
        trigger.setAttribute('role', 'combobox');
        trigger.setAttribute('aria-haspopup', 'listbox');
        trigger.setAttribute('aria-expanded', 'false');
        var label = labelTextFor(sel);
        if (label) { trigger.setAttribute('aria-label', label); }

        var value = document.createElement('span');
        value.className = 'wbs-value';
        trigger.appendChild(value);
        var caret = document.createElement('span');
        caret.className = 'wbs-caret';
        caret.setAttribute('aria-hidden', 'true');
        trigger.appendChild(caret);

        var menu = document.createElement('ul');
        menu.className = 'wbs-menu';
        menu.setAttribute('role', 'listbox');
        menu.hidden = true;

        // rebuild 按当前原生 option 重建菜单项。
        //
        // 为什么不是建一次就完：页面会**动态改 option** —— 后台「上级菜单候选过滤」就按类型
        // 逐项 setAttribute('hidden'/'disabled')，抽屉插入的新表单也自带选项。菜单若不同步，
        // 用户看到的是过期列表，选了还会被服务端打回。所以这里既支持重建，也挂 MutationObserver。
        var items = [];
        function rebuild() {
            menu.innerHTML = '';
            items = [];
            WBUI.each(sel.options, function (opt, i) {
                var li = document.createElement('li');
                li.className = 'wbs-option';
                li.setAttribute('role', 'option');
                li.dataset.index = String(i);
                li.textContent = opt.textContent;
                if (opt.disabled || opt.hidden) { li.setAttribute('aria-disabled', 'true'); }
                menu.appendChild(li);
                items.push(li);
            });
            sync();
        }
        rebuild();

        root.appendChild(trigger);
        root.appendChild(menu);

        var active = sel.selectedIndex < 0 ? 0 : sel.selectedIndex;

        function sync() {
            var opt = sel.options[sel.selectedIndex];
            value.textContent = opt ? opt.textContent : '';
            items.forEach(function (li, i) {
                li.classList.toggle('is-selected', i === sel.selectedIndex);
                li.setAttribute('aria-selected', i === sel.selectedIndex ? 'true' : 'false');
            });
        }

        function highlight(i) {
            if (items.length === 0) { return; }
            active = (i + items.length) % items.length;
            items.forEach(function (li, k) { li.classList.toggle('is-active', k === active); });
            var el = items[active];
            if (el && el.scrollIntoView) { el.scrollIntoView({ block: 'nearest' }); }
        }

        function open() {
            closeAll(root);
            menu.hidden = false;
            root.classList.add(OPEN_CLASS);
            trigger.setAttribute('aria-expanded', 'true');
            highlight(sel.selectedIndex < 0 ? 0 : sel.selectedIndex);
        }

        function close() {
            menu.hidden = true;
            root.classList.remove(OPEN_CLASS);
            trigger.setAttribute('aria-expanded', 'false');
        }

        function choose(i) {
            if (i < 0 || i >= sel.options.length) { return; }
            if (sel.options[i].disabled || sel.options[i].hidden) { return; }
            sel.selectedIndex = i;
            sel.dispatchEvent(new Event('change', { bubbles: true }));
            sel.dispatchEvent(new Event('input', { bubbles: true }));
            sync();
            close();
            trigger.focus();
        }

        trigger.addEventListener('click', function (e) {
            e.preventDefault();
            if (root.classList.contains(OPEN_CLASS)) { close(); } else { open(); }
        });
        trigger.addEventListener('keydown', function (e) {
            var opened = root.classList.contains(OPEN_CLASS);
            switch (e.key) {
                case 'ArrowDown': e.preventDefault(); opened ? highlight(active + 1) : open(); break;
                case 'ArrowUp': e.preventDefault(); opened ? highlight(active - 1) : open(); break;
                case 'Home': if (opened) { e.preventDefault(); highlight(0); } break;
                case 'End': if (opened) { e.preventDefault(); highlight(items.length - 1); } break;
                case 'Enter': case ' ': e.preventDefault(); opened ? choose(active) : open(); break;
                case 'Escape': if (opened) { e.preventDefault(); close(); } break;
                case 'Tab': close(); break;
                default:
                    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
                        var ch = e.key.toLowerCase();
                        var hit = -1;
                        for (var i = 0; i < sel.options.length; i++) {
                            if ((sel.options[i].textContent || '').trim().toLowerCase().indexOf(ch) === 0) { hit = i; break; }
                        }
                        if (hit >= 0) { e.preventDefault(); opened ? highlight(hit) : choose(hit); }
                    }
            }
        });
        menu.addEventListener('click', function (e) {
            var li = e.target.closest('.wbs-option');
            if (!li) { return; }
            e.preventDefault();
            choose(parseInt(li.dataset.index, 10));
        });
        // label[for] 点击会把焦点交给被隐藏的原生 select，转给可见触发器。
        sel.addEventListener('focus', function () { trigger.focus(); });
        sel.addEventListener('change', sync);
        // option 增删/禁用/隐藏后重建菜单（后台的「上级菜单候选过滤」就是这么改的）。
        if (window.MutationObserver) {
            new MutationObserver(function () { rebuild(); })
                .observe(sel, { childList: true, subtree: true, attributes: true });
        }

        sync();
    }

    // 控件登记：index.js 与 htmx 重扫都会调用。
    WBUI.register(function (scope) {
        WBUI.each(WBUI.$$('select:not([data-wb-path])', scope), enhance);
    });

    document.addEventListener('click', function (e) {
        if (!e.target.closest || !e.target.closest('.wbs')) { closeAll(null); return; }
        openRoot = e.target.closest('.wbs');
    });
    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') { closeAll(null); }
    });
})(window);
