/* ui/_util.js — 控件基座的共用助手。
 *
 * 这一层是给「原始控件」（select / input / tabs / modal …）共用的底座：
 * 它们都需要「DOM 就绪后扫描一遍」「按选择器找元素」「可重复增强（htmx 局部替换后重扫）」。
 * 放在普通脚本里（非 ES module）是因为两个消费方都吃不了 import：
 * 后台页面用 <script src>，前台产物由构建期内联。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});

    // ready DOM 就绪后回调（已就绪则同步执行）。
    WBUI.ready = function (fn) {
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', fn);
        } else {
            fn();
        }
    };

    // each 类数组遍历（NodeList 在老环境没有 forEach）。
    WBUI.each = function (list, fn) {
        Array.prototype.forEach.call(list || [], fn);
    };

    // $$ 查询（scope 缺省 document）。
    WBUI.$$ = function (sel, scope) {
        return (scope || document).querySelectorAll(sel);
    };

    // markOnce 打一次性标记：同一元素不会被二次增强。
    // 反复增强是这类脚本最常见的坑 —— htmx 局部替换后重扫时，
    // 没标记就会在同一元素上叠出第二套菜单/监听。
    WBUI.markOnce = function (el, flag) {
        if (!el || el.dataset['wbui' + flag] === '1') { return false; }
        el.dataset['wbui' + flag] = '1';
        return true;
    };

    // register 登记一个控件增强：index.js 与 htmx 重扫都会调用它。
    WBUI.controls = WBUI.controls || [];
    WBUI.register = function (init) {
        WBUI.controls.push(init);
    };

    // 读取实际 CSS 过渡时长，JS 收尾不再维护第二份毫秒常量。
    WBUI.transitionTime = function (el) {
        var style = global.getComputedStyle(el);
        function times(value) {
            return value.split(',').map(function (part) {
                part = part.trim();
                return (parseFloat(part) || 0) * (part.endsWith('ms') ? 1 : 1000);
            });
        }
        var durations = times(style.transitionDuration);
        var delays = times(style.transitionDelay);
        return Math.max.apply(null, durations.map(function (time, i) { return time + delays[i % delays.length]; }));
    };

    // scan 对给定范围跑一遍全部已登记控件。
    WBUI.scan = function (scope) {
        var errors = [];
        WBUI.controls.forEach(function (init) {
            try {
                init(scope || document);
            } catch (e) {
                // 保留其余控件的初始化，但错误必须可观测，调用方可显示就地提示。
                errors.push(e);
                console.error('[WBUI] 控件初始化失败', e);
                document.dispatchEvent(new CustomEvent('wbui:error', { detail: { error: e, scope: scope } }));
            }
        });
        return errors;
    };
})(window);
