/* ui/preview-zoom.js — 详情页预览的就地放大（点击右栏缩略 → 覆盖层里看大的）。

 * 为什么不用抽屉：抽屉基座（ui/drawer.js）的片段校验**禁止 iframe**，而预览必须是 iframe
 *（详情页是完整文档，自带样式与脚本；塞进 srcdoc 会与后台页面同源、脚本可能互扰）。
 * 放宽那条白名单是为一个预览松安全边界，代价不对等。
 *
 * 为什么不用新标签页（唯一替代方案）：换标签会离开正在编辑的表单，而预览的用法是
 *「扫一眼 → 继续改」。覆盖层大（92vw×88vh）、内层仍按 1440 宽渲染（scale≈0.9，接近 1:1），
 * Esc 或点背景即回到编辑。真要 1:1，覆盖层里仍留了「新标签打开」的出口。
 *
 * 覆盖层里的 iframe 会**再请求一次**预览帧（渲染一次）—— 这是点击才发生的成本，
 * 换来的是「不打断编辑」；不缓存 DOM 副本是为了避免两份预览不同步（保存后要刷新两处）。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});

    var VIEWPORT = 1440;
    var mask = null;
    var onKeydown = null;

    function close() {
        if (!mask) {
            return;
        }
        document.removeEventListener('keydown', onKeydown, true);
        mask.remove();
        mask = null;
        onKeydown = null;
        document.body.style.overflow = '';
    }

    function fitScale(box) {
        var viewport = parseFloat(box.getAttribute('data-preview-viewport')) || VIEWPORT;
        var avail = box.clientWidth;
        if (!avail || viewport <= 0) {
            return 1;
        }
        return Math.min(1, avail / viewport);
    }

    function open(src, title) {
        close();
        mask = document.createElement('div');
        mask.className = 'preview-zoom-mask';
        mask.setAttribute('role', 'dialog');
        mask.setAttribute('aria-modal', 'true');
        mask.setAttribute('aria-label', title || '预览');

        var box = document.createElement('div');
        box.className = 'preview-zoom-box';
        box.setAttribute('data-preview-viewport', String(VIEWPORT));

        var bar = document.createElement('div');
        bar.className = 'preview-zoom-bar';
        var label = document.createElement('span');
        label.textContent = title || '';
        var full = document.createElement('a');
        full.className = 'btn btn-ghost';
        full.href = src;
        full.target = '_blank';
        full.rel = 'noopener';
        full.textContent = '新标签打开（1:1）';
        var shut = document.createElement('button');
        shut.type = 'button';
        shut.className = 'btn btn-ghost';
        shut.textContent = '关闭（Esc）';
        shut.addEventListener('click', close);
        bar.appendChild(label);
        bar.appendChild(full);
        bar.appendChild(shut);

        var frame = document.createElement('iframe');
        frame.className = 'preview-zoom-frame';
        frame.setAttribute('sandbox', '');
        frame.src = src;

        box.appendChild(bar);
        box.appendChild(frame);
        mask.appendChild(box);
        document.body.appendChild(mask);
        document.body.style.overflow = 'hidden';

        // 内层按 1440 宽渲染，外层等比缩放 —— 与右栏缩略同一套做法，
        // 只是容器更大，于是 scale 更接近 1。
        var apply = function () {
            box.style.setProperty('--preview-scale', String(fitScale(box)));
        };
        apply();
        if (typeof ResizeObserver === 'function') {
            new ResizeObserver(apply).observe(box);
        }

        // 点背景关闭（点内容不关）：比整层可点更不容易误关。
        mask.addEventListener('click', function (e) {
            if (e.target === mask) {
                close();
            }
        });
        onKeydown = function (e) {
            if (e.key === 'Escape') {
                e.stopPropagation();
                close();
            }
        };
        document.addEventListener('keydown', onKeydown, true);
        shut.focus();
    }

    WBUI.register(function (scope) {
        WBUI.$$('[data-preview-zoom]', scope).forEach(function (root) {
            var frame = root.querySelector('iframe');
            if (!frame) {
                return;
            }
            // 幂等标记落在**内容节点**（iframe）而不是容器上：htmx 复用容器、只换里面的内容时，
            // 容器级标记会让新的 iframe 失去点击能力（见 internal/templates/CLAUDE.md 的同类教训）。
            if (!WBUI.markOnce(frame, 'PreviewZoom')) {
                return;
            }
            var trigger = function () {
                open(frame.getAttribute('src'), root.getAttribute('data-preview-zoom') || '');
            };
            root.addEventListener('click', trigger);
            // 容器声明了 role="button" + tabindex="0"：那就必须能用键盘触发。
            // 只加 tabindex 不加处理，屏幕阅读器会念「按钮」而按下去没反应 —— 比不可聚焦更糟。
            root.addEventListener('keydown', function (e) {
                if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') {
                    e.preventDefault();
                    trigger();
                }
            });
            root.classList.add('is-zoomable');
        });
    });
})(window);
