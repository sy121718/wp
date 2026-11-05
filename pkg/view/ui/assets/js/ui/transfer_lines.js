/* ui/transfer_lines.js — 调拨建单的明细行控件（选源仓 → 拉该仓可售的货 → 逐行填件数）。

 * 为什么要有它：表单没有嵌套数组语法（实测：lines[0].stockId=x 绑不出 []TransferLineReq），
 * 所以行必须由前端渲染成 lines[i].stockId / lines[i].quantity 的**扁平键**，DTO 的
 * BindForm 才收得到。手写三个固定行不行：一个调拨单可能有十几行货。
 *
 * 候选来自 /api/inventory/transfer/stock-options?warehouseId=…（只回可售 > 0 的行）：
 * 摆一条可售为 0 的货，选中之后必然报「库存不足」—— 那不是信息更全，是把失败推迟到点击之后。
 *
 * 换源仓时清空已选行：留着上一仓的 stockId 会让提交时报「这条货不在源仓」，
 * 而人看上去什么都没做错（他只是换了仓）。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});

    // rowTemplate 一行明细的骨架。data-index 由 ensureIndex 填，不在这里写死 ——
    // 删行之后下标要重排，写死会让两行撞同一个 index（服务端收成一行）。
    var ROW_HTML = [
        '<div class="transfer-line" data-transfer-line>',
        '  <select class="form-select" data-line-stock></select>',
        '  <input class="form-input" type="number" min="1" step="1" inputmode="numeric" data-line-qty>',
        '  <button type="button" class="btn btn-sm btn-ghost" data-line-remove aria-label="remove">×</button>',
        '</div>'
    ].join('');

    function lineEls(root) {
        var body = root.querySelector('[data-transfer-lines-body]');
        return body ? Array.prototype.slice.call(body.querySelectorAll('[data-transfer-line]')) : [];
    }

    // ensureIndex 按当前顺序重排 name（lines[0].stockId / lines[0].quantity …）。
    //
    // 每次都全量重排而不是只补新的：删掉中间一行后，若只重排被删行之后的部分，
    // 前面的下标还停在旧值 —— 两行撞号时服务端把第二行当成「重复 StockID」拒掉。
    function ensureIndex(root) {
        var name = root.getAttribute('data-transfer-lines-name') || 'lines';
        lineEls(root).forEach(function (line, i) {
            var sel = line.querySelector('[data-line-stock]');
            var qty = line.querySelector('[data-line-qty]');
            if (sel) { sel.name = name + '[' + i + '].stockId'; }
            if (qty) { qty.name = name + '[' + i + '].quantity'; }
        });
    }

    function fillOptions(root, options) {
        root._options = options || [];
        lineEls(root).forEach(function (line) {
            var sel = line.querySelector('[data-line-stock]');
            if (!sel) { return; }
            var keep = sel.value;
            sel.innerHTML = '<option value="">选择货品</option>';
            root._options.forEach(function (o) {
                var opt = document.createElement('option');
                opt.value = o.stockId;
                // 可售数量写在文案里：选一条只有 2 件可售的货去搬 10 件，提交时必失败。
                opt.textContent = (o.name ? o.name + ' · ' : '') + o.skuCode + '（可售 ' + o.availableQuantity + '）';
                if (o.stockId === keep) { opt.selected = true; }
                sel.appendChild(opt);
            });
        });
    }

    function loadOptions(root, warehouseId) {
        if (!warehouseId) {
            fillOptions(root, []);
            return;
        }
        var url = root.getAttribute('data-transfer-options-url');
        if (!url) { return; }
        var sep = url.indexOf('?') >= 0 ? '&' : '?';
        fetch(url + sep + 'warehouseId=' + encodeURIComponent(warehouseId), { credentials: 'same-origin' })
            .then(function (r) { return r.json(); })
            .then(function (j) {
                var data = (j && j.data) || [];
                fillOptions(root, data);
                if (!data.length) {
                    var empty = root.querySelector('[data-transfer-lines-empty]');
                    if (empty) { empty.hidden = false; }
                }
            })
            .catch(function () {
                // 载入失败不静默：空下拉会让人以为「这个仓没货」，而那与事实相反。
                fillOptions(root, []);
                var empty = root.querySelector('[data-transfer-lines-empty]');
                if (empty) {
                    empty.hidden = false;
                    empty.textContent = '可调货品载入失败，请刷新页面重试';
                }
            });
    }

    function addLine(root) {
        var body = root.querySelector('[data-transfer-lines-body]');
        if (!body) { return; }
        var wrap = document.createElement('div');
        wrap.innerHTML = ROW_HTML;
        var line = wrap.firstElementChild;
        body.appendChild(line);
        var sel = line.querySelector('[data-line-stock]');
        if (sel && root._options) {
            sel.innerHTML = '<option value="">选择货品</option>';
            root._options.forEach(function (o) {
                var opt = document.createElement('option');
                opt.value = o.stockId;
                opt.textContent = (o.name ? o.name + ' · ' : '') + o.skuCode + '（可售 ' + o.availableQuantity + '）';
                sel.appendChild(opt);
            });
        }
        ensureIndex(root);
    }

    WBUI.register(function (scope) {
        WBUI.$$('[data-transfer-lines]', scope).forEach(function (root) {
            if (!WBUI.markOnce(root, 'TransferLines')) { return; }
            var add = root.querySelector('[data-transfer-line-add]');
            if (add) {
                add.addEventListener('click', function () { addLine(root); });
            }
            root.addEventListener('click', function (e) {
                var rm = e.target && e.target.closest ? e.target.closest('[data-line-remove]') : null;
                if (!rm) { return; }
                var line = rm.closest('[data-transfer-line]');
                if (line) { line.remove(); }
                // 一行都不剩会让「至少要有一行明细」这条服务端判据在提交后才出现 ——
                // 删到最后一行时补一行空白，让人看到「这里要填东西」。
                if (!lineEls(root).length) { addLine(root); }
                ensureIndex(root);
            });

            // 源仓变化：清空已选行（上一仓的 stockId 在别的仓不存在）再拉候选。
            var form = root.closest('form');
            var from = form ? form.querySelector('[data-transfer-from]') : null;
            if (from) {
                from.addEventListener('change', function () {
                    var body = root.querySelector('[data-transfer-lines-body]');
                    if (body) { body.innerHTML = ''; }
                    var empty = root.querySelector('[data-transfer-lines-empty]');
                    if (empty) { empty.hidden = true; }
                    addLine(root);
                    loadOptions(root, from.value);
                });
            }
            ensureIndex(root);
        });
    });
})(window);
