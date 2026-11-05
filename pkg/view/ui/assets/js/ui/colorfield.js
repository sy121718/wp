/* ui/colorfield.js — 原始控件：颜色字段（文本框 + 色块取色）。
 *
 * 用法：
 *   <input type="text" name="colors.primary" data-color-field value="#3d444f">
 *
 * 为什么不用 <input type="color"> 直接当表单字段：它没有「未设置」状态 ——
 * 空值会被浏览器补成 #000000，保存一次就把「跟随内置默认」写成黑色。
 * （admin/theme_settings.html 早期注释记过这个坑，于是那一版干脆退成纯文本框，
 *   代价是用户只能手敲 hex。）
 *
 * 这里两者都要：**文本框仍是唯一真值来源**（可留空、可写 rgba()/hsl()/var(--token)），
 * 色块只是取色入口 —— 点开系统取色器 → 写回文本框 → 派发 input/change，
 * 页面既有的收集逻辑（按 input 事件取值）一行都不用改。
 * 留空即「未设置」，色块显示棋盘格 + 斜线，不再有「总得提交一个色值」的问题。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    // colorizable 判断一个值能不能当颜色显示（hex / rgb() / hsl() 交给浏览器定夺；
    // var(--token) 这类合法但解析不出颜色的值返回空串 → 走「未设置」外观）。
    function colorizable(v) {
        v = String(v == null ? '' : v).trim();
        if (!v) { return ''; }
        if (/^#([0-9a-f]{3}|[0-9a-f]{6})$/i.test(v)) { return v; }
        if (/^(rgb|hsl)a?\(/i.test(v)) {
            var probe = document.createElement('span');
            probe.style.color = '';
            probe.style.color = v;
            return probe.style.color ? v : '';
        }
        return '';
    }

    // toHex 给系统取色器一个起点：它只认 #rrggbb（rgba 丢 alpha，其它退回黑色起点）。
    function toHex(v) {
        v = String(v == null ? '' : v).trim();
        if (/^#[0-9a-f]{6}$/i.test(v)) { return v.toLowerCase(); }
        if (/^#([0-9a-f]{3})$/i.test(v)) { return ('#' + v[1] + v[1] + v[2] + v[2] + v[3] + v[3]).toLowerCase(); }
        var m = v.match(/(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/);
        if (m) {
            var h = function (n) {
                var x = Math.max(0, Math.min(255, parseInt(n, 10))).toString(16);
                return x.length === 1 ? '0' + x : x;
            };
            return ('#' + h(m[1]) + h(m[2]) + h(m[3])).toLowerCase();
        }
        return '#000000';
    }

    function enhance(el) {
        if (!WBUI.markOnce(el, 'ColorField')) { return; }

        var wrap = document.createElement('span');
        wrap.className = 'wbc';
        el.parentNode.insertBefore(wrap, el);
        wrap.appendChild(el);

        var swatch = document.createElement('button');
        swatch.type = 'button';
        swatch.className = 'wbc-swatch';
        swatch.setAttribute('aria-label', '选择颜色');
        wrap.appendChild(swatch);

        var native = document.createElement('input');
        native.type = 'color';
        native.className = 'wbc-native';
        native.tabIndex = -1;
        native.setAttribute('aria-hidden', 'true');
        wrap.appendChild(native);

        function sync() {
            var raw = String(el.value || '').trim();
            var css = colorizable(raw);
            // 只改 backgroundColor：色块的棋盘格是 background-image，留着它才能
            // 同时表达「半透明色」与「未设置」。
            swatch.style.backgroundColor = css || '';
            // 同一个颜色也交给输入框（ui.css 用它在左侧画一条色带）：
            // 光看 hex 字符串判断不出「偏亮还是偏暗」，色块 + 色带两处呼应才直观。
            wrap.style.setProperty('--wbc-color', css || 'transparent');
            wrap.classList.toggle('is-empty', !css);
            swatch.title = css ? raw
                : (raw ? '当前值不是具体颜色（' + raw + '），点击可从取色器选一个' : '未设置（跟随内置默认），点击取色');
            native.value = toHex(css || raw);
        }

        el.addEventListener('input', sync);
        el.addEventListener('change', sync);
        swatch.addEventListener('click', function () { native.click(); });

        // 系统取色器：input 是拖动中的连续值，change 是确认值 —— 两个都写回并派发，
        // 让页面原有的收集逻辑（含「已改动」判断）保持有效。
        function apply() {
            if (el.value === native.value) { return; }
            el.value = native.value;
            sync();
            el.dispatchEvent(new Event('input', { bubbles: true }));
            el.dispatchEvent(new Event('change', { bubbles: true }));
        }
        native.addEventListener('input', apply);
        native.addEventListener('change', apply);

        sync();
        // 再同步一次：页面自己的回填脚本可能在本控件扫描之后才跑
        //（程序化 el.value = x 不触发事件，Observer 也看不到 —— value 是 property 不是 attribute），
        // 不补这一下会出现「文本框填了 #3d444f、色块还是旧颜色」。
        global.setTimeout(sync, 0);
    }

    WBUI.register(function (scope) {
        WBUI.each(WBUI.$$('input[data-color-field]', scope), enhance);
    });
})(window);
