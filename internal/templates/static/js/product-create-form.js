/* 商品新建控件：候选筛选与可选输入接入统一扫描，不复制 SKU 命名或库存业务规则。 */
(function () {
    'use strict';
    var WBUI = window.WBUI;

    function enhance(form) {
        var input = form.querySelector('[data-sku-input]');
        if (!input || input.dataset.skuEnhanced === '1') return;
        input.dataset.skuEnhanced = '1';
        form.dataset.skuEnhanced = '1';

        var warehouses = Array.from(form.querySelectorAll('[data-warehouse-check]'));
        var candidates = form.querySelector('[data-sku-candidates]');
        var track = form.querySelector('[data-track-quantity]');
        var quantity = form.querySelector('[data-quantity-input]');
        var priceSwitch = form.querySelector('[data-default-price-switch]');
        var price = form.querySelector('[data-default-price-input]');

        function syncCandidates() {
            if (!candidates) return;
            var selected = warehouses.filter(function (box) { return box.checked; }).map(function (box) { return box.value; });
            Array.from(candidates.querySelectorAll('option')).forEach(function (option) {
                // 未选择仓库时保留所有候选，默认仓、复用与编码最终均由 Service 判定。
                option.disabled = selected.length > 0 && selected.indexOf(option.dataset.warehouse) < 0;
            });
        }

        function syncQuantity() {
            if (!quantity) return;
            quantity.disabled = !(track && track.checked);
            if (quantity.disabled) quantity.value = '';
        }

        function syncPrice() {
            if (!price) return;
            price.disabled = !(priceSwitch && priceSwitch.checked);
            price.required = !price.disabled;
        }

        warehouses.forEach(function (box) { box.addEventListener('change', syncCandidates); });
        if (track) track.addEventListener('change', syncQuantity);
        if (priceSwitch) priceSwitch.addEventListener('change', syncPrice);
        syncCandidates();
        syncQuantity();
        syncPrice();
    }

    function scan(scope) {
        if (scope.matches && scope.matches('form[data-product-create-form]')) enhance(scope);
        scope.querySelectorAll('form[data-product-create-form]').forEach(enhance);
    }

    WBUI.register(scan);
    // 页面专用 defer 脚本可能晚于基座首扫；仍使用相同扫描入口，标记避免重复监听。
    WBUI.ready(function () { scan(document); });
})();
