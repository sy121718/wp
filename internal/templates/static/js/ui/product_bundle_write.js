/* Bundle form-to-DTO projection; transport and feedback belong to json_write.js. */
(function (global) {
    'use strict';

    var ROW_SEL = '[data-bundle-member-row]';

    // 一行里同一个名字可能有多个控件（隐藏域 + 候选下拉），按 DOM 顺序取第一个非空值：
    // 老成员行是 hidden variantId，新加的空行是 select。
    function fieldValue(row, name) {
        var nodes = row.querySelectorAll('[name="' + name + '"]');
        for (var i = 0; i < nodes.length; i++) {
            var value = String(nodes[i].value == null ? '' : nodes[i].value).trim();
            if (value !== '') { return value; }
        }
        return '';
    }

    function integer(raw) {
        var parsed = parseInt(String(raw == null ? '' : raw).trim(), 10);
        return isNaN(parsed) ? 0 : parsed;
    }

    // optionOf 一行 → 一个 BundleOption；没有 SKU 的行不是成员（空行占位，不提交）。
    function optionOf(row) {
        var variantId = fieldValue(row, 'variantId');
        if (variantId === '') { return null; }
        return {
            variantId: variantId,
            required: fieldValue(row, 'required') === '1',
            defaultQty: integer(fieldValue(row, 'defaultQty')),
            minQty: integer(fieldValue(row, 'minQty')),
            maxQty: integer(fieldValue(row, 'maxQty')),
            sourceKind: fieldValue(row, 'sourceKind'),
            warehouseId: fieldValue(row, 'memberWarehouseId'),
            warehouseSku: fieldValue(row, 'memberWarehouseSku'),
            externalSku: fieldValue(row, 'memberExternalSku')
        };
    }

    // 行内九个具名字段由 data-write-rows 收进 payload.memberRows（不进顶层）；
    // 这里把它们装配成服务端认的 config，然后删掉中转键 —— 请求体只有 {productId, config}。
    global.WPJsonWrite.registerPayloadBuilder('product-bundle-config', function (form, submitter, payload) {
        var options = [];
        var rows = form.querySelectorAll(ROW_SEL);
        for (var i = 0; i < rows.length; i++) {
            var option = optionOf(rows[i]);
            if (option) { options.push(option); }
        }
        payload.config = {
            maxOptions: integer(payload.maxOptions),
            minTotalQty: integer(payload.minTotalQty),
            maxTotalQty: integer(payload.maxTotalQty),
            options: options
        };
        delete payload.memberRows;
        delete payload.maxOptions;
        delete payload.minTotalQty;
        delete payload.maxTotalQty;
        return payload;
    });
})(window);
