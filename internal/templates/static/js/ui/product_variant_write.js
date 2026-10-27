/**
 * 商品编辑页的变体清单 + 每行的「仓库明细」（docs/14 §8 的「预览—保存」模型 + B8）。
 *
 * 四条规则决定这段脚本的形态：
 * 1. 事件用 document 级委托 —— 抽屉内容是从 <template> 克隆进 DOM 的，逐个绑监听会在
 *    抽屉关闭时失效；委托对克隆前后的元素一视同仁。
 * 2. 「生成」是一次**不落库**的取数：POST 到 /admin/products/variant/preview，把返回的行
 *    按清单行的同一种结构（<template data-variant-row-tpl>）追加进 DOM。
 * 3. 「保存」交给写基座（ui/write-base.js）：清单行不是具名控件，基座收不到它们，
 *    因此这里注册一个 data-write-builder —— 把清单如实组装成 rows；组合合法性与唯一性
 *    一律由服务端重算（前端提交的形状不作数）。
 * 4. 仓库明细（B8）**跟着变体行走**：每个变体行后面跟一个隐藏的明细行，
 *    打开时把该变体的货行渲染进面板。提交时每个变体的 stocks 由面板 DOM 组装 ——
 *    嵌套数组没有表单语法，只能走 builder（同第 3 条）。
 *
 * 文案一律从 data-msg-* / data-write-* / 面板里的 aria-label 取（服务端按当前语言渲染），
 * 脚本里不写死文案。清单全页唯一（一页只编辑一个商品），查询都用 document 作用域。
 */
