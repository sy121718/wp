/* 原生开合补上视口避让、外点关闭和 Esc 后焦点归还。 */
function initCartIconPanels() {
  if (initCartIconPanels.ready) return;
  initCartIconPanels.ready = true;
  var roots = document.querySelectorAll('[data-cart-icon]');
  function place(root) {
    var mode = root.getAttribute('data-cart-icon');
    if (mode !== 'dropdown' && mode !== 'hover') return;
    var panel = root.querySelector('.sky-cart-icon-panel');
    if (!panel || !panel.getClientRects().length) return;
    panel.style.transform = '';
    panel.style.top = '';
    panel.style.bottom = '';
    panel.style.maxHeight = '';
    var rect = panel.getBoundingClientRect();
    var width = document.documentElement.clientWidth;
    var height = document.documentElement.clientHeight;
    var gap = 16;
    var shift = rect.left < gap ? gap - rect.left : rect.right > width - gap ? width - gap - rect.right : 0;
    panel.style.transform = 'translateX(' + shift + 'px)';
    var anchor = root.getBoundingClientRect();
    var below = height - anchor.bottom - gap - 8;
    var above = anchor.top - gap - 8;
    if (rect.bottom > height - gap && above > below) {
      panel.style.top = 'auto';
      panel.style.bottom = 'calc(100% + 8px)';
      panel.style.maxHeight = Math.max(80, above) + 'px';
    } else {
      panel.style.maxHeight = Math.max(80, below) + 'px';
    }
  }
  function open(root) {
    var details = root.querySelector('details[data-cart-icon-panel]');
    var toggle = root.querySelector('.sky-cart-icon-toggle');
    return details && details.hasAttribute('open') || toggle && toggle.checked || root.getAttribute('data-cart-icon') === 'hover' && root.matches(':hover');
  }
  function close(root, focus) {
    var details = root.querySelector('details[data-cart-icon-panel]');
    var toggle = root.querySelector('.sky-cart-icon-toggle');
    if (details) details.removeAttribute('open');
    if (toggle) toggle.checked = false;
    if (focus) {
      var trigger = details ? details.querySelector('summary') : toggle;
      if (trigger) trigger.focus();
      if (root.getAttribute('data-cart-icon') === 'hover') root.setAttribute('data-cart-panel-dismissed', '');
    }
  }
  roots.forEach(function(root) {
    var details = root.querySelector('details[data-cart-icon-panel]');
    if (details) details.addEventListener('toggle', function() { if (details.open) place(root); });
    root.addEventListener('mouseenter', function() { root.removeAttribute('data-cart-panel-dismissed'); place(root); });
    root.addEventListener('focusin', function() { root.removeAttribute('data-cart-panel-dismissed'); place(root); });
    root.addEventListener('change', function() { place(root); });
  });
  document.addEventListener('click', function(event) {
    roots.forEach(function(root) { if (open(root) && !root.contains(event.target)) close(root, false); });
  });
  document.addEventListener('keydown', function(event) {
    if (event.key !== 'Escape' && event.key !== 'Esc') return;
    roots.forEach(function(root) { if (open(root)) close(root, root.contains(document.activeElement)); });
  });
  window.addEventListener('resize', function() { roots.forEach(function(root) { if (open(root)) place(root); }); });
}
