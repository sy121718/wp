/* ui/hovercard.js — 行内悬浮明细卡（列表单元格里的「点开/悬停看明细」）。
 *
 * 为什么不复用 .help-pop：那是 position: absolute（theme.css 的 .help{position:relative}），
 * 而列表都包在 .table-scroll（overflow: auto，theme.css 的表头吸顶那段）里 ——
 * 绝对定位的浮层会被这个容器裁掉，鼠标一移到表格中段就只剩半个卡片。
 * 这里把面板克隆到 <body> 下的 fixed 图层，坐标由触发元素的 getBoundingClientRect 现算，
 * 容器裁剪、横向滚动、窗口缩放都不会让它错位（滚动与缩放直接关掉，重开即重新定位）。
 *
 * 面板放 <template> 里而不是 hidden 的 div：模板内容不参与布局、不进可访问性树、
 * 也不会被读屏念一遍 —— 一个 hidden div 仍会出现在可访问性树里，而这张卡的内容
 * 在没打开时不该被念到。
 *
 * 契约（一个触发器 + 一份面板，缺任一个则整个 host 不生效）：
 *   <span data-wb-hover>
 *     <button type="button" class="wb-hover-trigger">…</button>
 *     <template class="wb-hover-panel">…任意 HTML（不能带 <script>）…</template>
 *   </span>
 *
 * 触发与关闭：鼠标进入触发器或面板（面板里可能有链接，所以要能移进去）、键盘聚焦、
 * 触屏点按（点一下开、再点一下关）、Escape、页面滚动或窗口缩放。
 *
 * 不做 init 扫描：全部走 document 级委托，htmx 换入的新行天然生效，不必登记 WBUI.scan。
 */
(function (global) {
    'use strict';

    var HOST = '[data-wb-hover]';
    var TRIGGER = '.wb-hover-trigger';
    var PANEL = 'template.wb-hover-panel';
    var GAP = 6;   // 触发器与卡片之间的间距
    var EDGE = 8;  // 距视口边缘的安全距离
    var CLOSE_DELAY = 120; // 鼠标从触发器移到面板途中的宽限时间

    var layer = null;
    var current = null;
    var hideTimer = 0;

    function ensureLayer() {
        if (layer && layer.parentNode) { return layer; }
        layer = document.createElement('div');
        layer.className = 'wb-hover-layer';
        layer.setAttribute('hidden', '');
        document.body.appendChild(layer);
        return layer;
    }

    function place(trigger, panel) {
        var t = trigger.getBoundingClientRect();
        var p = panel.getBoundingClientRect();
        var left = t.left;
        if (left + p.width > global.innerWidth - EDGE) {
            left = global.innerWidth - EDGE - p.width;
        }
        if (left < EDGE) { left = EDGE; }
        var top = t.bottom + GAP;
        // 下方放不下就翻到上方；上方也放不下（触发器贴着视口边缘）时贴住视口下沿 ——
        // 宁可压住触发元素，也不能让卡片跑出视口（跑出去等于点了没反应）。
        if (top + p.height > global.innerHeight - EDGE) {
            top = t.top - GAP - p.height;
            if (top < EDGE) { top = Math.max(EDGE, global.innerHeight - EDGE - p.height); }
        }
        panel.style.left = Math.round(left) + 'px';
        panel.style.top = Math.round(top) + 'px';
    }

    function close() {
        clearTimeout(hideTimer);
        if (!current) { return; }
        current.host.removeAttribute('data-open');
        if (current.panel.parentNode) { current.panel.parentNode.removeChild(current.panel); }
        current = null;
        if (layer) { layer.setAttribute('hidden', ''); }
    }

    function open(host) {
        var trigger = host.querySelector(TRIGGER);
        var tpl = host.querySelector(PANEL);
        if (!trigger || !tpl) { return; }
        close();
        var panel = document.createElement('div');
        panel.className = 'wb-hover-card';
        panel.setAttribute('role', 'tooltip');
        panel.appendChild(tpl.content.cloneNode(true));
        ensureLayer().appendChild(panel);
        layer.removeAttribute('hidden');
        // 先插入再量尺寸：面板必须在文档里才量得到真实宽高（决定要不要翻转）。
        place(trigger, panel);
        current = { host: host, panel: panel, trigger: trigger };
        host.setAttribute('data-open', '');
    }

    function scheduleClose() {
        clearTimeout(hideTimer);
        hideTimer = setTimeout(close, CLOSE_DELAY);
    }

    function cancelClose() { clearTimeout(hideTimer); }

    function hostOf(node) {
        return node && node.closest ? node.closest(HOST) : null;
    }

    function insidePanel(node) {
        return !!(current && node && current.panel.contains(node));
    }

    document.addEventListener('mouseover', function (e) {
        var host = hostOf(e.target);
        if (host) {
            cancelClose();
            if (!current || current.host !== host) { open(host); }
            return;
        }
        if (insidePanel(e.target)) { cancelClose(); return; }
        if (current) { scheduleClose(); }
    });

    document.addEventListener('focusin', function (e) {
        var host = hostOf(e.target);
        if (host) { open(host); return; }
        if (current && !insidePanel(e.target)) { close(); }
    });

    document.addEventListener('click', function (e) {
        var host = hostOf(e.target);
        if (host) {
            // 触屏没有 hover：点一下开、再点一下关。
            if (current && current.host === host) { close(); } else { open(host); }
            return;
        }
        if (current && !insidePanel(e.target)) { close(); }
    });

    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') { close(); }
    });

    // 坐标是打开那一刻算的：滚动与缩放都会让它失效，直接关掉比跟踪重算更不容易出错
    // （capture 是为了拿到任意滚动容器的滚动，表格自己滚也要关）。
    global.addEventListener('scroll', close, true);
    global.addEventListener('resize', close);

    // 供测试与将来的程序化调用；也方便在控制台里确认这份基座真的加载了。
    global.WBHover = { open: open, close: close, get current() { return current; } };
})(window);
