// admin.js — 后台通用交互：右侧抽屉 + 侧边栏状态 + 上级菜单候选过滤。
// 零框架：原生 template/dataset。页面模板只需：
//   <button data-drawer-open="#tpl-id" data-drawer-title="新建XX">…</button>
//   <template id="tpl-id"> <form>…</form> </template>
// 列表筛选已改为服务端（GET 表单提交，条件进 SQL 与分页同源），前端不再逐行过滤。
// 抽屉本体（开/关/遮罩/Esc/新内容扫描）已迁到控件基座 js/ui/drawer.js；
// 本文件只保留**业务逻辑**：上级菜单候选过滤与图标字段挂载。
// 两者都挂在基座广播的 wbui:drawer-open 上 —— 业务规则不塞进控件里。
(function () {
    'use strict';

    // 抽屉打开：接管基座广播，挂载本页面的业务增强。
    document.addEventListener('wbui:drawer-open', function (e) {
        var body = e.detail && e.detail.body;
        if (!body) return;
        applyParentFilter(body);
        initIconFields(body);
    });

    /* 上级菜单候选过滤：按当前「类型」只列出合法父级（事前预防，与后端 validateMenuPlacement 双保险）。
       目录只能挂目录下；菜单/iframe/外链可挂目录或菜单下（父级菜单可有子菜单）；按钮挂菜单下。 */
    function applyParentFilter(scope) {
        var typeSel = scope.querySelector('select[name="type"]');
        var parentSel = scope.querySelector('select[name="parent_id"]');
        if (!typeSel || !parentSel) return;
        function filter() {
            var t = parseInt(typeSel.value, 10);
            var allow = (t === 1) ? ['1'] : (t === 3 ? ['2'] : ['1', '2']);
            Array.prototype.forEach.call(parentSel.querySelectorAll('option'), function (o) {
                if (o.value === '0') return;
                var ok = allow.indexOf(o.getAttribute('data-type')) >= 0;
                o.hidden = !ok;
                o.disabled = !ok;
                if (!ok && o.selected) parentSel.value = '0';
            });
        }
        typeSel.addEventListener('change', filter);
        filter();
    }

    /* 图标选择器挂载：表单里放 <div data-icon-field></div> + <input type="hidden" name="icon">，
       打开抽屉时按 hidden 当前值初始化 WPIcons.picker（图标可不选，清除即空）。 */
    var iconLibState = 0;
    var iconLibCbs = [];
    function ensureIconLib(cb) {
        if (window.WPIcons && window.WPIcons.picker) { cb(); return; }
        if (iconLibState === 3) return;
        iconLibCbs.push(cb);
        if (iconLibState === 1) return;
        iconLibState = 1;
        var s = document.createElement('script');
        s.src = '/static/js/icons.js';
        s.onload = function () {
            iconLibState = (window.WPIcons && window.WPIcons.picker) ? 2 : 3;
            iconLibCbs.splice(0).forEach(function (fn) { try { fn(); } catch (e) {} });
        };
        s.onerror = function () { iconLibState = 3; iconLibCbs.length = 0; };
        document.head.appendChild(s);
    }

    /* 列表图标列渲染：扫描 [data-icon-name]，图标库就绪后填充 SVG。 */
    function renderListIcons() {
        var cells = document.querySelectorAll('[data-icon-name]');
        if (!cells.length) return;
        ensureIconLib(function () {
            Array.prototype.forEach.call(cells, function (el) {
                if (el.dataset.iconDone === '1') return;
                var name = el.getAttribute('data-icon-name');
                if (name && window.WPIcons && window.WPIcons.svg(name)) {
                    el.innerHTML = window.WPIcons.svg(name);
                    el.title = window.WPIcons.label(name);
                    el.classList.add('has-icon');
                } else if (name) {
                    el.classList.add('is-unknown');
                    el.title = name + '（图标库无此项）';
                } else {
                    el.classList.add('is-empty');
                }
                el.dataset.iconDone = '1';
            });
        });
    }

    function initIconFields(scope) {
        var hosts = scope.querySelectorAll('[data-icon-field]');
        if (!hosts.length) return;
        ensureIconLib(function () { mountIconFields(hosts); });
    }

    function mountIconFields(hosts) {
        if (!window.WPIcons || !window.WPIcons.picker) return;
        Array.prototype.forEach.call(hosts, function (host) {
            if (host.dataset.iconReady === '1') return;   // 防重复挂载
            var wrap = host.closest('.form-group') || host.parentElement;
            var hidden = wrap ? wrap.querySelector('input[name="icon"]') : null;
            var picker = window.WPIcons.picker({
                value: hidden ? hidden.value : '',
                placeholder: '选择图标（可不选）',
                onPick: function (name) { if (hidden) hidden.value = name || ''; }
            });
            host.appendChild(picker.el);
            host.dataset.iconReady = '1';
        });
    }

    /* ===== 侧边栏：一级图标切换 / 三角展开 / 导航后收起 / 内容区点击收起 / Esc 收起 / 固定 ===== */
    (function initSidebar() {
        var rail = document.querySelector('.rail');
        // subnav 仅在当前页有二级内容时渲染；仪表盘这类直接链接页没有它，
        // 此时一级图标点击走「跳转到该组第一个页面」分支（见下方）。
        var subnav = document.querySelector('.subnav');
        if (!rail) return;

        var pinned = document.body.classList.contains('sidebar-pinned') ||
                     document.cookie.indexOf('sidebar_pinned=1') >= 0;

        function setCookie(name, value) {
            document.cookie = name + '=' + value + '; path=/; max-age=31536000; SameSite=Lax';
        }
        function setCollapsed(v) {
            document.body.classList.toggle('sidebar-collapsed', v);
            setCookie('sidebar_open', v ? '0' : '1');
        }
        // 文案由服务端按请求语言渲染进 data-label-*/data-hint-*（sys_i18n shell.sidebar.*）；
        // 属性缺失时回退中文原文，保证不出现空白（多语言 P1 第二步）。
        function labelOf(btn, key, fallback) {
            return (btn && btn.dataset && btn.dataset[key]) || fallback;
        }
        function applyPin() {
            document.body.classList.toggle('sidebar-pinned', pinned);
            var btn = document.getElementById('pinBtn');
            if (btn) {
                btn.classList.toggle('is-on', pinned);
                var txt = document.getElementById('pinText');
                if (txt) txt.textContent = labelOf(btn, pinned ? 'labelPinned' : 'labelPin', pinned ? '已固定' : '固定侧栏');
                var hint = document.getElementById('pinHint');
                if (hint) hint.textContent = labelOf(btn, pinned ? 'hintPinned' : 'hintUnpinned', pinned ? '导航后保持展开' : '点击菜单后收起');
            }
            setCookie('sidebar_pinned', pinned ? '1' : '0');
        }

        // 一级图标：切换分组 + 展开；点当前分组则收起/展开切换
        var current = (document.querySelector('.subnav-group.is-active') || {}).dataset;
        var currentKey = current ? current.group : null;
        rail.addEventListener('click', function (e) {
            var btn = e.target.closest('.rail-btn[data-group]');
            if (!btn) return;
            // 当前页未渲染二级栏（如仪表盘这类直接链接页）→ 跳该组第一个页面，
            // 目标页会正常渲染二级栏（服务端按 HasSubnav 决定）。
            if (!document.querySelector('.subnav')) {
                var first = btn.getAttribute('data-first-url');
                if (first) location.href = first;
                return;
            }
            var key = btn.getAttribute('data-group');
            var collapsed = document.body.classList.contains('sidebar-collapsed');
            if (key === currentKey && !collapsed) { setCollapsed(true); return; }
            currentKey = key;
            Array.prototype.forEach.call(rail.querySelectorAll('.rail-btn'), function (b) {
                b.classList.toggle('is-active', b === btn);
            });
            Array.prototype.forEach.call(document.querySelectorAll('.subnav-group'), function (g) {
                g.classList.toggle('is-active', g.getAttribute('data-group') === key);
            });
            setCollapsed(false);
        });

        // 三角：展开/收起子级（点文字仍走链接导航）
        if (subnav) subnav.addEventListener('click', function (e) {
            var toggle = e.target.closest('.tree-toggle');
            if (!toggle) return;
            e.preventDefault();
            e.stopPropagation();
            toggle.closest('.tree-node').classList.toggle('is-open');
        });

        // 点菜单项：未固定时先收起再让浏览器导航（cookie 已写，新页面首屏即收起态）
        if (subnav) subnav.addEventListener('click', function (e) {
            var link = e.target.closest('a.tree-link, a.subnav-overview');
            if (!link) return;
            if (pinned) return;
            setCollapsed(true);   // 不 preventDefault：浏览器照常跳转
        });

        // 点击内容区（右侧主区域）自动收起侧栏——补全「只能点一级图标关闭」的缺口。
        // 已固定（pin）时不关；抽屉遮罩层点击由抽屉自身处理，不在此列。
        var mainArea = document.querySelector('.admin-main');
        if (mainArea) {
            mainArea.addEventListener('click', function () {
                if (pinned) return;
                if (document.body.classList.contains('sidebar-collapsed')) return;
                setCollapsed(true);
            });
        }

        // Esc 关闭侧栏（键盘习惯）。抽屉打开时优先关抽屉，不连带收侧栏；固定时不关。
        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape') return;
            if (pinned) return;
            // 用 drawer-open class 判断（关闭时立即移除）；hidden 属性有 220ms 过渡延迟，
            // 用它会在这段窗口内误判「抽屉仍开着」而漏关侧栏。
            if (document.body.classList.contains('drawer-open')) return;
            if (document.body.classList.contains('sidebar-collapsed')) return;
            setCollapsed(true);
        });

        // 固定按钮
        var pinBtn = document.getElementById('pinBtn');
        if (pinBtn) {
            pinBtn.addEventListener('click', function (e) {
                e.stopPropagation();
                pinned = !pinned;
                applyPin();
                if (pinned) setCollapsed(false);
            });
        }
        applyPin();
    })();

    renderListIcons();
})();