/* 商品实时事实的展示规则。金额仅做格式化，不在前端计算业务金额或库存。 */
(function (global) {
    'use strict';
    function fact(el, data) {
        return Array.isArray(data) ? data.find(function (item) { return item.id === el.getAttribute('data-variant-id'); }) : null;
    }
    function label(el, name) { return el.getAttribute('data-label-' + name) || ''; }
    function money(cents) {
        if (!Number.isSafeInteger(cents)) return '';
        var value = String(Math.abs(cents)), whole = value.length > 2 ? value.slice(0, -2) : '0';
        var decimal = value.padStart(3, '0').slice(-2).replace(/0+$/, '');
        return (cents < 0 ? '-' : '') + whole + (decimal ? '.' + decimal : '');
    }
    global.WPSiteData.register('variantPrice', function (el, data) {
        var item = fact(el, data), message = '';
        if (item && item.known) {
            if (!item.enabled) message = label(el, 'delisted');
            else if (item.price !== null && Number.isSafeInteger(item.price) && item.price !== Number(el.getAttribute('data-price'))) {
                message = label(el, 'updated').replace('%s', (el.getAttribute('data-currency') || '') + money(item.price));
            }
        }
        el.textContent = message;
        el.classList.toggle('is-changed', !!message);
    });
    global.WPSiteData.register('variantStock', function (el, data) {
        var item = fact(el, data), message = label(el, 'unknown'), out = false;
        if (item && item.known && !item.enabled) {
            message = label(el, 'out'); out = true;
        } else if (item && item.known && Number.isSafeInteger(item.available)) {
            out = !item.enabled || item.available <= 0;
            if (out) message = label(el, 'out');
            else if (item.available <= 5) message = label(el, 'low').replace('%d', String(item.available));
            else message = label(el, 'in');
        }
        el.textContent = message;
        el.classList.toggle('is-out', out);
    });
})(window);
