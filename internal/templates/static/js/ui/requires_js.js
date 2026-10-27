/* ui/requires_js.js — 放行「需要 JS 才能用」的写按钮。

 * 背景：写基座（ui/write-base.js）用 fetch 发请求，没有 JS 的浏览器里那些按钮点了
 * 什么都不会发生。所以模板把这类按钮写成 [data-requires-js] hidden disabled ——
 * **无 JS 时不可见也不可点**，由本文件在 JS 起来之后摘掉这三个标记。
 *
 * 为什么需要单独一个文件：这件事原来由已删除的 json_write.js 顺手做（它在启动时
 * 遍历 [data-requires-js]）。基座合并成 write-base.js 时那段没跟过来，而
 * check-write-attributes.sh / write_endpoint_contract_test.go 仍在要求模板写下这个属性 ——
 * 于是全站 34 个模板里的写按钮**永远保持 hidden**：属性齐全、门禁全绿、功能全无。
 * 这个洞靠门禁发现不了（门禁只看模板有没有写属性，不看有没有人读它），
 * 只能靠「模板声明 ↔ 实现」两侧的手工核对。
 *
 * 为什么立即执行而不是 WBUI.register：register 走的是控件扫描（index.js 的
 * MutationObserver），而本文件排在 index.js **之前**加载，登记进 controls 也不会被首帧扫描
 * —— 首屏那批按钮就会一直是 hidden，直到页面上又插入了新节点才被顺带放行。
 * 这里自己 ready + 自己观察新增节点，与控件扫描互不依赖。
 */
(function (global) {
    'use strict';

    function release(root) {
        var nodes = (root || document).querySelectorAll('[data-requires-js]');
        for (var i = 0; i < nodes.length; i++) {
            var el = nodes[i];
            if (el.getAttribute('data-requires-js') === '0') { continue; }
            el.removeAttribute('data-requires-js');
            el.removeAttribute('hidden');
            el.removeAttribute('disabled');
        }
    }

    function start() {
        release(document);
        var MO = global.MutationObserver || global.WebKitMutationObserver;
        if (!MO) { return; }
        // 只关心新增子树：抽屉内容是从 <template> 克隆进来的（drawer.js），
        // 那批按钮在被克隆之前根本不在文档里，首帧的 release 覆盖不到。
        var mo = new MO(function (mutations) {
            for (var i = 0; i < mutations.length; i++) {
                var added = mutations[i].addedNodes;
                for (var j = 0; j < added.length; j++) {
                    var node = added[j];
                    if (node.nodeType !== 1) { continue; }
                    if (node.hasAttribute && node.hasAttribute('data-requires-js')) {
                        release(node.parentNode || document);
                        continue;
                    }
                    if (node.querySelector && node.querySelector('[data-requires-js]')) {
                        release(node);
                    }
                }
            }
        });
        mo.observe(document.documentElement, { childList: true, subtree: true });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', start);
    } else {
        start();
    }
})(window);
