/* ui/daterange.js — 日期区间控件：一个触发器 + 双月日历弹层 + 左侧快捷区间列。
 *
 * 为什么把两个 <input type="date"> 合并成一个控件：两个独立日期框表达的是「两个时刻」，
 * 不是「一个区间」—— 先后要用户自己保证；原生弹层由浏览器 / 桌面环境提供，跨平台不一致；
 * 窄屏下两个框把筛选条撑成两行。合并后区间由控件保证（终点早于起点时自动交换），
 * 交互由我们控制，也能在无头环境里断言。
 *
 * 契约（与 ui/select.js 同款，不破坏既有用法）：
 *   - 原生两个 date 原样保留在 DOM 里（视觉隐藏），name/value/label[for]/表单提交全部照旧
 *     —— 替换只发生在前端，服务端与已发布 URL 一个字都不用改；
 *   - 声明式挂载：容器加 data-wb-daterange，接管其中前两个 input[type="date"]；
 *   - 快捷区间**由模板声明**（<div data-wb-presets> 里放 button[data-preset]），
 *     控件只负责算区间与落到两个 date 上 —— 控件内零硬编码文案；
 *   - 提交的档位字段恒为 custom：所有页面都支持「显式区间优先」（from/to 是唯一真源），
 *     而档位名在服务端按各自时区口径重算，与用户在日历上眼见的那两天可能差一天；
 *   - 多端：弹层宽度按「元素自身到视口两侧的实际距离」封顶，空间不足时左右翻转、双月退单月；
 *     触屏另给放大触摸目标；滚轮 / 触摸板滚动换月；
 *   - 键盘：Tab 聚焦触发器，Enter/Space/↓ 展开，Esc 收起并还焦；日历内 ←→↑↓ 移动、
 *     Home/End 到本周首/末日、PageUp/PageDown 换月、Enter 选定。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    var OPEN_CLASS = 'is-open';
    var EDGE = 12;                 // 弹层与视口边缘的最小距离
    var DESIGN_SINGLE = 300;       // 单月弹层的设计宽度
    var DESIGN_DOUBLE = 660;       // 双月 + 快捷列的设计宽度
    var WHEEL_LOCK_MS = 140;       // 滚轮换月的节流窗口

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

    // 一周从**周日**起算（中文日历习惯）：Date.getDay() 的 0 就是周日，直接可用。
    function weekdayIndex(y, m, d) { return new Date(y, m - 1, d).getDay(); }

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

    // ---- 快捷区间：口径与 date_filter.html 的档位名一一对应 ----

    // 近半年：同一日往前六个月，月末裁剪到目标月的最后一天（8 月 31 日 → 2 月 28/29 日）。
    function halfYearAgo(p) {
        var t = shiftMonth(p, -6);
        return { y: t.y, m: t.m, d: Math.min(p.d, daysInMonth(t.y, t.m)) };
    }

    // rangeOf 返回 [fromISO, toISO]；key 不认识时返回 null（调用方据此不做任何事）。
    // 「本周」是本周一 ~ 今天：未来的日期没有数据，把窗口伸到周日只会让日均变小。
    // base 只给测试用（固定「今天」才能断言跨月/跨年边界），页面调用一律省略。
    function rangeOf(key, base) {
        var t = base || today();
        switch (key) {
            case 'today':
                return [toISO(t), toISO(t)];
            case 'yesterday':
                var y = shiftDay(t, -1);
                return [toISO(y), toISO(y)];
            case 'week':
                // 「本周」按 ISO 周（周一起）算，与日历行首是周日不冲突：一个是数据窗口的
                // 口径，一个是格子排布。未来的日期没有数据，窗口不伸到周日。
                return [toISO(shiftDay(t, -(((new Date(t.y, t.m - 1, t.d).getDay() + 6) % 7)))), toISO(t)];
            case 'month':
                return [toISO({ y: t.y, m: t.m, d: 1 }), toISO(t)];
            case 'lastMonth':
                var first = shiftMonth(t, -1);
                return [toISO(first), toISO({ y: first.y, m: first.m, d: daysInMonth(first.y, first.m) })];
            case 'halfYear':
                return [toISO(halfYearAgo(t)), toISO(t)];
            case 'year':
                return [toISO({ y: t.y, m: 1, d: 1 }), toISO(t)];
            default:
                return null;
        }
    }

    // ---- 弹层尺寸：只由视口宽度决定，与 DOM 无关（所以能被单测钉住）----

    // 视口够宽才双月；不够就单月并把宽度收到可用宽度内。返回给 layout() 用。
    function popMetrics(viewport) {
        var avail = Math.max(viewport - EDGE * 2, 0);
        var double = avail >= DESIGN_DOUBLE;
        return { width: Math.min(double ? DESIGN_DOUBLE : DESIGN_SINGLE, avail), months: double ? 2 : 1 };
    }

    // ---- 文案：月份 / 星期 / 完整日期交给 Intl（按页面 lang），控件内不写死词条 ----

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
        var f = formatter('weekday', { short: true, weekday: 'short' });
        var out = [];
        for (var i = 0; i < 7; i++) {
            // 2024-01-07 是周日：以它为一周起点取七个短名，与日历的周日起始口径一致。
            out.push(f ? f.format(new Date(2024, 0, 7 + i)) : '');
        }
        weekdayCache = out;
        return out;
    }

    // ---- 弹层开关：同一时刻只留一个（跨控件也互斥）----

    function closeAll(except) {
        if (openRoot && openRoot !== except) {
            var api = instances.get(openRoot);
            if (api) { api.close(); } else { openRoot = null; }
        }
        openRoot = except || null;
    }

    function ensureId(el, prefix) {
        if (!el.id) { el.id = prefix + (++nextId); }
        return el.id;
    }

    // ---- 增强 ----

    function enhance(root) {
        var inputs = root.querySelectorAll('input[type="date"]');
        // 结构不完整（不是「区间」形态）时不接管：原生控件留在原地继续可用，属优雅退化。
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

        // 视觉隐藏原生框（不是 display:none）：label[for] 的点击转焦仍在，由下面的 focus
        // 转发交给可见触发器；表单提交、name/value 完全不受影响。
        WBUI.each([from, to], function (input) {
            input.classList.add('wbd-native');
            input.setAttribute('tabindex', '-1');
            input.setAttribute('aria-hidden', 'true');
        });
        // 无 JS 时两个原生框之间的「~」分隔符：接管后由触发器里的「至」表达，这里收起来。
        WBUI.each(root.children, function (child) {
            if (child.hasAttribute && child.hasAttribute('data-wb-sep-toggle')) { child.hidden = true; }
        });
        root.classList.add('wbd');

        var placeholder = root.getAttribute('data-wb-placeholder') || '';
        var separator = root.getAttribute('data-wb-sep') || '\u81f3';
        var presetInput = root.querySelector('[data-wb-preset-value]');

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

        // 快捷列：模板已经渲染好按钮，这里只是把它搬进弹层（文案留在模板层，控件不掺和）。
        var presetBox = root.querySelector('[data-wb-presets]');
        if (presetBox) {
            var presets = document.createElement('div');
            presets.className = 'wbd-presets';
            while (presetBox.firstChild) { presets.appendChild(presetBox.firstChild); }
            presetBox.parentNode.removeChild(presetBox);
            pop.appendChild(presets);
        }

        var body = document.createElement('div');
        body.className = 'wbd-body';
        pop.appendChild(body);

        var months = document.createElement('div');
        months.className = 'wbd-months';
        body.appendChild(months);

        var clearLabel = root.getAttribute('data-wb-clear');
        var clearBtn = null;
        if (clearLabel) {
            var foot = document.createElement('div');
            foot.className = 'wbd-foot';
            clearBtn = document.createElement('button');
            clearBtn.type = 'button';
            clearBtn.className = 'wbd-clear';
            clearBtn.dataset.act = 'clear';
            clearBtn.textContent = clearLabel;
            clearBtn.hidden = true;
            foot.appendChild(clearBtn);
            body.appendChild(foot);
        }

        // 触发器插在两个原生框之前（它们已视觉隐藏）：视觉顺序是「值 → 提交 → 重置」。
        root.insertBefore(trigger, from);
        root.appendChild(pop);

        var state = {
            left: null,     // 左栏所在月（区间起点侧）
            right: null,    // 右栏所在月（区间终点侧，两栏互不联动）
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

        function sameMonth(a, b) { return !!a && !!b && a.y === b.y && a.m === b.m; }

        // 两栏 = 区间的两端，各自独立翻月（不是「连续两个月」）：起点 1 月、终点 10 月这种
        // 跨半年的区间，连续两月根本表达不出来。没有区间时左为今天、右为下个月。
        function setView(fromAnchor, toAnchor) {
            state.left = { y: fromAnchor.y, m: fromAnchor.m, d: 1 };
            var right = toAnchor && !sameMonth(toAnchor, fromAnchor) ? toAnchor : shiftMonth(state.left, 1);
            state.right = { y: right.y, m: right.m, d: 1 };
        }

        // 焦点落在哪一栏；不在任何一栏时按左栏算（下一句 ensureFocusVisible 会把它拉回来）。
        function sideOf(p) { return sameMonth(p, state.right) ? 'right' : 'left'; }

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
                blank.className = 'wbd-blank';
                grid.appendChild(blank);
            }
            var total = daysInMonth(ym.y, ym.m);
            for (var d = 1; d <= total; d++) {
                grid.appendChild(buildDay({ y: ym.y, m: ym.m, d: d }));
            }
            // 补足整行：尾部空格不补的话最后一行的格子宽度会被拉伸（grid 自动等分）。
            var tail = (7 - ((lead + total) % 7)) % 7;
            for (var k = 0; k < tail; k++) {
                var pad = document.createElement('span');
                pad.className = 'wbd-blank';
                grid.appendChild(pad);
            }
            return grid;
        }

        // paint 只改状态类，不重建 DOM（hover 预览会高频触发，重建会闪）。
        function paint() {
            var range = rangeEnds();
            var now = today();
            WBUI.each(months.querySelectorAll('.wbd-day'), function (btn) {
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

        // 每栏一组「« ‹ 标题 › »」：年退 / 月退 / 月进 / 年进。可访问名就是目标月份本身
        // （Intl 生成，零硬编码词条）—— 读屏器读「2025年12月 按钮」，比「上个月」准。
        function monthNav(act, side, glyph, label) {
            var btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'wbd-mnav';
            btn.dataset.mact = act;
            btn.dataset.side = side;
            btn.setAttribute('aria-label', label);
            btn.textContent = glyph;
            return btn;
        }

        function buildMonthBlock(ym, side) {
            var box = document.createElement('div');
            box.className = 'wbd-month';
            box.setAttribute('role', 'group');
            box.setAttribute('aria-label', monthLabel(ym));

            var head = document.createElement('div');
            head.className = 'wbd-mhead';
            head.appendChild(monthNav('prevYear', side, '\u00ab', monthLabel(shiftMonth(ym, -12))));
            head.appendChild(monthNav('prev', side, '\u2039', monthLabel(shiftMonth(ym, -1))));
            var title = document.createElement('span');
            title.className = 'wbd-mtitle';
            title.textContent = monthLabel(ym);
            head.appendChild(title);
            head.appendChild(monthNav('next', side, '\u203a', monthLabel(shiftMonth(ym, 1))));
            head.appendChild(monthNav('nextYear', side, '\u00bb', monthLabel(shiftMonth(ym, 12))));

            box.appendChild(head);
            box.appendChild(buildMonth(ym));
            return box;
        }

        function render() {
            months.innerHTML = '';
            months.appendChild(buildMonthBlock(state.left, 'left'));
            if (monthCount > 1) { months.appendChild(buildMonthBlock(state.right, 'right')); }
            pop.classList.toggle('is-double', monthCount > 1);
            paint();
        }

        function rangeText() {
            var a = state.start ? toISO(state.start) : '';
            var b = state.end ? toISO(state.end) : '';
            if (!a && !b) { return placeholder; }
            if (a && !b) { return a; }
            if (!a && b) { return separator + ' ' + b; }
            return a + ' ' + separator + ' ' + b;
        }

        function writeInput(input, next) {
            if (input.value === next) { return; }
            input.value = next;
            input.dispatchEvent(new Event('input', { bubbles: true }));
            input.dispatchEvent(new Event('change', { bubbles: true }));
        }

        // 快捷列的选中态：当前两端恰好等于某个档位就算命中（不猜、不记忆 —— 手改日期后
        // 自然不再命中，无需另写一条「回落自定义」的分支）。
        function paintPresets() {
            if (!presets) { return; }
            WBUI.each(presets.querySelectorAll('[data-preset]'), function (btn) {
                var r = rangeOf(btn.dataset.preset);
                var hit = !!r && !!state.start && !!state.end && r[0] === toISO(state.start) && r[1] === toISO(state.end);
                btn.classList.toggle('is-active', hit);
                btn.setAttribute('aria-pressed', hit ? 'true' : 'false');
            });
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
            if (presetInput) { presetInput.value = 'custom'; }
            paintPresets();
        }

        // ---- 弹层定位：上限由「元素自身到视口两侧的实际距离」决定 ----

        // 宽度：先看视口能匀出多少（两侧各留 EDGE），够双月才双月，否则退单月。
        // 横向位置：默认与触发器左缘对齐，越界就贴边 —— 绝对定位的参照物是 .date-filter，
        // 它的宽度与视口无关（可能只占卡片里的半行），所以只有 left 的像素值可靠；
        // 早先靠 right:0 翻转，容器比弹层窄时就会把弹层推出屏幕左侧。
        function layout() {
            var rect = trigger.getBoundingClientRect();
            var host = pop.offsetParent || root;
            var hostRect = host.getBoundingClientRect();
            var viewport = global.innerWidth || document.documentElement.clientWidth || 0;
            var size = popMetrics(viewport);
            var width = size.width;
            monthCount = size.months;
            pop.style.width = width + 'px';
            pop.style.maxWidth = Math.max(viewport - EDGE * 2, 0) + 'px';

            var left = Math.round(rect.left - hostRect.left);
            var maxLeft = Math.round(viewport - EDGE - width - hostRect.left);
            var minLeft = Math.round(EDGE - hostRect.left);
            if (left > maxLeft) { left = maxLeft; }
            if (left < minLeft) { left = minLeft; }
            pop.style.left = left + 'px';
            pop.style.right = 'auto';
        }

        // 键盘能把焦点带出两栏之外（连按 ↓ 跨月、快捷区间跨年）：把左栏拉过去，
        // 右栏跟成同一月的下一个月 —— 两栏都在视口里，焦点才不会丢。
        function ensureFocusVisible() {
            if (sameMonth(state.focus, state.left) || sameMonth(state.focus, state.right)) { return; }
            state.left = { y: state.focus.y, m: state.focus.m, d: 1 };
            state.right = shiftMonth(state.left, 1);
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
            setView(anchor, state.end);
            state.focus = anchor;
            layout();
            render();
            paintPresets();
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

        // sync() 写原生 input 会派发 change 事件、同步触发 reload() —— 没有 guard 时，
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
            paintPresets();
            if (!pop.hidden) {
                setView(state.start || today(), state.end);
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

        // side 决定翻哪一栏（按钮自带 data-side）；滚轮 / PageUp 没有 side 时翻焦点所在栏。
        function stepMonth(delta, side) {
            var target = side === 'left' || side === 'right' ? side : sideOf(state.focus);
            var next = shiftMonth(state[target], delta);
            state[target] = next;
            state.focus = { y: next.y, m: next.m, d: Math.min(state.focus.d, daysInMonth(next.y, next.m)) };
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
            // label[for] 点击会把焦点交给视觉隐藏的原生 label 关联框，转给可见触发器。
            trigger.focus();
        }
        from.addEventListener('focus', onTriggerFocus);
        to.addEventListener('focus', onTriggerFocus);
        from.addEventListener('change', reload);
        to.addEventListener('change', reload);

        if (presets) {
            presets.addEventListener('click', function (e) {
                var btn = e.target.closest ? e.target.closest('[data-preset]') : null;
                if (!btn) { return; }
                e.preventDefault();
                var r = rangeOf(btn.dataset.preset);
                if (!r) { return; }
                state.start = parseISO(r[0]);
                state.end = parseISO(r[1]);
                state.hover = null;
                state.focus = state.start;
                sync();
                close();
                trigger.focus();
            });
        }

        var NAV_STEP = { prevYear: -12, prev: -1, next: 1, nextYear: 12 };

        months.addEventListener('click', function (e) {
            var nav = e.target.closest ? e.target.closest('[data-mact]') : null;
            if (nav) {
                e.preventDefault();
                stepMonth(NAV_STEP[nav.dataset.mact] || 0, nav.dataset.side);
                return;
            }
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
                root.classList.remove('wbd');
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
        rangeOf: rangeOf,
        popMetrics: popMetrics,
        closeAll: function () { closeAll(null); }
    };

    // 控件登记：index.js 与 htmx 重扫都会调用。找不到元素时什么都不做（不 return 掉整个扫描），
    // 字段随抽屉进 DOM 的场景由 MutationObserver 的重扫接上。
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
    // 表单 reset 不派发 change：按钮回到初值时触发器必须跟着回到初值。
    document.addEventListener('reset', function (e) {
        var form = e.target;
        if (!form || !form.elements) { return; }
        setTimeout(function () {
            WBUI.each(form.elements, function (el) {
                var host = el.parentNode && el.parentNode.closest ? el.parentNode.closest('[data-wb-daterange]') : null;
                var api = host ? instances.get(host) : null;
                if (api) { api.reload(); }
            });
        }, 0);
    });
})(window);
