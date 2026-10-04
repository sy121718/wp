/* ui/modal.js — 原始控件：模态弹窗（<dialog> 承载）。
 *
 * 为什么它属于基座：媒体库的分类弹窗与上传弹窗各自维护「弹窗 div + 遮罩 div + hidden 开关」，
 * 于是三件事一起缺：没有焦点陷阱、没有 Esc 关闭、关闭后焦点回不到触发按钮；遮罩与弹窗是两个
 * 独立元素，必须成对显隐，漏一处就是「遮罩还在、框没了」。这些不是业务逻辑，
 * 是「一个弹窗该有的样子」—— 与确认框同一类，所以跟 confirm.js 并列在基座里。
 *
 * 用法（业务逻辑留在页面脚本，挂 wbui:modal-open / wbui:modal-close 事件）：
 *   <button type="button" data-modal-open="ml-cat-modal">新建分类</button>
 *   <dialog class="wb-modal" id="ml-cat-modal" data-modal>
 *     <div class="wb-modal-inner">
 *       <header class="wb-modal-head"><strong>标题</strong>
 *         <button type="button" class="wb-modal-x" data-modal-close>×</button></header>
 *       <div class="wb-modal-body">…</div>
 *       <footer class="wb-modal-foot">…</footer>
 *     </div>
 *   </dialog>
 *
 * 可选属性：
 *   data-modal-static        点遮罩不关闭（危险操作 / 必填表单）
 *   data-modal-nokeyboard    Esc 不关闭
 *   data-modal-autofocus     打开后聚焦的选择器（缺省按可聚焦顺序取第一个）
 *   data-modal-auto-open     扫描到就地打开（服务端渲染的「结果弹窗」用：整页渲染完
 *                            直接弹出来，不需要页面再写一句 JS 去开它）。打开后属性被摘掉，
 *                            后续 htmx 扫描不会把用户刚关掉的窗重新弹开。
 *
 * 代码调用：
 *   WBUI.modal.open('ml-cat-modal')     WBUI.modal.close('ml-cat-modal')
 *
 * 事件（自 dialog 冒泡，可挂在 document 上）：
 *   wbui:modal-open / wbui:modal-close   detail { dialog, id }
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    // get 按 id 或按元素取弹窗。
    function get(ref) {
        if (!ref) { return null; }
        if (typeof ref === 'string') { return document.getElementById(ref); }
        return ref.tagName ? ref : null;
    }

    function emit(dlg, kind) {
        dlg.dispatchEvent(new CustomEvent('wbui:modal-' + kind, {
            bubbles: true,
            detail: { dialog: dlg, id: dlg.id || '' }
        }));
    }

    // focusFirst 打开后落焦。
    // .wbs-trigger 排在原生 select 之前：原生 select 被基座换成自绘触发器后只剩 1×1 的
    // 隐藏元素，聚焦到它上面对用户等于没有焦点（与 ui/index.js 里那条「先扫描再聚焦」同源）。
    function focusFirst(dlg) {
        var sel = dlg.getAttribute('data-modal-autofocus');
        var el = sel ? dlg.querySelector(sel) : null;
        if (!el) { el = dlg.querySelector('.wbs-trigger'); }
        if (!el) {
            el = dlg.querySelector('input:not([type=hidden]):not([disabled]), textarea, [href], [tabindex]:not([tabindex="-1"])');
        }
        if (!el) { el = dlg.querySelector('button, select'); }
        if (el && el.focus) { el.focus(); } else if (dlg.focus) { dlg.focus(); }
    }

    // enhance 一次性初始化：markOnce 防 htmx 重扫时叠出第二套监听。
    function enhance(dlg) {
        if (!WBUI.markOnce(dlg, 'Modal')) { return; }
        // 点遮罩关闭：内容包在 .wb-modal-inner 里，所以 target 恰好是 dialog 自身即为遮罩区
        //（弹窗本体不设 padding，见 ui.css）。
        dlg.addEventListener('click', function (e) {
            if (e.target === dlg && !dlg.hasAttribute('data-modal-static')) { close(dlg); }
        });
        // Esc 关闭：**自己处理，不依赖浏览器原生行为**。
        // <dialog> 的原生 Esc 是浏览器的 default action —— 合成键盘事件（CDP/自动化）
        // 不产生 default action，实测原生 <dialog> 在自动化环境里按 Esc 也不关。
        // 依赖它的结果是「真实用户能关、自动化测不到、真出问题时也没有兜底」。
        // 这里显式接管，并按「最上层优先」stopPropagation，避免同时开着的抽屉被一起关掉。
        dlg.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape' || dlg.hasAttribute('data-modal-nokeyboard')) { return; }
            e.preventDefault();
            e.stopPropagation();
            close(dlg);
        });
        // 原生 cancel 只剩一个作用：静态/禁键盘弹窗要把它拦下来。
        dlg.addEventListener('cancel', function (e) {
            if (dlg.hasAttribute('data-modal-nokeyboard')) { e.preventDefault(); }
        });
        dlg.addEventListener('close', function () {
            dlg.classList.remove('is-open');
            var opener = dlg.__wbuiOpener;
            dlg.__wbuiOpener = null;
            // 焦点还给触发者：否则关闭后焦点掉回 body，键盘用户要从头 Tab 一遍。
            if (opener && opener.isConnected && opener.focus) { opener.focus(); }
            emit(dlg, 'close');
        });
    }

    function open(ref, opener) {
        var dlg = get(ref);
        if (!dlg) { return null; }
        enhance(dlg);
        if (dlg.hasAttribute('open')) { return dlg; }
        dlg.__wbuiOpener = opener || (document.activeElement !== document.body ? document.activeElement : null);
        // 先扫描再聚焦：扫描会把弹窗里的原生 select 换成自绘触发器。
        if (WBUI.scan) { WBUI.scan(dlg); }
        if (typeof dlg.showModal === 'function') { dlg.showModal(); } else { dlg.setAttribute('open', ''); }
        dlg.classList.add('is-open');
        focusFirst(dlg);
        emit(dlg, 'open');
        return dlg;
    }

    function close(ref) {
        var dlg = get(ref);
        if (!dlg || !dlg.hasAttribute('open')) { return; }
        if (typeof dlg.close === 'function') {
            dlg.close();
            return;
        }
        dlg.removeAttribute('open');
        dlg.classList.remove('is-open');
        emit(dlg, 'close');
    }

    // 声明式触发走 document 委托：触发按钮可能是 htmx 换进来的，逐个绑定必漏。
    document.addEventListener('click', function (e) {
        if (!e.target || !e.target.closest) { return; }
        var opener = e.target.closest('[data-modal-open]');
        if (opener) {
            e.preventDefault();
            open(opener.getAttribute('data-modal-open'), opener);
            return;
        }
        var closer = e.target.closest('[data-modal-close]');
        if (closer) {
            e.preventDefault();
            close(closer.closest('dialog') || closer.getAttribute('data-modal-close'));
        }
    });

    // autoOpen 打开声明了 data-modal-auto-open 的弹窗（用一次即摘属性）。
    // 摘属性是必须的：WBUI.scan 在每次 htmx:afterSwap 后都会重跑这个注册回调，
    // 留着属性等于「用户关一次、下一次局部刷新又给他弹回来」。
    function autoOpen(dlg) {
        if (!dlg.hasAttribute('data-modal-auto-open')) { return; }
        dlg.removeAttribute('data-modal-auto-open');
        open(dlg);
    }

    WBUI.register(function (scope) {
        WBUI.each(WBUI.$$('[data-modal]', scope || document), function (dlg) {
            enhance(dlg);
            autoOpen(dlg);
        });
    });

    WBUI.modal = { open: open, close: close };
})(window);
