/**
 * 商品编辑页的变体清单（docs/14 §8 的「预览—保存」模型）。
 *
 * 三条规则决定这段脚本的形态：
 * 1. 事件用 document 级委托 —— 抽屉内容是从 <template> 克隆进 DOM 的，逐个绑监听会在
 *    抽屉关闭（body.innerHTML = ''）时失效；委托对克隆前后的元素一视同仁。
 * 2. 「生成」是一次**不落库**的取数：POST 到 /admin/products/variant/preview，把返回的行
 *    按清单行的同一种结构（<template data-variant-row-tpl>）追加进 DOM。
 * 3. 「保存」交给 JSON 写基座（ui/json_write.js）：清单行不是具名控件，基座收不到它们，
 *    因此这里注册一个 data-write-builder —— 把清单如实组装成 rows；组合合法性与唯一性
 *    一律由服务端重算（前端提交的形状不作数）。
 *
 * 文案一律从 data-msg-* / data-write-* 取（服务端按当前语言渲染），脚本里不写死文案。
 * 清单全页唯一（一页只编辑一个商品），所以下面的查询都用 document 作用域。
 */
(function (global) {
    'use strict';

    function variantListBody() {
        return global.document.querySelector('[data-variant-list]');
    }

    // 清单里每行的 option_values（规范化 JSON 文本）：原样回传给预览接口做去重判据。
    function variantListOptionValues() {
        var out = [];
        var trs = global.document.querySelectorAll('[data-variant-row]');
        for (var i = 0; i < trs.length; i++) {
            var raw = trs[i].getAttribute('data-option-values');
            if (raw) { out.push(raw); }
        }
        return out;
    }

    // variantRows 把清单 DOM 组装成服务端的 rows（顺序即期望顺序）：
    // variantId 为空 = 新增行；skuCode 是用户可编辑的那一格；optionValues 原样回传。
    function variantRows() {
        var rows = [];
        var trs = global.document.querySelectorAll('[data-variant-row]');
        for (var i = 0; i < trs.length; i++) {
            var tr = trs[i];
            var sku = tr.querySelector('[data-variant-sku]');
            var opts = {};
            try { opts = JSON.parse(tr.getAttribute('data-option-values') || '{}') || {}; } catch (err) { opts = {}; }
            rows.push({
                variantId: tr.getAttribute('data-variant-id') || '',
                skuCode: sku ? sku.value : '',
                optionValues: opts
            });
        }
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
        var boxes = global.document.querySelectorAll('[data-generate-status]');
        for (var i = 0; i < boxes.length; i++) { boxes[i].textContent = text || ''; }
    }

    global.document.addEventListener('click', function (e) {
        var remove = e.target.closest('[data-variant-remove]');
        if (remove) {
            var tr = remove.closest('[data-variant-row]');
            if (tr) { tr.remove(); }
            hideVariantEmptyRow();
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

    // rows 由清单 DOM 组装（不是表单控件），基座收不到 —— 用它的 builder 逃生舱补上。
    if (global.WPJsonWrite && global.WPJsonWrite.registerPayloadBuilder) {
        global.WPJsonWrite.registerPayloadBuilder('product-variant-rows', function (form, submitter, payload) {
            payload.rows = variantRows();
            return payload;
        });
    }
})(window);
