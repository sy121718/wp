/* carticon enhance — 点浮层外部 / 按 Esc 收起购物车浮层。
 *
 * <details> 是原生的可展开元素：开合、键盘、触屏都由浏览器负责，这里只补它唯一的缺口 ——
 * 点外部不关。不自己实现整套开合状态：那才是这类浮层最容易写错的地方（焦点陷阱、
 * 重复绑定、Esc 与点击互相打架）。收起仍由原生负责（removeAttribute('open') 即原生收起）。
 *
 * 只在页面里出现过 data-cart-icon-panel 时注入（见 core.RegisterEnhanceBlock 的 Feats）。
 */
function initCartIconPanels() {
  var roots = document.querySelectorAll('details[data-cart-icon-panel]');
  if (!roots.length) {
    return;
  }

  function closeAll(except) {
    for (var i = 0; i < roots.length; i++) {
      if (roots[i] !== except) {
        roots[i].removeAttribute('open');
      }
    }
  }

  document.addEventListener('click', function (event) {
    for (var i = 0; i < roots.length; i++) {
      var root = roots[i];
      if (root.hasAttribute('open') && !root.contains(event.target)) {
        root.removeAttribute('open');
      }
    }
  });

  document.addEventListener('keydown', function (event) {
    if (event.key === 'Escape' || event.key === 'Esc') {
      closeAll(null);
    }
  });
}
