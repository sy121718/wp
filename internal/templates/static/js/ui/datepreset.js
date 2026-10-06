/* ui/datepreset.js — 「快捷区间」下拉：把「今日 / 本周 / 本月」这类常见区间填进旁边两个 date。
 *
 * 为什么是下拉而不是一排胶囊按钮：胶囊每个占 50~70px，六个就把筛选条撑满一行，
 * 窄屏必然折行；而「选哪个区间」是低频动作，为它常驻六个按钮不划算。
 *
 * 为什么不用服务端算（仪表盘原先的做法是服务端拼 <a href>）：那样每个区间都是一个新
 * URL，点一下要整页刷新，而且链要带着页面上的其它筛选（工程、状态…）一起走 —— 每加一个
 * 筛选参数，服务端拼链的地方就多一处要改。前端算则是就地改两个 date 的值，由表单
 * 本来那次提交带走一切。
 *
 * 口径：**一律按浏览器本地日历**。服务端算「今天」用的是 UTC（见 dashboard_range.go），
 * 对 UTC+8 的用户来说每天有 8 小时窗口里两者差一天 —— 「今日」在用户心里指的是他自己
 * 所在时区的今天，所以这里以本地日历为准。（原胶囊链接的服务端口径在本次改造中被替换，
 * 两处不会再并存。）
 *
 * 区间形状与 dashboard_range.go 对齐：「本周」是**本周一 ~ 今天**（ISO 周，周一起），
 * 不是周一到周日 —— 未来的日期没有数据，把窗口伸过去只会让日均变小。
 *
 * 渐进增强：下拉初始 hidden（模板上写着 hidden），本脚本确认两个 date 都在之后才显示 ——
 * 无 JS 时「选了一个不填日期的下拉」比「没有这个下拉」更让人困惑。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;

    function pad(n) { return n < 10 ? '0' + n : String(n); }

    // 用本地日历字段手拼 ISO，不走 toISOString()：后者先转 UTC，
    // 东八区在 08:00 之前会把日期挪回前一天。
    function iso(dt) {
        return dt.getFullYear() + '-' + pad(dt.getMonth() + 1) + '-' + pad(dt.getDate());
    }

    function today() {
        var now = new Date();
        return new Date(now.getFullYear(), now.getMonth(), now.getDate());
    }

    function shiftDays(dt, days) {
        return new Date(dt.getFullYear(), dt.getMonth(), dt.getDate() + days);
    }

    function startOfMonth(dt) {
        return new Date(dt.getFullYear(), dt.getMonth(), 1);
    }

    // rangeOf 返回 [from, to]（ISO 字符串），key 不认识时返回 null。
    // null 的语义是「自定义」：两个 date 交给用户手选，不在这里猜。
    function rangeOf(key) {
        var t = today();
        switch (key) {
            case 'today':
                return [iso(t), iso(t)];
            case 'yesterday':
                var y = shiftDays(t, -1);
                return [iso(y), iso(y)];
            case 'week':
                // ISO 周：getDay() 是 0=周日，折成 0=周一。
                return [iso(shiftDays(t, -((t.getDay() + 6) % 7))), iso(t)];
            case 'month':
                return [iso(startOfMonth(t)), iso(t)];
            case 'lastMonth':
                var first = new Date(t.getFullYear(), t.getMonth() - 1, 1);
                var last = new Date(t.getFullYear(), t.getMonth(), 0); // 上月末 = 本月 0 日
                return [iso(first), iso(last)];
            case 'days7':
                return [iso(shiftDays(t, -6)), iso(t)];
            case 'days30':
                return [iso(shiftDays(t, -29)), iso(t)];
            case 'year':
                return [iso(new Date(t.getFullYear(), 0, 1)), iso(t)];
            default:
                return null;
        }
    }

    // 回填下拉的选中项：当前两个 date 恰好等于某个区间时就选中它，
    // 否则停在「自定义」。没有这一步的话，带着 ?from=&to= 打开的页面上
    // 下拉永远写着「自定义」，而框里其实是「本周」。
    function syncSelection(sel, from, to) {
        if (!from.value || !to.value) { return; }
        var keys = ['today', 'yesterday', 'week', 'month', 'lastMonth', 'days7', 'days30', 'year'];
        for (var i = 0; i < keys.length; i++) {
            var r = rangeOf(keys[i]);
            if (r && r[0] === from.value && r[1] === to.value) {
                sel.value = keys[i];
                return;
            }
        }
    }

    function enhance(host) {
        var from = document.getElementById(host.getAttribute('data-date-from'));
        var to = document.getElementById(host.getAttribute('data-date-to'));
        if (!from || !to) { return; }
        host.hidden = false;
        syncSelection(host, from, to);

        host.addEventListener('change', function () {
            var r = rangeOf(host.value);
            if (!r) {
                // 自定义：清空两端并聚焦起始端，省掉用户「先点一下再选」。
                from.value = '';
                to.value = '';
                from.focus();
                return;
            }
            from.value = r[0];
            to.value = r[1];
        });

        // 手改任一端就回落到「自定义」：否则下拉写着「本月」而框里是别的区间，
        // 下一次提交会带着这个不一致的显示走。
        [from, to].forEach(function (input) {
            input.addEventListener('change', function () { host.value = 'custom'; });
        });
    }

    function init(scope) {
        WBUI.each(WBUI.$$('[data-date-preset]', scope), function (host) {
            if (!WBUI.markOnce(host, 'DatePreset')) { return; }
            enhance(host);
        });
    }

    WBUI.register(init);
})(window);
