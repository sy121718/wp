// admin.js — 后台**业务逻辑**：侧边栏状态 + 上级菜单候选过滤。
// 通用控件（抽屉、下拉、图标字段）已迁到控件基座 js/ui/，本文件不再实现控件。
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
    // 图标字段不在这里挂 —— 它已随基座扫描（WBUI.scan(body)）一起生效。
    document.addEventListener('wbui:drawer-open', function (e) {
        var body = e.detail && e.detail.body;
        if (!body) return;
        applyParentFilter(body);
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

    /* ===== 侧边栏：一级图标切换 / 三角展开 / 导航后收起 / 内容区点击收起 / Esc 收起 / 固定 ===== */
    (function initSidebar() {
        var rail = document.querySelector('.rail');
        // subnav 仅在当前页有二级内容时渲染；仪表盘这类直接链接页没有它，
        // 此时一级图标点击走「跳转到该组第一个页面」分支（见下方）。
        var subnav = document.querySelector('.subnav');
        if (!rail) return;

        // 窄屏（<1024px）导航是覆盖式抽屉（theme.css 的断点段 + 下方的 initNavDrawer）：
        // 桌面那套「收起后写 cookie」的自动收放在窄屏不适用 —— 让它写 cookie 会污染桌面首屏态。
        var mobileNav = window.matchMedia('(max-width: 1023px)');

        var pinned = document.body.classList.contains('sidebar-pinned') ||
                     document.cookie.indexOf('sidebar_pinned=1') >= 0;

        function setCookie(name, value) {
            document.cookie = name + '=' + value + '; path=/; max-age=31536000; SameSite=Lax';
        }
        function setCollapsed(v) {
            // 窄屏：rail / subnav 是覆盖式抽屉（theme.css 断点段），开合只认 body.nav-open。
            // 桌面那套「收起态」在窄屏既不适用、写进 cookie 还会污染桌面首屏（手机上逛一圈，
            // 回到桌面打开后台会看到二级栏默认收起）—— 窄屏一律不落状态。
            if (mobileNav.matches) return;
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
            // 「再点当前分组 = 收起二级栏」只对桌面成立：窄屏整栏都在抽屉里，没有可收起的目标，
            // 走这条分支会让窄屏点当前分组变成「什么都没发生」。
            if (!mobileNav.matches && key === currentKey && !collapsed) { setCollapsed(true); return; }
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
                if (mobileNav.matches) return;
                if (pinned) return;
                if (document.body.classList.contains('sidebar-collapsed')) return;
                setCollapsed(true);
            });
        }

        // Esc 关闭侧栏（键盘习惯）。抽屉打开时优先关抽屉，不连带收侧栏；固定时不关。
        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape') return;
            if (mobileNav.matches) return;   // 窄屏由 initNavDrawer 接管（它先捕获并停止冒泡）
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
    /* ===== 窄屏导航抽屉（审计 UI-001）=====
       <1024px 时 rail 与 subnav 在 theme.css 的断点段里变成覆盖式抽屉，开合只认 body.nav-open。
       本段只维护状态（开 / 关 / 无障碍属性），不动布局：汉堡切换、遮罩点击关闭、Esc 关闭、
       回到桌面宽度时清理。无 JS 时按钮由 CSS 隐藏（.nav-toggle { display: none }），
       桌面布局与降级路径都不受影响。 */
    (function initNavDrawer() {
        var toggle = document.getElementById('navToggle');
        if (!toggle) return;
        var scrim = document.querySelector('[data-nav-scrim]');
        var mq = window.matchMedia('(max-width: 1023px)');

        // 文案由服务端按请求语言渲染进 data-label-*（sys_i18n shell.nav.*），属性缺失回退中文原文 ——
        // 与固定按钮（pinBtn）同一手法，避免 JS 里再存一份词表。
        function label(key, fallback) {
            return (toggle.dataset && toggle.dataset[key]) || fallback;
        }
        function isOpen() {
            return document.body.classList.contains('nav-open');
        }
        function setOpen(open) {
            document.body.classList.toggle('nav-open', open);
            toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
            var text = label(open ? 'labelClose' : 'labelOpen', open ? '关闭导航' : '打开导航');
            toggle.setAttribute('aria-label', text);
            toggle.title = text;
        }

        toggle.addEventListener('click', function () { setOpen(!isOpen()); });
        if (scrim) scrim.addEventListener('click', function () { setOpen(false); });
        // 捕获阶段并停止冒泡：否则一次 Esc 会同时触发侧栏那条「Esc 收侧栏」，
        // 表现为一次按键关掉两层（抽屉没了、桌面展开态也被写进 cookie）。
        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape' || !isOpen()) return;
            e.stopPropagation();
            setOpen(false);
            toggle.focus();
        }, true);
        // 回到桌面宽度：桌面布局不认 body.nav-open，留着会让 aria-expanded 与实际状态不一致。
        function onBreakpoint() { if (!mq.matches) setOpen(false); }
        if (mq.addEventListener) mq.addEventListener('change', onBreakpoint);
        else if (mq.addListener) mq.addListener(onBreakpoint);
    })();
    // 列表图标与图标字段的渲染已随基座扫描执行（js/ui/iconfield.js），
    // 且不再局限在页面加载这一次 —— htmx 局部替换后会重扫。

    /* ===== HTMX 全局反馈：统一 loading 指示 + 失败提示（审计 UI-011）=====
       此前这段能力各页面各写各的（多数干脆没有）：请求在飞时点了没反应，失败时也不出声 ——
       htmx 默认只有 2xx 会替换目标节点，4xx/5xx 的响应直接丢弃，用户会以为保存成功了。
       这里把两件事收成横切能力，页面模板不需要再逐个写 hx-indicator：
         ① 请求计数驱动 layout.html 上的 [data-hx-progress] 进度条，同时给触发按钮加
            控件基座的忙碌态（is-busy 自带 disabled ⇒ 顺带防重复点击）；
         ② responseError / sendError / timeout 一律弹 toast（失败绝不静默）。
       4xx 与 5xx 都覆盖：htmx 的 responseHandling 把 [45].. 标为 error，两类都派发
       htmx:responseError —— 会话过期（401）与 CSRF 失败（403）走同一条路径。 */
    (function initHtmxFeedback() {
        var bar = document.querySelector('[data-hx-progress]');
        var inflight = 0;
        var pending = new Map();      // 触发元素 → 忙碌态收尾函数
        var settled = new WeakSet();  // 已收尾的 xhr，防同一请求被重复计数（见 afterRequest）

        function fallback(key, text) {
            return (bar && bar.getAttribute('data-msg-' + key)) || text;
        }
        function setBar(on) {
            if (!bar) return;
            if (on) { bar.removeAttribute('hidden'); } else { bar.setAttribute('hidden', ''); }
        }

        // 忙碌态落在触发元素（或其内的提交按钮）上。优先复用基座 WBUI.busy：它记住原始
        // disabled 状态、成功失败都恢复，比手写 classList 更不容易漏收尾。
        function markBusy(elt) {
            if (!elt || elt.nodeType !== 1 || pending.has(elt)) return;
            var btn = elt;
            if (btn.tagName !== 'BUTTON' && !(btn.tagName === 'INPUT' && btn.type === 'submit')) {
                btn = btn.querySelector('button[type="submit"], input[type="submit"], button');
            }
            if (!btn) return;
            var finish;
            if (window.WBUI && WBUI.busy) {
                finish = WBUI.busy(btn);
            } else {
                btn.classList.add('is-busy');
                btn.setAttribute('aria-busy', 'true');
                finish = function () {
                    btn.classList.remove('is-busy');
                    btn.removeAttribute('aria-busy');
                };
            }
            pending.set(elt, finish);
        }
        function unmarkBusy(elt) {
            var finish = elt && pending.get(elt);
            if (!finish) return;
            pending.delete(elt);
            finish();
        }

        // 只取纯文本：服务端可能回 JSON（统一响应结构）或 HTML 错误页，剥掉标签后再交给
        // toast —— 提示文案不是注入点。
        function clean(s) {
            return String(s).replace(/[<>]/g, '').replace(/\s+/g, ' ').trim().slice(0, 200);
        }
        function bodyMessage(raw) {
            if (!raw || typeof raw !== 'string') return '';
            var s = raw.trim();
            if (!s) return '';
            if (s.charAt(0) === '{' || s.charAt(0) === '[') {
                try {
                    var obj = JSON.parse(s);
                    var msg = obj && (obj.message || obj.msg || obj.error || obj.Message || obj.Msg || obj.Error);
                    if (typeof msg === 'string' && msg) return clean(msg);
                } catch (err) { /* 不是 JSON，按文本继续 */ }
            }
            var doc = null;
            try { doc = new DOMParser().parseFromString(s, 'text/html'); } catch (err) { doc = null; }
            return clean(doc && doc.body ? doc.body.textContent : s);
        }

        function report(e, kind) {
            var detail = (e && e.detail) || {};
            var xhr = detail.xhr;
            var tail = kind === 'network' ? fallback('network', '网络异常，请求未送达')
                : kind === 'timeout' ? fallback('timeout', '请求超时，请重试')
                    : fallback('error', '请求失败，请重试');
            var head = xhr ? bodyMessage(xhr.responseText) : '';
            if (!head && xhr && xhr.status) head = 'HTTP ' + xhr.status;
            if (window.WBUI && WBUI.toast) {
                WBUI.toast(head ? head + ' — ' + tail : tail, { type: 'error', duration: 6000 });
            }
        }

        document.body.addEventListener('htmx:beforeRequest', function (e) {
            inflight++;
            setBar(true);
            markBusy(e.detail && e.detail.elt);
        });
        // afterRequest 在成功 / 失败 / abort / 超时四条路径上都会派发（htmx 的 onload /
        // onerror / onabort / ontimeout 各派发一次），计数与忙碌态统一在这里收尾。
        // 触发元素在响应时已被移出文档的情况下，htmx 会再对最近的存活祖先补派发一次
        // afterRequest（同一个 xhr）—— 用 settled 去重，否则计数会被多减、进度条提前消失。
        document.body.addEventListener('htmx:afterRequest', function (e) {
            var detail = (e && e.detail) || {};
            if (detail.xhr) {
                if (settled.has(detail.xhr)) return;
                settled.add(detail.xhr);
            }
            inflight = inflight > 0 ? inflight - 1 : 0;
            if (inflight === 0) setBar(false);
            unmarkBusy(detail.elt);
        });
        document.body.addEventListener('htmx:responseError', function (e) { report(e, 'response'); });
        document.body.addEventListener('htmx:sendError', function (e) { report(e, 'network'); });
        document.body.addEventListener('htmx:timeout', function (e) { report(e, 'timeout'); });
    })();
})();