(function (global) {
    'use strict';

    function variantListBody() {
        return global.document.querySelector('[data-variant-list]');
    }

    function variantRowEls() {
        return Array.prototype.slice.call(global.document.querySelectorAll('[data-variant-row]'));
    }

    // stocksRowOf 取某个变体行后面那一行仓库明细（结构见 product_edit.html 的 data-variant-stocks-row）。
    function stocksRowOf(tr) {
        var next = tr ? tr.nextElementSibling : null;
        return next && next.hasAttribute('data-variant-stocks-row') ? next : null;
    }

    // 清单里每行的 option_values（规范化 JSON 文本）：原样回传给预览接口做去重判据。
    function variantListOptionValues() {
        var out = [];
        variantRowEls().forEach(function (tr) {
            var raw = tr.getAttribute('data-option-values');
            if (raw) { out.push(raw); }
        });
        return out;
    }

    // —— 仓库明细：候选货 → 每行的下拉 ——

    // catalogOptions 把整页一份的候选（<template data-stock-catalog>）复制成新节点。
    // 按 template 取而不是各页内联候选：一个 200 变体的商品按行渲候选会渲出几万个 <option>。
    function catalogOptions() {
        var tpl = global.document.querySelector('[data-stock-catalog]');
        if (!tpl || !tpl.content) { return []; }
        return Array.prototype.slice.call(tpl.content.querySelectorAll('optgroup'));
    }

    function fillStockSelect(panel) {
        var sel = panel.querySelector('[data-stock-add-select]');
        if (!sel || sel.getAttribute('data-filled') === '1') { return; }
        var groups = catalogOptions();
        if (!groups.length) { return; }
        groups.forEach(function (g) { sel.appendChild(g.cloneNode(true)); });
        sel.setAttribute('data-filled', '1');
    }

    // stockPanelOf 取「某个变体行」的明细面板（不存在则返回 null）。
    function stockPanelOf(tr) {
        var row = stocksRowOf(tr);
        return row ? row.querySelector('[data-stock-panel]') : null;
    }

    // stockRowHTML 一条明细行的骨架。下标由 ensureStockIndex 全量重排，不在这里写死 ——
    // 删掉中间一行之后，写死的下标会让两行撞号（服务端按重复拒掉）。
    function stockRowHTML(opt, i18n) {
        // 不跟踪（无限）的行数量框留空 + 提示「无限」：填数字等于把它改成跟踪并校准到该数，
        // 留空 = 一动不动。表单里「空」与「0」必须是两件事（0 = 这批货卖光了）。
        var tracked = opt.getAttribute('data-track') === '1';
        var qty = tracked ? (opt.getAttribute('data-quantity') || '0') : '';
        var name = opt.getAttribute('data-name') || opt.getAttribute('data-sku-code') || '';
        var image = opt.getAttribute('data-image') || '';
        var row = global.document.createElement('div');
        row.className = 'stock-row';
        row.setAttribute('data-stock-row', '');
        row.setAttribute('data-stock-id', opt.getAttribute('data-stock-id') || '');
        row.setAttribute('data-warehouse-id', opt.getAttribute('data-warehouse-id') || '');
        row.setAttribute('data-sku-code', opt.getAttribute('data-sku-code') || '');
        var html = '';
        if (image) {
            html += '<img class="stock-thumb" src="' + image + '" alt="" loading="lazy">';
        }
        html += '<span class="stock-name">' + name + '</span>';
        html += '<span class="stock-code">' + (opt.getAttribute('data-sku-code') || '') + '</span>';
        html += '<input type="number" class="form-input stock-qty" min="0" step="1" inputmode="numeric" data-stock-qty value="' + qty + '"' +
            (tracked ? '' : ' placeholder="' + (i18n.unlimited || '') + '" title="' + (i18n.untracked || '') + '"') +
            ' aria-label="' + (i18n.qtyLabel || '') + '">';
        html += '<button type="button" class="btn btn-sm btn-ghost" data-stock-remove aria-label="' + (i18n.remove || '') + '">×</button>';
        row.innerHTML = html;
        return row;
    }

    function ensureStockGroup(panel, opt) {
        var groups = panel.querySelector('[data-stock-groups]');
        if (!groups) { return null; }
        var warehouseID = opt.getAttribute('data-warehouse-id') || '';
        var found = groups.querySelector('[data-stock-group][data-warehouse-id="' + warehouseID + '"]');
        if (found) { return found; }
        var group = global.document.createElement('div');
        group.className = 'stock-group';
        group.setAttribute('data-stock-group', '');
        group.setAttribute('data-warehouse-id', warehouseID);
        group.setAttribute('data-warehouse-code', opt.getAttribute('data-warehouse-code') || '');
        var head = global.document.createElement('div');
        head.className = 'stock-group-head';
        // 组头的仓名从 optgroup 的 label 取（服务端已按「仓名（仓码）」拼好）。
        var label = '';
        var og = opt.closest('optgroup');
        if (og && og.getAttribute('label')) { label = og.getAttribute('label'); }
        head.innerHTML = '<span class="stock-group-name">' + (label || warehouseID) + '</span>';
        var rows = global.document.createElement('div');
        rows.className = 'stock-rows';
        rows.setAttribute('data-stock-rows', '');
        group.appendChild(head);
        group.appendChild(rows);
        groups.appendChild(group);
        return group;
    }

    function toggleStockEmpty(panel) {
        var empty = panel.querySelector('[data-stock-empty]');
        if (!empty) { return; }
        empty.hidden = !!panel.querySelector('[data-stock-row]');
    }

    function stocksI18n(panel) {
        return {
            qtyLabel: panel.getAttribute('data-i18n-qty') || '',
            untracked: panel.getAttribute('data-i18n-untracked') || '',
            unlimited: panel.getAttribute('data-i18n-unlimited') || '',
            remove: panel.getAttribute('data-i18n-remove') || '',
            duplicate: panel.getAttribute('data-i18n-duplicate') || ''
        };
    }

    function addStockRow(panel, opt) {
        var group = ensureStockGroup(panel, opt);
        if (!group) { return; }
        var rows = group.querySelector('[data-stock-rows]');
        var warehouseID = opt.getAttribute('data-warehouse-id') || '';
        var sku = opt.getAttribute('data-sku-code') || '';
        var duplicated = Array.prototype.some.call(panel.querySelectorAll('[data-stock-row]'), function (el) {
            return el.getAttribute('data-warehouse-id') === warehouseID && el.getAttribute('data-sku-code') === sku;
        });
        if (duplicated) {
            // 同一个仓同一个 SKU 在库里只有一行：重复添加在服务端会被拒（ErrVariantStockDuplicated），
            // 在客户端就把它挡住，并说清怎么办 —— 改数量即可，不用再加一行。
            var tip = stocksI18n(panel).duplicate;
            if (tip && global.WBUI && global.WBUI.toast) { global.WBUI.toast(tip, { type: 'error' }); }
            return;
        }
        rows.appendChild(stockRowHTML(opt, stocksI18n(panel)));
        toggleStockEmpty(panel);
    }

    // —— 清单构造 ——

    // stocksOf 读一个变体行后面的明细行，组装该变体的 stocks。
    //
    // 面板 data-stocks-known=0 时**不提交** stocks（返回 null）：那表示服务端这次没读到明细，
    // 提交一个空数组等于告诉服务端「把它的关联全删掉」—— 一次读故障就把用户的数据裁没了。
    //
    // 折叠状态也照提交：面板内容就是服务端渲染的那份真相，没编辑过时提交等于原样回传
    //（服务端按目标值比较，数量没变就不写库）。若只在展开时提交，「删了一条又收起」
    // 的改动就会静默丢失。
    function stocksOf(tr) {
        var row = stocksRowOf(tr);
        if (!row) { return null; }
        var panel = row.querySelector('[data-stock-panel]');
        if (!panel || panel.getAttribute('data-stocks-known') !== '1') { return null; }
        var out = [];
        Array.prototype.forEach.call(panel.querySelectorAll('[data-stock-row]'), function (el) {
            var qty = el.querySelector('[data-stock-qty]');
            var raw = qty ? String(qty.value).trim() : '';
            // 空 = 不动（服务端按目标值比较，不跟踪行当前值恒 0，于是这一行整行跳过）。
            // 只有真的写了数字才提交该数字 —— 空与 0 在作数上不同。
            out.push({
                stockId: el.getAttribute('data-stock-id') || '',
                warehouseId: el.getAttribute('data-warehouse-id') || '',
                skuCode: el.getAttribute('data-sku-code') || '',
                quantity: raw === '' ? 0 : (parseInt(raw, 10) || 0)
            });
        });
        return out;
    }

    // variantRows 把清单 DOM 组装成服务端的 rows（顺序即期望顺序）：
    // variantId 为空 = 新增行；skuCode 是用户可编辑的那一格；optionValues 原样回传；
    // stocks 只在明细行被打开过（或已渲染出内容）时带上 —— 见 stocksOf。
    function variantRows() {
        var rows = [];
        variantRowEls().forEach(function (tr) {
            var sku = tr.querySelector('[data-variant-sku]');
            var opts = {};
            try { opts = JSON.parse(tr.getAttribute('data-option-values') || '{}') || {}; } catch (err) { opts = {}; }
            var row = {
                variantId: tr.getAttribute('data-variant-id') || '',
                skuCode: sku ? sku.value : '',
                optionValues: opts
            };
            var stocks = stocksOf(tr);
            if (stocks) { row.stocks = stocks; }
            rows.push(row);
        });
        return rows;
    }

    function appendVariantRows(rows) {
        var body = variantListBody();
        var tpl = global.document.querySelector('[data-variant-row-tpl]');
        if (!body || !tpl) { return; }
        rows.forEach(function (row) {
            var frag = tpl.content.cloneNode(true);
            var tr = frag.querySelector('tr');
            if (!tr) { return; }
            tr.setAttribute('data-option-values', JSON.stringify(row.optionValues || {}));
            var spec = tr.querySelector('[data-cell-spec]');
            if (spec) { spec.textContent = row.spec || ''; }
            var sku = tr.querySelector('[data-variant-sku]');
            if (sku) { sku.value = row.skuCode || ''; }
            body.appendChild(frag);
            var added = body.lastElementChild;
            if (added) { enhanceStocksRow(added); }
        });
        hideVariantEmptyRow();
    }

    // 空态行：有行时隐藏。hidden 之外再给一次行内 display —— 页面样式里若给行设过
    // display，UA 的 [hidden] 规则会被盖掉（.btn 与 [hidden] 的同一个坑）。
    function hideVariantEmptyRow() {
        var body = variantListBody();
        if (!body) { return; }
        var empty = body.querySelector('[data-variant-empty]');
        if (!empty) { return; }
        var hasRow = !!body.querySelector('[data-variant-row]');
        if (hasRow) {
            empty.hidden = true;
            empty.style.display = 'none';
        } else {
            empty.hidden = false;
            empty.style.display = '';
        }
    }

    function setGenerateStatus(text) {
        global.document.querySelectorAll('[data-generate-status]').forEach(function (box) {
            box.textContent = text || '';
        });
    }

    global.document.addEventListener('click', function (e) {
        var remove = e.target.closest('[data-variant-remove]');
        if (remove) {
            var tr = remove.closest('[data-variant-row]');
            if (tr) {
                var stocksRow = stocksRowOf(tr);
                if (stocksRow) { stocksRow.remove(); }
                tr.remove();
            }
            hideVariantEmptyRow();
            return;
        }
        var toggle = e.target.closest('[data-variant-stocks-toggle]');
        if (toggle) {
            var owner = toggle.closest('[data-variant-row]');
            var row = stocksRowOf(owner);
            if (!row) { return; }
            row.hidden = !row.hidden;
            toggle.setAttribute('aria-expanded', row.hidden ? 'false' : 'true');
            if (!row.hidden) {
                var panel = row.querySelector('[data-stock-panel]');
                if (panel) {
                    fillStockSelect(panel);
                    toggleStockEmpty(panel);
                }
            }
            return;
        }
        var addStock = e.target.closest('[data-stock-add]');
        if (addStock) {
            var stockPanel = addStock.closest('[data-stock-panel]');
            var sel = stockPanel ? stockPanel.querySelector('[data-stock-add-select]') : null;
            var opt = sel && sel.selectedIndex >= 0 ? sel.options[sel.selectedIndex] : null;
            if (!opt || !opt.value) { return; }
            addStockRow(stockPanel, opt);
            sel.selectedIndex = 0;
            return;
        }
        var removeStock = e.target.closest('[data-stock-remove]');
        if (removeStock) {
            var el = removeStock.closest('[data-stock-row]');
            var group = el ? el.closest('[data-stock-group]') : null;
            if (el) { el.remove(); }
            // 组内空了就连组一起去掉：留一个只有仓名的空分组，读起来像「这个仓有东西」。
            if (group && !group.querySelector('[data-stock-row]')) { group.remove(); }
            var ownerPanel = removeStock.closest('[data-stock-panel]');
            if (ownerPanel) { toggleStockEmpty(ownerPanel); }
            return;
        }
        var trigger = e.target.closest('[data-generate-selected], [data-generate-all]');
        if (!trigger) { return; }
        var box = trigger.closest('[data-variant-generate]');
        if (!box) { return; }
        var fd = new FormData();
        fd.append('projectId', box.getAttribute('data-project') || '');
        fd.append('productId', box.getAttribute('data-product') || '');
        var warehouse = box.querySelector('[data-generate-warehouse]');
        fd.append('warehouseId', warehouse ? warehouse.value : '');
        if (!trigger.hasAttribute('data-generate-all')) {
            box.querySelectorAll('input[type=checkbox][name^="attr:"]:checked').forEach(function (cb) {
                fd.append(cb.name, cb.value);
            });
        }
        variantListOptionValues().forEach(function (raw) { fd.append('optionValues', raw); });
        setGenerateStatus('');
        fetch(box.getAttribute('data-preview-url') || '', {
            method: 'POST',
            headers: { 'X-CSRF-Token': box.getAttribute('data-csrf') || '' },
            body: fd
        }).then(function (res) {
            return res.json().catch(function () { return null; });
        }).then(function (data) {
            if (!data || !data.ok) {
                var reason = data && data.message ? data.message : '';
                setGenerateStatus((box.getAttribute('data-msg-failed') || '') + reason);
                return;
            }
            var rows = data.rows || [];
            appendVariantRows(rows);
            if (rows.length) {
                setGenerateStatus((box.getAttribute('data-msg-added') || '').replace('%d', String(rows.length)));
            } else {
                setGenerateStatus(box.getAttribute('data-msg-none') || '');
            }
        }).catch(function () {
            setGenerateStatus(box.getAttribute('data-msg-failed') || '');
        });
    });

    // 克隆进 DOM 的明细行：候选下拉要在打开时才填（见 fillStockSelect），
    // 这里只做一次性标记，避免 WBUI.scan 重扫时重复处理。
    function enhanceStocksRow(tr) {
        var row = stocksRowOf(tr);
        if (!row) { return; }
        var panel = row.querySelector('[data-stock-panel]');
        if (panel) { toggleStockEmpty(panel); }
    }

    // WBUI 控件登记：整页扫描与 htmx 局部替换后都会跑一遍（详见 ui/_util.js 的 scan）。
    if (global.WBUI && global.WBUI.register) {
        global.WBUI.register(function (scope) {
            (scope || global.document).querySelectorAll('[data-variant-row]').forEach(function (tr) {
                if (!global.WBUI.markOnce(tr, 'VariantStocks')) { return; }
                enhanceStocksRow(tr);
            });
        });
    }

    // rows 由清单 DOM 组装（不是表单控件），基座收不到 —— 用它的 builder 逃生舱补上。
    // 组装规则只有一份：两个基座并存期间各注册一次（见 ui/write-base.js 的 registerPayloadBuilder）。
    function variantRowsBuilder(form, submitter, payload) {
        payload.rows = variantRows();
        return payload;
    }

    if (global.WPJsonWrite && global.WPJsonWrite.registerPayloadBuilder) {
        global.WPJsonWrite.registerPayloadBuilder('product-variant-rows', variantRowsBuilder);
    }
    if (global.WBFormBody && global.WBFormBody.registerPayloadBuilder) {
        global.WBFormBody.registerPayloadBuilder('product-variant-rows', variantRowsBuilder);
    }
})(window);
