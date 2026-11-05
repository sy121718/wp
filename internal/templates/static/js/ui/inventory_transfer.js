/* 调拨建单表单 → 结构化请求对象；发送、CSRF 与失败区交给 write-base 基座。
   明细行由 ui/transfer_lines.js 渲染成 lines[i].stockId / lines[i].quantity 的扁平键，
   这里把它们收成明确的 lines 数组后按 JSON 提交。 */
(function (global) {
    'use strict';
    var ui = global.WBUI;
    if (!ui || !ui.writeBase || typeof ui.writeBase.registerPayloadBuilder !== 'function') { return; }

    ui.writeBase.registerPayloadBuilder('inventory-transfer', function (form) {
        var fields = new FormData(form);
        var lines = [];
        form.querySelectorAll('.transfer-line').forEach(function (row) {
            var stock = row.querySelector('[data-line-stock]');
            var qty = row.querySelector('[data-line-qty]');
            var stockId = stock ? String(stock.value || '').trim() : '';
            if (!stockId) { return; }
            var quantity = parseInt(qty ? qty.value : '', 10);
            lines.push({stockId: stockId, quantity: Number.isFinite(quantity) ? quantity : 0});
        });
        return {
            projectId: fields.get('projectId') || '',
            fromWarehouseId: fields.get('fromWarehouseId') || '',
            toWarehouseId: fields.get('toWarehouseId') || '',
            remark: fields.get('remark') || '',
            lines: lines
        };
    });
})(window);
