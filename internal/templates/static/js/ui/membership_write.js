/* Membership form-to-DTO projection; transport and feedback belong to json_write.js. */
(function (global) {
    'use strict';

    function integer(raw, fallback) {
        var value = String(raw == null ? '' : raw).trim();
        if (value === '') { return fallback; }
        if (!/^-?\d+$/.test(value)) { return value; }
        var parsed = Number(value);
        return Number.isSafeInteger(parsed) ? parsed : value;
    }

    function cents(raw) {
        var value = String(raw == null ? '' : raw).trim().replace(/,/g, '');
        if (value === '') { return 0; }
        var match = /^(-?)(\d*)\.?([0-9]{0,2})$/.exec(value);
        if (!match) { return value; }
        var whole = BigInt(match[2] || '0');
        var fraction = BigInt((match[3] + '00').slice(0, 2));
        var result = (whole * 100n + fraction) * (match[1] ? -1n : 1n);
        if (result > BigInt(Number.MAX_SAFE_INTEGER) || result < BigInt(Number.MIN_SAFE_INTEGER)) {
            return value;
        }
        return Number(result);
    }

    function entitlements(form, payload) {
        var items = [];
        var shipping = form.querySelector('[name="freeShipping"]');
        if (shipping && shipping.checked) {
            items.push({kind: form.getAttribute('data-membership-shipping-kind'), valueInt: 1});
        }
        var discount = String(payload.discountPercent == null ? '' : payload.discountPercent).trim();
        if (discount !== '') {
            items.push({kind: form.getAttribute('data-membership-discount-kind'), valueInt: integer(discount, 0)});
        }
        delete payload.freeShipping;
        delete payload.discountPercent;
        return items;
    }

    global.WPJsonWrite.registerPayloadBuilder('membership-tier', function (form, submitter, payload) {
        payload.thresholdAmount = cents(payload.thresholdYuan);
        delete payload.thresholdYuan;
        payload.sortOrder = integer(payload.sortOrder, 0);
        if (form.getAttribute('data-membership-create') === 'true') {
            payload.entitlements = entitlements(form, payload);
        }
        return payload;
    });

    global.WPJsonWrite.registerPayloadBuilder('membership-entitlements', function (form, submitter, payload) {
        payload.items = entitlements(form, payload);
        return payload;
    });
})(window);
