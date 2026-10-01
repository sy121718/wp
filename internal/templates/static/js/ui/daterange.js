/* ui/daterange.js — 原始控件：日期范围（两个 <input type="date"> 的可控替身，渐进增强）。
 *
 * 为什么把一个区间从两个独立 date 合并成一个控件：两个 <input type="date"> 表达的是
 * 「两个时刻」而不是「一个区间」—— 起点与终点的先后要用户自己保证；原生弹层由浏览器 /
 * 桌面环境提供，跨平台表现不一致；窄屏下两个控件还会把一行筛选撑成两行。
 * 合并成一个触发器 + 自绘日历后，区间由控件保证（终点早于起点时自动交换），
 * 交互由我们控制，也能在无头环境断言。
 *
 * 契约（与 ui/select.js 同款，不破坏既有用法）：
 *   - 原生两个 <input type="date"> 原样保留（视觉隐藏），表单提交、name/value、label[for] 全部照旧；
 *   - 声明式挂载：容器加 data-wb-daterange，接管其内前两个 date；容器内的 label 不搬动
 *     —— 第一个留作可见标签，其余视觉隐藏但继续参与可访问名计算（label[for] 点击转焦照旧）；
 *   - 键盘：Tab 聚焦触发器，Enter/Space/↓ 展开，Esc 收起并把焦点还给触发器；日历内
 *     ←→↑↓ 移动、Home/End 到本周一/周日、PageUp/PageDown 换月、Enter 选定；
 *   - 无障碍：触发器用 aria-labelledby 指向两个原生 label（名字）+ aria-describedby 指向
 *     当前值（描述）；日历 role="dialog"；每个日期按钮的可访问名由 Intl 生成的完整日期给出；
 *   - 文案：控件内**零硬编码文案** —— 月份 / 星期 / 完整日期一律由 Intl.DateTimeFormat
 *     按页面 lang 生成；换月按钮名取自目标月份标题；清除按钮名由模板 data-wb-clear 提供；
 *   - 多端：弹层宽度按「元素自身到视口两侧的实际距离」封顶（不是按视口比例估的经验值），
 *     空间不够时左右翻转、双月退单月；:hover 规则都在 @media (hover: hover) 里，触屏另给
 *     触摸目标；滚轮与触摸板滚动切换月份；
 *   - SSR 表单与 htmx 局部刷新共用；字段随抽屉进 DOM 时**不提前 return**，交给 WBUI.scan。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    var OPEN_CLASS = 'is-open';
    var EDGE = 12;              // 弹层与视口边缘的最小距离
    var DESIGN_SINGLE = 264;    // 单月弹层的设计宽度
    var DESIGN_DOUBLE = 552;    // 双月弹层的设计宽度
    var WHEEL_LOCK_MS = 140;    // 滚轮换月的节流窗口
    var ISO_SHAPE = 'yyyy-mm-dd';

    var openRoot = null;
    var instances = new WeakMap();
    var nextId = 0;
    var weekdayCache = null;
    var formatters = {};

    // ---- 日历值：一律按本地日历字段处理，不经过 UTC 往返（否则时区会把日期挪一天）----

    function parseISO(value) {
        var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(value == null ? '' : value).trim());
        if (!m) { return null; }
        return { y: +m[1], m: +m[2], d: +m[3] };
    }

    function pad(n) { return n < 10 ? '0' + n : String(n); }

    function toISO(p) { return p.y + '-' + pad(p.m) + '-' + pad(p.d); }

    function compare(a, b) { return a.y - b.y || a.m - b.m || a.d - b.d; }

    function daysInMonth(y, m) { return new Date(y, m, 0).getDate(); }

    // 一周从周一起算：Date.getDay() 是 0=周日，换算成 0=周一。
    function weekdayIndex(y, m, d) { return (new Date(y, m - 1, d).getDay() + 6) % 7; }

    function shiftMonth(p, delta) {
        var total = p.y * 12 + (p.m - 1) + delta;
        var y = Math.floor(total / 12);
        return { y: y, m: total - y * 12 + 1, d: 1 };
    }

    function shiftDay(p, delta) {
        var dt = new Date(p.y, p.m - 1, p.d + delta);
        return { y: dt.getFullYear(), m: dt.getMonth() + 1, d: dt.getDate() };
    }

    function sameDay(a, b) { return !!a && !!b && compare(a, b) === 0; }

    function today() {
        var dt = new Date();
        return { y: dt.getFullYear(), m: dt.getMonth() + 1, d: dt.getDate() };
    }

    // ---- 文案：全部交给 Intl（按页面 lang），控件内不出现任何硬编码词条 ----

    function locale() {
        var lang = document.documentElement && document.documentElement.getAttribute('lang');
        if (lang) { return lang; }
        return (global.navigator && global.navigator.language) || 'zh-CN';
    }

    function formatter(key, options) {
        if (!(key in formatters)) {
            var made = null;
            try {
                made = new Intl.DateTimeFormat(locale(), options);
            } catch (e) {
                made = null;
            }
            formatters[key] = made;
        }
        return formatters[key];
    }

    function monthLabel(ym) {
        var f = formatter('month', { year: 'numeric', month: 'long' });
        return f ? f.format(new Date(ym.y, ym.m - 1, 1)) : (ym.y + '-' + pad(ym.m));
    }

    function dayLabel(p) {
        var f = formatter('day', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' });
        return f ? f.format(new Date(p.y, p.m - 1, p.d)) : toISO(p);
    }

    function weekdayLabels() {
        if (weekdayCache) { return weekdayCache; }
        var f = formatter('weekday', { weekday: 'short' });
        var out = [];
        for (var i = 0; i < 7; i++) {
            // 2024-01-01 是周一：以它为一周起点取七个短名，与日历的周一起始口径一致。
            out.push(f ? f.format(new Date(2024, 0, 1 + i)) : '');
        }
        weekdayCache = out;
        return out;
    }

    // ---- 弹层开关：同一时刻只留一个（跨控件也尽量互斥）----

    function closeAll(except) {
        if (openRoot && openRoot !== except) {
            var api = instances.get(openRoot);
            if (api) { api.close(); } else { openRoot = null; }
        }
        openRoot = except || null;
    }

    function labelTextOf(root) {
        var parts = [];
        WBUI.each(root.querySelectorAll('label'), function (lab) {
            var text = (lab.textContent || '').trim();
            if (text && parts.indexOf(text) < 0) { parts.push(text); }
        });
        return parts;
    }

    function ensureId(el, prefix) {
        if (!el.id) { el.id = prefix + (++nextId); }
        return el.id;
    }

    // ---- 增强 ----

    function enhance(root) {
        var inputs = root.querySelectorAll('input[type="date"]');
        // 结构不完整（不是「范围」形态）时不接管：原生控件留在原地继续可用，属优雅退化，
        // 而不是「初始化时没找到元素就整体退出」—— 后者见 WBUI.register 的扫描方式。
        if (inputs.length < 2) { return null; }

        var previous = instances.get(root);
        if (previous && root.contains(previous.trigger)) { return previous; }
        if (previous) { previous.destroy(); }
        // morph / clone 会复制外观但不复制监听器：清掉残留的自绘部分再重建。
        WBUI.each(Array.prototype.slice.call(root.children), function (child) {
            if (child.classList && (child.classList.contains('wbd-trigger') || child.classList.contains('wbd-pop'))) {
                root.removeChild(child);
            }
        });

        var from = inputs[0];
        var to = inputs[1];

        // 视觉隐藏原生 input（尺寸收成 1px，不用 display:none）：label[for] 的点击转焦仍在，
        // 由下面的 focus 转发交给可见触发器；表单提交、name/value 完全不受影响。
        WBUI.each([from, to], function (input) {
            input.classList.add('wbd-native');
            input.setAttribute('tabindex', '-1');
            input.setAttribute('aria-hidden', 'true');
        });

        var labelIds = [];
        WBUI.each(root.querySelectorAll('label'), function (lab) { labelIds.push(ensureId(lab, 'wbd-label-')); });

        var trigger = document.createElement('button');
        trigger.type = 'button';
        trigger.className = 'wbd-trigger';
        trigger.setAttribute('aria-haspopup', 'dialog');
        trigger.setAttribute('aria-expanded', 'false');
        if (labelIds.length) { trigger.setAttribute('aria-labelledby', labelIds.join(' ')); }

        var value = document.createElement('span');
        value.className = 'wbd-value';
        value.id = 'wbd-value-' + (++nextId);
        trigger.appendChild(value);
        trigger.setAttribute('aria-describedby', value.id);

        var caret = document.createElement('span');
        caret.className = 'wbd-caret';
        caret.setAttribute('aria-hidden', 'true');
        trigger.appendChild(caret);

        var pop = document.createElement('div');
        pop.className = 'wbd-pop';
        pop.id = 'wbd-pop-' + (++nextId);
        pop.hidden = true;
        pop.setAttribute('role', 'dialog');
        if (labelIds.length) { pop.setAttribute('aria-labelledby', labelIds.join(' ')); }
        trigger.setAttribute('aria-controls', pop.id);

        var head = document.createElement('div');
        head.className = 'wbd-head';
        var titles = document.createElement('div');
        titles.className = 'wbd-titles';
        var prevBtn = document.createElement('button');
        prevBtn.type = 'button';
        prevBtn.className = 'wbd-nav';
        prevBtn.dataset.act = 'prev';
        prevBtn.textContent = '\u2039';
        var nextBtn = document.createElement('button');
        nextBtn.type = 'button';
        nextBtn.className = 'wbd-nav';
        nextBtn.dataset.act = 'next';
        nextBtn.textContent = '\u203A';
        head.appendChild(prevBtn);
        head.appendChild(titles);
        head.appendChild(nextBtn);

        var months = document.createElement('div');
        months.className = 'wbd-months';
        pop.appendChild(head);
        pop.appendChild(months);

        var clearLabel = root.getAttribute('data-wb-clear');
        var clearBtn = null;
        if (clearLabel) {
            var foot = document.createElement('div');
            foot.className = 'wbd-foot';
            clearBtn = document.createElement('button');
            clearBtn.type = 'button';
            clearBtn.className = 'wbd-clear';
            clearBtn.dataset.act = 'clear';
            clearBtn.textContent = '\u00D7';
            clearBtn.setAttribute('aria-label', clearLabel);
            clearBtn.hidden = true;
            foot.appendChild(clearBtn);
            pop.appendChild(foot);
        }

        root.appendChild(trigger);
        root.appendChild(pop);

        var state = {
            view: null,
            start: parseISO(from.value),
            end: parseISO(to.value),
            hover: null,
            focus: null
        };
        if (state.start && state.end && compare(state.end, state.start) < 0) {
            var swap = state.start;
            state.start = state.end;
            state.end = swap;
        }
        var monthCount = 1;
        var wheelAt = 0;
        var opened = false;

        // ---- 渲染 ----

        function rangeEnds() {
            if (state.start && state.end) { return [state.start, state.end]; }
            if (state.start && state.hover) {
                return compare(state.hover, state.start) < 0 ? [state.hover, state.start] : [state.start, state.hover];
            }
            return null;
        }

        function buildDay(p) {
            var btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'wbd-day';
            btn.dataset.date = toISO(p);
            btn.textContent = String(p.d);
            btn.setAttribute('aria-label', dayLabel(p));
            btn.tabIndex = -1;
            return btn;
        }

        function buildMonth(ym) {
            var wrap = document.createElement('div');
            var grid = document.createElement('div');
            grid.className = 'wbd-grid';
            // 日期按钮自带完整日期名，星期行只是视觉表头，不重复给读屏器。
            WBUI.each(weekdayLabels(), function (name) {
                var cell = document.createElement('span');
                cell.className = 'wbd-weekday';
                cell.setAttribute('aria-hidden', 'true');
                cell.textContent = name;
                grid.appendChild(cell);
            });
            var lead = weekdayIndex(ym.y, ym.m, 1);
            for (var i = 0; i < lead; i++) {
                var blank = document.createElement('span');
                grid.appendChild(blank);
            }
            var total = daysInMonth(ym.y, ym.m);
            for (var d = 1; d <= total; d++) {
                grid.appendChild(buildDay({ y: ym.y, m: ym.m, d: d }));
            }
            wrap.appendChild(grid);
            return wrap;
        }

        // paint 只改状态类，不重建 DOM（hover 预览会高频触发，重建会闪）。
        function paint() {
            var range = rangeEnds();
            var now = today();
            var days = months.querySelectorAll('.wbd-day');
            WBUI.each(days, function (btn) {
                var p = parseISO(btn.dataset.date);
                var isStart = sameDay(p, state.start);
                var isEnd = sameDay(p, state.end);
                var inRange = !!range && compare(p, range[0]) > 0 && compare(p, range[1]) < 0;
                btn.classList.toggle('is-start', isStart);
                btn.classList.toggle('is-end', isEnd);
                btn.classList.toggle('is-between', inRange);
                btn.classList.toggle('is-today', sameDay(p, now));
                btn.setAttribute('aria-pressed', (isStart || isEnd) ? 'true' : 'false');
                btn.tabIndex = sameDay(p, state.focus) ? 0 : -1;
            });
        }

        function render() {
            titles.innerHTML = '';
            months.innerHTML = '';
            for (var i = 0; i < monthCount; i++) {
                var ym = shiftMonth(state.view, i);
                var title = document.createElement('span');
                title.textContent = monthLabel(ym);
                titles.appendChild(title);
                months.appendChild(buildMonth(ym));
            }
            pop.classList.toggle('is-double', monthCount > 1);
            // 换月按钮的可访问名就是目标月份本身（Intl 生成，零硬编码词条）：
            // 读屏器读「2025年12月 按钮」，语义比「上个月」更准 —— 点它就是跳到那个月。
            prevBtn.setAttribute('aria-label', monthLabel(shiftMonth(state.view, -1)));
            nextBtn.setAttribute('aria-label', monthLabel(shiftMonth(state.view, monthCount)));
            paint();
        }

        function rangeText() {
            var a = state.start ? toISO(state.start) : '';
            var b = state.end ? toISO(state.end) : '';
            if (!a && !b) { return ISO_SHAPE + ' ~ ' + ISO_SHAPE; }
            return (a || ISO_SHAPE) + ' ~ ' + (b || ISO_SHAPE);
        }

        function writeInput(input, next) {
            if (input.value === next) { return; }
            input.value = next;
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.dispatchEvent(new Event('change', { bubbles: true }));
        }

        function sync() {
            syncing = true;
            try {
                writeInput(from, state.start ? toISO(state.start) : '');
                writeInput(to, state.end ? toISO(state.end) : '');
            } finally { syncing = false; }
            value.textContent = rangeText();
            trigger.classList.toggle('has-value', !!(state.start || state.end));
            if (clearBtn) { clearBtn.hidden = !(state.start || state.end); }
        }

        // ---- 弹层定位：上限由「元素自身到视口两侧的实际距离」决定 ----

        function layout() {
            var rect = root.getBoundingClientRect();
            var viewport = global.innerWidth || document.documentElement.clientWidth || 0;
            var spaceRight = viewport - EDGE - rect.left;
            var spaceLeft = rect.right - EDGE;
            var widest = spaceRight > spaceLeft ? spaceRight : spaceLeft;
            var double = widest >= DESIGN_DOUBLE;
            var design = double ? DESIGN_DOUBLE : DESIGN_SINGLE;
            var width = Math.min(design, Math.max(widest, 0));
            monthCount = double ? 2 : 1;
            pop.style.maxWidth = width + 'px';
            pop.classList.toggle('is-flipped', spaceRight < width && spaceLeft > spaceRight);
        }

        function ensureFocusVisible() {
            var inView = false;
            for (var i = 0; i < monthCount; i++) {
                var ym = shiftMonth(state.view, i);
                if (state.focus.y === ym.y && state.focus.m === ym.m) { inView = true; }
            }
            if (!inView) { state.view = { y: state.focus.y, m: state.focus.m, d: 1 }; }
        }

        function focusDay() {
            var btn = months.querySelector('.wbd-day[data-date="' + toISO(state.focus) + '"]');
            if (btn) { btn.focus(); }
        }

        function open() {
            if (from.disabled || to.disabled) { return; }
            if (WBUI.select && WBUI.select.closeAll) { WBUI.select.closeAll(); }
            closeAll(root);
            state.hover = null;
            var anchor = state.start || today();
            state.view = { y: anchor.y, m: anchor.m, d: 1 };
            state.focus = anchor;
            layout();
            render();
            pop.hidden = false;
            root.classList.add(OPEN_CLASS);
            trigger.setAttribute('aria-expanded', 'true');
            opened = true;
            focusDay();
        }

        function close() {
            pop.hidden = true;
            root.classList.remove(OPEN_CLASS);
            trigger.setAttribute('aria-expanded', 'false');
            opened = false;
            if (openRoot === root) { openRoot = null; }
        }

        // sync() 写原生 input 会派发 change 事件、同步触发下方 reload() —— 没有 guard 时，
        // 「先清 from 再清 to」的批量同步会被 reload 把尚未写到的旧值读回 state
        // （实测：点清除后 to 残留旧日期，清不掉）。syncing 期间 reload 直接让路。
        var syncing = false;

        function reload() {
            if (syncing) { return; }
            state.start = parseISO(from.value);
            state.end = parseISO(to.value);
            if (state.start && state.end && compare(state.end, state.start) < 0) {
                var swap = state.start;
                state.start = state.end;
                state.end = swap;
            }
            value.textContent = rangeText();
            trigger.classList.toggle('has-value', !!(state.start || state.end));
            if (clearBtn) { clearBtn.hidden = !(state.start || state.end); }
            if (!pop.hidden) {
                state.view = { y: (state.start || today()).y, m: (state.start || today()).m, d: 1 };
                render();
            }
        }

        function choose(p) {
            if (!state.start || state.end) {
                state.start = p;
                state.end = null;
                state.hover = null;
            } else if (compare(p, state.start) < 0) {
                state.end = state.start;
                state.start = p;
            } else {
                state.end = p;
            }
            state.focus = p;
            state.hover = null;
            sync();
            if (state.start && state.end) {
                close();
                trigger.focus();
                return;
            }
            paint();
        }

        function moveFocus(target) {
            if (!target) { return; }
            state.focus = target;
            ensureFocusVisible();
            render();
            focusDay();
        }

        function stepMonth(delta) {
            state.view = shiftMonth(state.view, delta);
            state.focus = { y: state.view.y, m: state.view.m, d: Math.min(state.focus.d, daysInMonth(state.view.y, state.view.m)) };
            render();
            focusDay();
        }

        // ---- 事件 ----

        trigger.addEventListener('click', function () {
            if (opened) { close(); } else { open(); }
        });

        trigger.addEventListener('keydown', function (e) {
            if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') {
                e.preventDefault();
                open();
            } else if (e.key === 'Escape' && opened) {
                e.preventDefault();
                close();
            }
        });

        function onTriggerFocus() {
            // label[for] 点击会把焦点交给视觉隐藏的原生 input，转给可见触发器。
            trigger.focus();
        }
        from.addEventListener('focus', onTriggerFocus);
        to.addEventListener('focus', onTriggerFocus);
        from.addEventListener('change', reload);
        to.addEventListener('change', reload);

        months.addEventListener('click', function (e) {
            var day = e.target.closest ? e.target.closest('.wbd-day') : null;
            if (!day) { return; }
            e.preventDefault();
            choose(parseISO(day.dataset.date));
        });

        months.addEventListener('mouseover', function (e) {
            var day = e.target.closest ? e.target.closest('.wbd-day') : null;
            if (!day || !state.start || state.end) { return; }
            state.hover = parseISO(day.dataset.date);
            paint();
        });

        months.addEventListener('mouseleave', function () {
            if (!state.hover) { return; }
            state.hover = null;
            paint();
        });

        head.addEventListener('click', function (e) {
            var act = e.target.closest ? e.target.closest('[data-act]') : null;
            if (!act) { return; }
            e.preventDefault();
            stepMonth(act.dataset.act === 'prev' ? -1 : 1);
        });

        if (clearBtn) {
            clearBtn.addEventListener('click', function (e) {
                e.preventDefault();
                state.start = null;
                state.end = null;
                state.hover = null;
                sync();
                render();
                trigger.focus();
            });
        }

        // 键盘：焦点在日期按钮上时，方向键移动的是「哪一天」，不是页面滚动。
        pop.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') {
                e.preventDefault();
                e.stopPropagation();
                close();
                trigger.focus();
                return;
            }
            if (!state.focus) { return; }
            var handled = true;
            switch (e.key) {
                case 'ArrowLeft': moveFocus(shiftDay(state.focus, -1)); break;
                case 'ArrowRight': moveFocus(shiftDay(state.focus, 1)); break;
                case 'ArrowUp': moveFocus(shiftDay(state.focus, -7)); break;
                case 'ArrowDown': moveFocus(shiftDay(state.focus, 7)); break;
                case 'Home': moveFocus(shiftDay(state.focus, -weekdayIndex(state.focus.y, state.focus.m, state.focus.d))); break;
                case 'End': moveFocus(shiftDay(state.focus, 6 - weekdayIndex(state.focus.y, state.focus.m, state.focus.d))); break;
                case 'PageUp': stepMonth(-1); break;
                case 'PageDown': stepMonth(1); break;
                default: handled = false;
            }
            if (handled) { e.preventDefault(); }
        });

        // 滚轮 / 触摸板：在弹层上滚动即换月（阻止它同时滚动页面，否则两个动作打架）。
        pop.addEventListener('wheel', function (e) {
            e.preventDefault();
            var now = Date.now();
            if (now - wheelAt < WHEEL_LOCK_MS) { return; }
            wheelAt = now;
            stepMonth(e.deltaY > 0 ? 1 : -1);
        }, { passive: false });

        var onResize = function () {
            if (pop.hidden) { return; }
            layout();
            render();
        };
        global.addEventListener('resize', onResize);

        sync();

        var api = {
            root: root,
            trigger: trigger,
            pop: pop,
            open: open,
            close: close,
            reload: reload,
            destroy: function () {
                close();
                WBUI.each([from, to], function (input) {
                    input.classList.remove('wbd-native');
                    input.removeAttribute('tabindex');
                    input.removeAttribute('aria-hidden');
                });
                from.removeEventListener('focus', onTriggerFocus);
                to.removeEventListener('focus', onTriggerFocus);
                from.removeEventListener('change', reload);
                to.removeEventListener('change', reload);
                global.removeEventListener('resize', onResize);
                instances.delete(root);
            }
        };
        instances.set(root, api);
        return api;
    }

    WBUI.daterange = {
        create: enhance,
        closeAll: function () { closeAll(null); }
    };

    // 控件登记：index.js 与 htmx 重扫都会调用。找不到元素时什么都不做（不 return 掉整个扫描），
    // 字段随抽屉进 DOM 的场景由 htmx:afterSwap 的重扫接上。
    WBUI.register(function (scope) {
        if (scope && scope.nodeType === 1 && scope.matches && scope.matches('[data-wb-daterange]')) {
            enhance(scope);
        }
        WBUI.each(WBUI.$$('[data-wb-daterange]', scope), enhance);
    });

    document.addEventListener('click', function (e) {
        if (openRoot && !openRoot.contains(e.target)) { closeAll(null); }
    });
    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape' && openRoot) { closeAll(null); }
    });
    document.addEventListener('reset', function (e) {
        var form = e.target;
        if (!form || !form.elements) { return; }
        setTimeout(function () {
            if (e.defaultPrevented) { return; }
            WBUI.each(form.elements, function (el) {
                var host = el.parentNode && el.parentNode.closest ? el.parentNode.closest('[data-wb-daterange]') : null;
                var api = host ? instances.get(host) : null;
                if (api) { api.reload(); }
            });
        }, 0);
    });
})(window);
