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
 *   - 声明了 data-wb-native 的 select 跳过（如工作台的 wb-unit-select 单位选择器，
 *     它们与 spacing 控件直接联动，接管只会打架）；
 *   - SSR 表单与动态字段共用这一实现；动态字段通过 WBUI.select.create 创建。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];
    var OPEN_CLASS = 'is-open';
    var openRoot = null;
    var instances = new WeakMap();
    var nextId = 0;

    function closeAll(except) {
        if (openRoot && openRoot !== except) {
            var t = openRoot.querySelector('.wbs-trigger');
            var m = openRoot.querySelector('.wbs-menu');
            if (m) { m.hidden = true; }
            openRoot.classList.remove(OPEN_CLASS);
            if (t) { t.setAttribute('aria-expanded', 'false'); }
            if (t) { t.removeAttribute('aria-activedescendant'); }
        }
        openRoot = except || null;
    }

    function labelTextFor(sel) {
        if (sel.getAttribute('aria-label')) return sel.getAttribute('aria-label');
        if (sel.labels && sel.labels.length) return sel.labels[0].textContent.trim();
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
        if (sel.hasAttribute('data-wb-native') || sel.multiple || sel.size > 1) { return; }
        // 状态属于实际 DOM 实例，不能用会随 morph/clone 复制的 data 标记代表监听器。
        var previous = instances.get(sel);
        if (previous && sel.parentNode === previous.root && previous.root.contains(previous.trigger)) return previous;
        if (previous) previous.destroy();
        var label = labelTextFor(sel);

        var root = sel.parentElement;
        if (root.classList.contains('wbs')) {
            // 整个表单被 clone/morph 时，外观会复制，事件不会；清除旧外观后重新增强。
            root.replaceChildren(sel);
            root.classList.remove(OPEN_CLASS);
        } else {
            root = document.createElement('div');
            root.className = 'wbs';
            sel.parentNode.insertBefore(root, sel);
            root.appendChild(sel);
        }
        sel.classList.add('wbs-native');
        sel.setAttribute('tabindex', '-1');
        sel.setAttribute('aria-hidden', 'true');

        var trigger = document.createElement('button');
        trigger.type = 'button';
        trigger.className = 'wbs-trigger';
        trigger.setAttribute('role', 'combobox');
        trigger.setAttribute('aria-haspopup', 'listbox');
        trigger.setAttribute('aria-expanded', 'false');
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
        menu.id = 'wbs-menu-' + (++nextId);
        trigger.setAttribute('aria-controls', menu.id);

        // rebuild 按当前原生 option 重建菜单项。
        //
        // 为什么不是建一次就完：页面会**动态改 option** —— 后台「上级菜单候选过滤」就按类型
        // 逐项 setAttribute('hidden'/'disabled')，抽屉插入的新表单也自带选项。菜单若不同步，
        // 用户看到的是过期列表，选了还会被服务端打回。所以这里既支持重建，也挂 MutationObserver。
        var items = [];
        var active = -1;
        function available(i) {
            var opt = sel.options[i];
            var group = opt && opt.parentElement;
            return opt && !opt.disabled && !opt.hidden && !(group && group.tagName === 'OPTGROUP' && (group.disabled || group.hidden));
        }
        function rebuild() {
            menu.innerHTML = '';
            items = [];
            WBUI.each(sel.options, function (opt, i) {
                var li = document.createElement('li');
                li.className = 'wbs-option';
                li.setAttribute('role', 'option');
                li.dataset.index = String(i);
                li.id = menu.id + '-' + i;
                li.textContent = opt.label;
                li.hidden = opt.hidden || (opt.parentElement.tagName === 'OPTGROUP' && opt.parentElement.hidden);
                if (!available(i)) { li.setAttribute('aria-disabled', 'true'); }
                menu.appendChild(li);
                items.push(li);
            });
            sync();
            if (root.classList.contains(OPEN_CLASS)) highlight(sel.selectedIndex, 1);
        }
        rebuild();

        root.appendChild(trigger);
        root.appendChild(menu);

        function sync() {
            var opt = sel.options[sel.selectedIndex];
            value.textContent = opt ? opt.label : '';
            trigger.disabled = sel.disabled;
            trigger.setAttribute('aria-disabled', sel.disabled ? 'true' : 'false');
            if (sel.disabled) close();
            items.forEach(function (li, i) {
                li.classList.toggle('is-selected', i === sel.selectedIndex);
                li.setAttribute('aria-selected', i === sel.selectedIndex ? 'true' : 'false');
            });
        }

        function highlight(i, direction) {
            active = -1;
            for (var n = 0; n < items.length; n++) {
                var candidate = ((i + n * (direction || 1)) % items.length + items.length) % items.length;
                if (available(candidate)) { active = candidate; break; }
            }
            items.forEach(function (li, k) { li.classList.toggle('is-active', k === active); });
            var el = items[active];
            if (el) trigger.setAttribute('aria-activedescendant', el.id);
            else trigger.removeAttribute('aria-activedescendant');
            if (el && el.scrollIntoView) { el.scrollIntoView({ block: 'nearest' }); }
        }

        function open() {
            if (sel.disabled) return;
            sync();
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
            trigger.removeAttribute('aria-activedescendant');
            if (openRoot === root) openRoot = null;
        }

        function choose(i) {
            if (sel.disabled) return;
            if (i < 0 || i >= sel.options.length) { return; }
            if (!available(i)) { return; }
            var changed = sel.selectedIndex !== i;
            sel.selectedIndex = i;
            sync();
            close();
            trigger.focus();
            // 回调可能同步重建检查器：先收尾旧控件，再提交一次真实变更。
            if (changed) {
                sel.dispatchEvent(new Event('input', { bubbles: true }));
                sel.dispatchEvent(new Event('change', { bubbles: true }));
            }
        }

        trigger.addEventListener('click', function (e) {
            e.preventDefault();
            if (root.classList.contains(OPEN_CLASS)) { close(); } else { open(); }
        });
        trigger.addEventListener('keydown', function (e) {
            var opened = root.classList.contains(OPEN_CLASS);
            switch (e.key) {
                case 'ArrowDown': e.preventDefault(); opened ? highlight(active + 1) : open(); break;
                case 'ArrowUp': e.preventDefault(); opened ? highlight(active - 1, -1) : open(); break;
                case 'Home': if (opened) { e.preventDefault(); highlight(0); } break;
                case 'End': if (opened) { e.preventDefault(); highlight(items.length - 1, -1); } break;
                case 'Enter': case ' ': e.preventDefault(); opened ? choose(active) : open(); break;
                case 'Escape': if (opened) { e.preventDefault(); e.stopPropagation(); close(); } break;
                case 'Tab': close(); break;
                default:
                    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
                        var ch = e.key.toLowerCase();
                        var hit = -1;
                        for (var i = 0; i < sel.options.length; i++) {
                            if (available(i) && (sel.options[i].label || '').trim().toLowerCase().indexOf(ch) === 0) { hit = i; break; }
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
        function focusTrigger() { trigger.focus(); }
        sel.addEventListener('focus', focusTrigger);
        sel.addEventListener('change', sync);
        // option 增删/禁用/隐藏后重建菜单（后台的「上级菜单候选过滤」就是这么改的）。
        var observer;
        if (window.MutationObserver) {
            observer = new MutationObserver(function () { rebuild(); });
            observer.observe(sel, { childList: true, characterData: true, subtree: true, attributes: true,
                attributeFilter: ['disabled', 'hidden', 'selected', 'label', 'value'] });
        }

        sync();
        var api = {
            root: root, trigger: trigger, open: open, close: close, sync: sync,
            get value() { return sel.value; },
            set value(v) { sel.value = v == null ? '' : String(v); sync(); },
            destroy: function () {
                close();
                if (observer) observer.disconnect();
                sel.removeEventListener('focus', focusTrigger);
                sel.removeEventListener('change', sync);
                instances.delete(sel);
            }
        };
        instances.set(sel, api);
        return api;
    }

    WBUI.select = {
        create: function (choices, current, opts) {
            opts = opts || {};
            var mount = document.createElement('div'), sel = document.createElement('select');
            if (opts.label) sel.setAttribute('aria-label', opts.label);
            if (opts.key) sel.dataset.uiKey = String(opts.key);
            choices.forEach(function (choice) {
                var opt = document.createElement('option');
                opt.value = String(choice[0]); opt.textContent = choice[1]; sel.appendChild(opt);
            });
            sel.value = current == null ? '' : String(current);
            if (opts.onChange) sel.addEventListener('change', function () { opts.onChange(sel.value); });
            mount.appendChild(sel);
            return enhance(sel);
        },
        closeAll: function () { closeAll(null); },
        openKeys: function (scope) {
            return Array.from(scope.querySelectorAll('.wbs.is-open select[data-ui-key]')).map(function (sel) { return sel.dataset.uiKey; });
        },
        restoreOpen: function (scope, keys) {
            if (!keys || !keys.length) return;
            WBUI.each(scope.querySelectorAll('select[data-ui-key]'), function (sel) {
                var api = instances.get(sel);
                if (api && keys.indexOf(sel.dataset.uiKey) >= 0) api.open();
            });
        }
    };

    // 控件登记：index.js 与 htmx 重扫都会调用。
    WBUI.register(function (scope) {
        WBUI.each(WBUI.$$('select:not([data-wb-native])', scope), enhance);
    });

    document.addEventListener('click', function (e) {
        if (openRoot && !openRoot.contains(e.target)) closeAll(null);
    });
    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') { closeAll(null); }
    });
    document.addEventListener('reset', function (e) {
        var form = e.target;
        setTimeout(function () {
            if (e.defaultPrevented) return;
            WBUI.each(form.elements, function (sel) { var api = instances.get(sel); if (api) api.sync(); });
        }, 0);
    });
})(window);
