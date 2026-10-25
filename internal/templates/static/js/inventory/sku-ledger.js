// static/js/inventory/sku-ledger.js — 仓库商品 SKU 台账页（迁移 621）的抽屉联动。
//
// 只做一件事：认领抽屉里「选变体 → 反填 productId」。
//
// 为什么需要它：库存行上 product_id 与 variant_id 必须**一起落值**（认领是两个字段的
// 一次写入），而运营商只需要选「哪个变体」—— 让用户再手填一遍商品 id 只是多一个
// 可能填错的地方。变体下拉的每个 option 都带着 data-product，选中的那一刻把它写进隐藏域。
//
// 放在 static/js/inventory/ 而不是 static/js/ 根目录：根目录有白名单
//（admin.js / media-lib.js / media-admin.js / product-create-form.js / enhance.js /
// track.js / icons.js），域专属脚本一律落 <域>/ 子目录
//（internal/templates/admin_js_layout_boundary_test.go）。
//
// 抽屉是**模板 clone** 进 DOM 的（data-drawer-open 指向 <template>），所以事件用
// 委托式监听（document 上冒泡），否则 clone 出来的 select 拿不到已绑定的处理器。
(function () {
  'use strict';

  function syncClaimProduct(select) {
    var form = select.closest('form');
    if (!form) {
      return;
    }
    var target = form.querySelector('[data-claim-product]');
    if (!target) {
      return;
    }
    var option = select.options[select.selectedIndex];
    target.value = (option && option.getAttribute('data-product')) || '';
  }

  document.addEventListener('change', function (event) {
    var el = event.target;
    if (!el || el.tagName !== 'SELECT') {
      return;
    }
    if (!el.closest('form') || !el.closest('form').querySelector('[data-claim-product]')) {
      return;
    }
    syncClaimProduct(el);
  });
})();
