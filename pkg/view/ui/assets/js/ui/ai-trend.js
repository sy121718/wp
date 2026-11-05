/* ai-trend.js — 「Token 用量趋势」折线图的几何计算（服务端只交事实）。
 *
 * 服务端给的是逐日 token 数组与峰值：
 *
 *   <svg data-trend-svg data-trend-peak="峰值">
 *   <li data-trend-series="序号" data-values="12 34 0 7 ...">
 *
 * 这里按视口尺寸把它们换成坐标、连成折线、选中配色槽位。换图宽、换点数、
 * 换主题配色都不必改后端 —— 这是显示格式，本来就该在前端。
 *
 * 没有任何数据和没有 JS 时这一块保持空白：下面的图例与起止日期仍然可读，
 * 不会出现「空白画布 + 一句都看不到」的死页。
 */
(function (global) {
    'use strict';

    var ui = global.WBUI = global.WBUI || {};
    if (ui.aiTrend) { return; }

    var SVG_NS = 'http://www.w3.org/2000/svg';

    // 与服务端 viewBox 保持一致（sessions.html 的 viewBox="0 0 1000 200"）。
    var VIEW_W = 1000;
    var VIEW_H = 200;
    var PAD_X = 6;
    var PAD_Y = 12;

    function num(v) {
        var n = Number(v);
        return isFinite(n) ? n : 0;
    }

    // valuesOf 读 data-values：空格分隔的逐日 token 数。
    // 空串是「这段窗口一次都没有」，返回空数组而不是 [0] —— 两者画出来不一样。
    function valuesOf(raw) {
        var s = String(raw == null ? '' : raw).trim();
        if (s === '') { return []; }
        return s.split(/\s+/).map(num);
    }

    // pointsOf 把逐日值换成 SVG 坐标串：横轴按天等距，纵轴按峰值归一到画布高度。
    // 峰值 <= 0（整段窗口零用量）时所有点贴在基线上，线是平的 —— 这是真实情况，不是异常。
    function pointsOf(values, peak) {
        var n = values.length;
        if (n === 0) { return []; }
        var top = num(peak);
        var out = [];
        for (var i = 0; i < n; i++) {
            var x = n === 1 ? VIEW_W / 2 : PAD_X + Math.round(i * (VIEW_W - 2 * PAD_X) / (n - 1));
            var y = VIEW_H - PAD_Y;
            if (top > 0) {
                y = VIEW_H - PAD_Y - Math.round(num(values[i]) * (VIEW_H - 2 * PAD_Y) / top);
            }
            out.push([x, y]);
        }
        return out;
    }

    function el(name) { return global.document.createElementNS(SVG_NS, name); }

    // render 按页面上已有的 data-trend-series 重画整张图。可重复调用（幂等）。
    function render(svg) {
        if (!svg) { return false; }
        var legend = svg.closest ? svg.closest('.chart-trend') : null;
        var host = global.document.querySelector('[data-trend-legend]');
        if (!host) { return false; }
        if (legend) { /* closest 只是为了让桩也走到同一条路径 */ }

        var peak = num(svg.getAttribute('data-trend-peak'));
        var rows = host.querySelectorAll('[data-trend-series]');

        // 重画先清空：重复调用不会叠线。
        while (svg.firstChild) { svg.removeChild(svg.firstChild); }

        for (var i = 0; i < rows.length; i++) {
            var pts = pointsOf(valuesOf(rows[i].getAttribute('data-values')), peak);
            if (pts.length === 0) { continue; }

            var color = 'var(--chart-c' + (Number(rows[i].getAttribute('data-trend-series')) % 8 + 1) + ')';

            if (pts.length === 1) {
                // 单点用方块而不是圆：viewBox 被 preserveAspectRatio="none" 横向拉伸，
                // 圆会被拉成椭圆（宽高比随容器变），方块只是变胖一点。
                var rect = el('rect');
                rect.setAttribute('class', 'chart-trend-point');
                rect.setAttribute('x', pts[0][0]);
                rect.setAttribute('y', pts[0][1]);
                rect.setAttribute('width', 14);
                rect.setAttribute('height', 14);
                rect.setAttribute('rx', 3);
                rect.setAttribute('transform', 'translate(-7,-7)');
                rect.setAttribute('style', 'fill: ' + color);
                svg.appendChild(rect);
                continue;
            }

            var line = el('polyline');
            line.setAttribute('class', 'chart-trend-line');
            line.setAttribute('points', pts.map(function (p) { return p[0] + ',' + p[1]; }).join(' '));
            line.setAttribute('style', 'stroke: ' + color);
            svg.appendChild(line);
        }
        return true;
    }

    function renderAll() {
        var svgs = global.document.querySelectorAll('[data-trend-svg]');
        for (var i = 0; i < svgs.length; i++) { render(svgs[i]); }
        return svgs.length;
    }

    ui.aiTrend = { render: render, renderAll: renderAll, valuesOf: valuesOf, pointsOf: pointsOf };

    if (global.document && global.document.querySelectorAll) {
        renderAll();
    }
})(typeof window !== 'undefined' ? window : globalThis);
