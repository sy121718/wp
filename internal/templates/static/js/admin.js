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
       目录只能挂目录下；菜单/iframe/外链可挂目录或菜单下（父级菜单可有子菜单）；按钮挂菜单下。
       data-self="1"（编辑抽屉标出的「它自己与它的子孙」）永远不合法 —— 那类选择必然成环，
       服务端 MenuParentOptions 已经标好，这里只需认这个标记，别用类型规则去猜。 */
    function applyParentFilter(scope) {
        var typeSel = scope.querySelector('select[name="type"]');
        var parentSel = scope.querySelector('select[name="parent_id"]');
        if (!typeSel || !parentSel) return;
        function filter() {
            var t = parseInt(typeSel.value, 10);
            var allow = (t === 1) ? ['1'] : (t === 3 ? ['2'] : ['1', '2']);
            Array.prototype.forEach.call(parentSel.querySelectorAll('option'), function (o) {
                if (o.value === '0') return;
                var ok = o.getAttribute('data-self') !== '1' && allow.indexOf(o.getAttribute('data-type')) >= 0;
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

/* ==========================================================================
   说明浮层（.help）：领域说明默认不占版面，「?」按钮 hover / 聚焦展开。
   样式在 theme.css 的「后台页面骨架」段，纯 CSS 已能 hover / focus-within 展开 ——
   这里只补三件 CSS 做不到的事：Esc 收起、点页面别处收起、贴右边缘时自动左翻。
   ========================================================================== */
(function () {
    'use strict';

    function popOf(host) { return host.querySelector('.help-pop'); }
    function btnOf(host) { return host.querySelector('.help-btn'); }

    function close(host) {
        if (!host) return;
        host.removeAttribute('data-open');
        var b = btnOf(host);
        if (b) b.setAttribute('aria-expanded', 'false');
    }
    function closeAll(except) {
        var open = document.querySelectorAll('.help[data-open]');
        for (var i = 0; i < open.length; i++) {
            if (open[i] !== except) close(open[i]);
        }
    }
    // 贴右边缘的说明往左展开：展开态才有真实盒尺寸，所以先展开再校正。
    function align(host) {
        var pop = popOf(host);
        if (!pop) return;
        host.removeAttribute('data-align');
        var r = pop.getBoundingClientRect();
        if (r.right > window.innerWidth - 8) host.setAttribute('data-align', 'end');
    }

    document.addEventListener('click', function (e) {
        var host = e.target && e.target.closest ? e.target.closest('.help') : null;
        if (!host) { closeAll(null); return; }
        if (!e.target.closest('.help-btn')) { closeAll(host); return; }
        var on = host.hasAttribute('data-open');
        closeAll(host);
        if (on) { close(host); return; }
        host.setAttribute('data-open', '');
        var b = btnOf(host);
        if (b) b.setAttribute('aria-expanded', 'true');
        align(host);
    });

    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape' || e.key === 'Esc') closeAll(null);
    });

    // Tab 走开时同步收起 aria 状态（视觉收起已由 :focus-within 负责）。
    document.addEventListener('focusin', function (e) {
        var host = e.target && e.target.closest ? e.target.closest('.help') : null;
        closeAll(host);
    });

    // 鼠标路径也校正一次对齐：CSS :hover 展开时没有 data-open，只有这一次机会量。
    //
    // **必须等一帧**：mouseover 与 :hover 生效的先后由浏览器决定，立刻量到的往往还是
    // display:none 的盒（宽高 0、right 0），于是「不需要左翻」这个误判被 helpAligned
    // 标记固化下来，此后每次 hover 都不再校正 —— 贴着右边缘的那一列浮层永远伸出视口。
    // rAF 在样式计算之后执行，此时 :hover 已经生效，量到的是真实盒。
    // 窗口尺寸变了旧结论就不成立，所以 resize 时清掉标记重来。
    document.addEventListener('mouseover', function (e) {
        var host = e.target && e.target.closest ? e.target.closest('.help') : null;
        if (!host || host.dataset.helpAligned) return;
        host.dataset.helpAligned = '1';
        requestAnimationFrame(function () { align(host); });
    });
    window.addEventListener('resize', function () {
        var hosts = document.querySelectorAll('.help[data-help-aligned]');
        for (var i = 0; i < hosts.length; i++) delete hosts[i].dataset.helpAligned;
    });
})();

/* 描述类字段的字数提示（评审规则 admin-ui-logic §9）。
   控件上标 data-counter="<建议上限>"，配套输出元素 data-counter-out="<控件 id>"。
   上限只是**建议值**：超出转警告色，不拦截输入 —— SEO 标题 / 描述超长影响的是
   搜索结果里的展示截断，不是能不能存。 */
(function () {
    // 注意：这里**不能**写「页面加载时找不到字段就直接 return」——描述字段几乎都在抽屉里，
    // 而抽屉内容打开时才进 DOM，初始化时一个都没有。提前返回会把下面的委托监听器
    // 一起跳过，表现是「计数永远是空的」，而且刷新页面不会好（每次加载都提前返回）。
    function refresh(el) {
        var out = document.querySelector('[data-counter-out="' + el.id + '"]');
        if (!out) return;
        var max = parseInt(el.getAttribute('data-counter'), 10) || 0;
        var n = (el.value || '').length;
        var tpl = out.getAttribute('data-counter-template') || '{n} / {max}';
        out.textContent = tpl.replace('{n}', String(n)).replace('{max}', String(max));
        out.classList.toggle('is-over', max > 0 && n > max);
    }

    // 事件委托 + 抽屉打开时补初始化：描述字段主要出现在抽屉里，而抽屉内容是
    // 打开时才从 <template> 克隆进 DOM 的 —— 只在页面加载时 querySelectorAll 一次，
    // 这些字段永远绑不上（表现为计数一直是空的）。
    document.addEventListener('input', function (e) {
        var t = e.target;
        if (t && t.hasAttribute && t.hasAttribute('data-counter')) refresh(t);
    });
    document.addEventListener('wbui:drawer-open', function () {
        Array.prototype.forEach.call(document.querySelectorAll('[data-counter]'), refresh);
    });
    // 注意这里不要把初始化写成「遍历页面加载时找到的字段」——那需要提前查一次
    // querySelectorAll，而抽屉里的字段那时还不存在；真正生效的是上面两个委托监听器。
})();

/* 列表客户端筛选：输入即过滤行，不发请求。
   模板契约：输入框标 [data-filter-input]，可筛选的行标 [data-filter-text="参与匹配的文本"]，
   可选的结果为空提示标 [data-filter-empty]。
   作用域取最近的 .list-card / .card / form —— 一个页面可能有多张列表，互不干扰。
   匹配规则：关键词按空白分词，**全部命中**才算匹配（AND），不区分大小写。
   背景：模板里长期写着「admin.js 按 data-filter-text 匹配」但实现从未落地，
   于是「筛选框」是个死控件 —— 能输入、无反应，且不会报错。 */
(function () {
    function filterScope(el) {
        return (el.closest && el.closest('.list-card, .card, form')) || document;
    }

    function applyFilter(input) {
        var scope = filterScope(input);
        var raw = (input.value || '').trim().toLowerCase();
        var terms = raw ? raw.split(/\s+/) : [];
        var rows = scope.querySelectorAll('[data-filter-text]');
        var shown = 0;
        Array.prototype.forEach.call(rows, function (row) {
            var hay = (row.getAttribute('data-filter-text') || '').toLowerCase();
            var hit = true;
            for (var i = 0; i < terms.length; i++) {
                if (hay.indexOf(terms[i]) < 0) { hit = false; break; }
            }
            row.hidden = !hit;
            if (hit) shown++;
            // 被筛掉的行不参与批量提交：hidden 只影响显示，浏览器照样会提交隐藏表格行里的
            // checkbox（用 hidden 属性判断做不到）。所以筛掉时把该行的勾选一并撤销，
            // 并让 change 冒泡通知下面的批量选择模块刷新计数与全选态。
            if (!hit && row.querySelector) {
                var box = row.querySelector('[data-check-item]');
                if (box && box.checked) {
                    box.checked = false;
                    box.dispatchEvent(new Event('change', { bubbles: true }));
                }
            }
        });
        // 空结果提示：让「被筛掉了」与「本来就没数据」在界面上可区分。
        var note = scope.querySelector('[data-filter-empty]');
        if (note) note.hidden = !(terms.length > 0 && shown === 0);
    }

    document.addEventListener('input', function (e) {
        var t = e.target;
        if (t && t.hasAttribute && t.hasAttribute('data-filter-input')) applyFilter(t);
    });
    // 抽屉注入的列表同样适用（与字数提示同一考虑：不能只在页面加载时绑一次）。
    document.addEventListener('wbui:drawer-open', function () {
        Array.prototype.forEach.call(document.querySelectorAll('[data-filter-input]'), applyFilter);
    });
})();

/* 标签页（评审规则 admin-ui-logic §7 附则）：同一数据的多视图切换。
   按 WAI-ARIA tabs 模式实现 —— 点击切换，左右方向键在标签间移动，Home/End 到首尾，
   选中态用 aria-selected，面板用 hidden 控制（不用 display:none 内联样式，
   否则打印与"仅 CSS 可见性"的断言都会失真）。
   面板在服务端全部渲染好，切换是纯前端行为，不产生请求。

   **走事件委托，不在加载时逐节点绑**：标签组还会出现在加载之后才进 DOM 的容器里
   （抽屉从 <template> 克隆进来、htmx swap 进来的片段），一次性 querySelectorAll
   会漏掉它们 —— 表现是「点标签毫无反应、控制台也不报错」，比抛错更难查。
   委托版本对静态与动态两种来源一视同仁，也不需要重复初始化。 */
(function () {
    function activate(root, target) {
        var tabs = root.querySelectorAll('[role="tab"]');
        Array.prototype.forEach.call(tabs, function (t) {
            var on = t === target;
            t.setAttribute('aria-selected', on ? 'true' : 'false');
            t.setAttribute('tabindex', on ? '0' : '-1');
            var id = t.getAttribute('aria-controls');
            var panel = id ? root.querySelector('#' + id) : null;
            if (panel) panel.hidden = !on;
        });
    }

    /* 命中的节点必须是「某个 [data-tabs] 之内的 [role=tab]」——
       前者限定作用域（页面可有多个标签组），后者排除图表等自带 role=tab 的第三方结构。 */
    function tabOf(node) {
        if (!node || !node.closest) return null;
        var tab = node.closest('[role="tab"]');
        return tab && tab.closest('[data-tabs]') ? tab : null;
    }

    document.addEventListener('click', function (e) {
        var tab = tabOf(e.target);
        if (!tab) return;
        activate(tab.closest('[data-tabs]'), tab);
        tab.focus();
    });

    document.addEventListener('keydown', function (e) {
        var tab = tabOf(e.target);
        if (!tab) return;
        var root = tab.closest('[data-tabs]');
        var tabs = Array.prototype.slice.call(root.querySelectorAll('[role="tab"]'));
        var i = tabs.indexOf(tab);
        var next = null;
        if (e.key === 'ArrowRight') next = tabs[(i + 1) % tabs.length];
        else if (e.key === 'ArrowLeft') next = tabs[(i - 1 + tabs.length) % tabs.length];
        else if (e.key === 'Home') next = tabs[0];
        else if (e.key === 'End') next = tabs[tabs.length - 1];
        if (!next) return;
        e.preventDefault();
        activate(root, next);
        next.focus();
    });
})();

/* 列表批量选择（评审规则 admin-ui-logic §7）：全选联动 + 选中计数 + 批量条显隐 + 行选中态。
   勾选框本身就是批量表单的字段（name="ids"），所以这里只做视觉与计数 ——
   提交时浏览器直接带上所有勾中的 ids，不需要在提交前注入隐藏域。
   作用域取勾选框所在的 form：一个页面可以有多个列表，互不干扰。 */
(function () {
    var boxes = document.querySelectorAll('[data-check-all]');
    if (!boxes.length) return;

    function scopeOf(el) {
        return (el.closest && el.closest('form')) || document;
    }

    /* 行是否被客户端筛选藏起来了（applyFilter 把 hidden 设在 <tr> 上）。
       批量选择的作用域是**用户此刻看得见的那批行** —— 隐藏行必须排除在「全选」与
       「全选态判定」之外。hidden 只影响显示，浏览器照样提交隐藏表格行里的 checkbox，
       所以不过滤的后果是：筛出 17 行 → 点全选 → 勾中 117 行、批量表单带上 117 个 id，
       用户以为在删 17 行。静默越界，不报错。 */
    function isFilteredOut(el) {
        var tr = el && el.closest ? el.closest('tr') : null;
        return !!(tr && tr.hidden);
    }

    function refresh(scope) {
        var items = scope.querySelectorAll('[data-check-item]');
        var visible = 0;   // 可见（未被筛选隐藏）的行数 —— 全选态的分母
        var checked = 0;
        Array.prototype.forEach.call(items, function (it) {
            var tr = it.closest ? it.closest('tr') : null;
            if (tr) tr.classList.toggle('is-selected', !!it.checked);
            if (isFilteredOut(it)) return;
            visible++;
            if (it.checked) checked++;
        });
        var box = scope.querySelector('[data-check-all]');
        if (box) {
            // 分母只算可见行：用 items.length 的话，过滤状态下 17/117 会显示成「已全选」。
            box.checked = checked > 0 && checked === visible;
            box.indeterminate = checked > 0 && checked < visible;
        }
        var bar = scope.querySelector('[data-bulk-bar]');
        if (bar) bar.hidden = checked === 0;
        var count = scope.querySelector('[data-bulk-count]');
        if (count) {
            var tpl = count.getAttribute('data-bulk-template') || '已选 {n} 项';
            count.textContent = tpl.replace('{n}', String(checked));
        }
    }

    Array.prototype.forEach.call(boxes, function (box) {
        box.addEventListener('change', function () {
            var scope = scopeOf(box);
            Array.prototype.forEach.call(scope.querySelectorAll('[data-check-item]'), function (it) {
                // 只勾选**当前可见**的行：被筛选隐藏的行不参与本次全选（见 isFilteredOut）。
                // 不过滤等于「用户选了 17 行、提交 117 个 id」—— 删除/改状态类批量动作会
                // 静默改到用户从未看到的记录。没有过滤时不存在 hidden 行，行为完全不变。
                if (isFilteredOut(it)) return;
                it.checked = box.checked;
            });
            refresh(scope);
        });
    });

    // 行首勾选框没有属性值，getAttribute('data-check-item') 返回空串（falsy），
    // 必须用 hasAttribute 判断是否存在 —— 否则单行勾选静默失效（全选仍正常，极易漏测）。
    document.addEventListener('change', function (e) {
        var t = e.target;
        if (!t || !t.hasAttribute || !t.hasAttribute('data-check-item')) return;
        refresh(scopeOf(t));
    });

    // 筛选一变，可见行数（全选态的分母）就变了，必须重算 —— 否则残留这条错乱：
    // 筛出 17 行 → 点全选（17/17，全选框显示已勾）→ 清空筛选框 → 全选框仍然显示已勾，
    // 而实际只有 17/117 被勾中。此时用户点它想「补全到 117」，浏览器翻转成"取消"，
    // 结果一个都不剩 —— 按钮做了与标签相反的事。监听器注册在筛选模块之后，
    // 执行时行上的 hidden 已经更新完毕。
    document.addEventListener('input', function (e) {
        var t = e.target;
        if (!t || !t.hasAttribute || !t.hasAttribute('data-filter-input')) return;
        Array.prototype.forEach.call(boxes, function (b) { refresh(scopeOf(b)); });
    });
})();

/* 菜单项悬浮面板预览（超级菜单）：服务端渲染触发器与 iframe（见 admin/navigations.html），
   这里只绑行为。事件全部委托到 document —— 抽屉内容来自 <template> 克隆，
   每次打开都是新节点，逐个绑监听会随开合次数累积成泄漏。

   两个状态维度（F1 的修复依据）：
     · is-open   —— 浮层当前可见；
     · is-pinned —— 用户**主动点过**按钮把它固定住。
   有 hover 能力的设备上鼠标移入就会 is-open（mouseover）。若把 click 写成「纯开关」，
   用户「移上去看到浮层 → 点一下」必然得到「关掉」—— mouseover 已经先打开了，是错位。
   所以 click 的语义是**固定 / 取消固定**：未固定 → 固定并确保可见（视觉上常常「无变化」，
   这正是期望）；已固定 → 取消并关闭。无 hover 的设备（触屏）没有 mouseover，
   click 就是唯一的开合路径 —— 同一套语义下「点一下打开、再点关闭」依然成立，
   不需要为触屏另写一条分支（那正是 AddHover 等价形态要避免的「只有一边能用」）。 */
(function () {
    // 是否具备悬停能力：只有真 hover 设备才绑 mouseover/mouseout。
    // 触屏上这两条路径不存在，click 的 pin 语义承担全部开合。
    var canHover = !!(window.matchMedia && window.matchMedia('(hover: hover)').matches);

    function closest(el, sel) {
        return (el && el.closest) ? el.closest(sel) : null;
    }
    function rootOf(el) {
        return closest(el, '[data-panel-preview]');
    }

    // setOpen 是唯一的可见性实现：iframe 的 src 只在**首次打开**时写入 ——
    // 一个菜单项一个 iframe，整页渲染就等于同时发起几十次预览编译。
    function setOpen(root, on) {
        var btn = root.querySelector('[data-panel-preview-toggle]');
        var pop = root.querySelector('.nav-panel-preview-pop');
        var frame = root.querySelector('[data-panel-preview-frame]');
        if (!btn || !pop || !frame) return;
        if (on && !frame.getAttribute('src')) {
            frame.setAttribute('src', btn.getAttribute('data-preview-src') || '');
        }
        pop.hidden = !on;
        btn.setAttribute('aria-expanded', on ? 'true' : 'false');
        root.classList.toggle('is-open', on);
    }

    function setPinned(root, on) {
        root.classList.toggle('is-pinned', on);
    }

    // close 是唯一的关闭路径：取消固定 + 收起。两者必须一起做，否则会留下
    // 「已固定但不可见」的状态，下一次 mouseout 会因为它而拒绝收起。
    function close(root) {
        setPinned(root, false);
        setOpen(root, false);
    }

    function focusTrigger(root) {
        var btn = root.querySelector('[data-panel-preview-toggle]');
        if (btn) btn.focus();
    }

    function closeAll(except) {
        var open = document.querySelectorAll('[data-panel-preview].is-open, [data-panel-preview].is-pinned');
        Array.prototype.forEach.call(open, function (root) {
            if (root !== except) close(root);
        });
    }

    // 点击：按钮固定/取消固定（触屏的唯一开合路径）、关闭按钮收起、点外部收起。
    document.addEventListener('click', function (e) {
        var root = rootOf(e.target);
        if (root) {
            if (closest(e.target, '[data-panel-preview-close]')) {
                e.preventDefault();
                close(root);
                focusTrigger(root);
                return;
            }
            if (closest(e.target, '[data-panel-preview-toggle]')) {
                e.preventDefault();
                if (root.classList.contains('is-pinned')) {
                    // 已固定：这是用户主动收起（有 hover 与无 hover 都走这一条）。
                    close(root);
                } else {
                    // 未固定：固定住并确保可见。有 hover 的设备上浮层往往已经因 mouseover
                    // 打开 —— 此时「click 后仍然打开」正是 F1 要的结果；无 hover 的设备上
                    // 这一步就是打开。两条路径共用同一段实现。
                    closeAll(root);
                    setPinned(root, true);
                    setOpen(root, true);
                }
                return;
            }
        }
        closeAll(root);
    });

    // Esc 的作用域是「最上层的可关闭层」。
    //
    // ui/drawer.js 的 Esc 处理挂在 document 的**冒泡**阶段、而且不看浮层：浮层开着时它会
    // 先把整个抽屉关掉，而 closeDrawer 里 body.innerHTML='' —— 浮层节点一起被清掉，
    // 于是「只关浮层并把焦点还给触发按钮」永远不可达（F 线实测：Esc 后抽屉关闭、
    // 浮层 DOM 消失、焦点掉到 body）。
    // 这里用**捕获阶段**先跑并阻止继续传播，把 Esc 优先交给浮层；浮层没开时直接 return，
    // 不拦事件，抽屉的 Esc 行为原样保留。
    document.addEventListener('keydown', function (e) {
        if (e.key !== 'Escape') return;
        var open = document.querySelector('[data-panel-preview].is-open');
        if (!open) return;
        e.preventDefault();
        e.stopPropagation();
        close(open);
        focusTrigger(open);
    }, true);

    // 悬停触发：只在真正具备悬停能力的设备上绑（触屏没有 hover，走上面 click 的 pin 语义）。
    if (canHover) {
        document.addEventListener('mouseover', function (e) {
            var root = rootOf(e.target);
            if (!root) return;
            closeAll(root);
            setOpen(root, true);
        });
        document.addEventListener('mouseout', function (e) {
            var root = rootOf(e.target);
            if (!root || !root.classList.contains('is-open')) return;
            // 被固定住的浮层不随鼠标移开而关闭（用户点它就是为了让它留在屏幕上）。
            if (root.classList.contains('is-pinned')) return;
            if (e.relatedTarget && root.contains(e.relatedTarget)) return;
            // 焦点还在浮层里（键盘用户正在操作 iframe）时不收。
            if (root.contains(document.activeElement)) return;
            setOpen(root, false);
        });
    }
})();

/* 菜单页回跳（?menu=<id>）：自动打开对应菜单项的编辑抽屉。
   面板块「新建并编辑」跳到工作台，块保存后带着 ?menu= 回到本页 —— 用户回来时抽屉已经
   开着，看到的就是刚操作过的那一项，不必在几十行里重新找。
   window load 之后执行：控件基座（drawer.js）的 click 委托在 DOMContentLoaded 注册，
   早于它触发点击会静默无效（按钮点了没反应，且没有任何报错）。 */
(function () {
    function autoOpen() {
        var btn = document.querySelector('[data-drawer-auto]');
        if (!btn || btn.getAttribute('data-drawer-auto-done')) return;
        btn.setAttribute('data-drawer-auto-done', '1');
        btn.click();
    }
    if (document.readyState === 'complete') window.setTimeout(autoOpen, 0);
    else window.addEventListener('load', function () { window.setTimeout(autoOpen, 0); });
})();
