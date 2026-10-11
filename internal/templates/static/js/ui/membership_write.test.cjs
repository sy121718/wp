const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const builders = {};
vm.runInNewContext(fs.readFileSync(__dirname + '/membership_write.js', 'utf8'), {
    window: {WPJsonWrite: {registerPayloadBuilder(name, fn) { builders[name] = fn; }}}
});
function form(create, shipping) {
    return {
        getAttribute(name) {
            return {'data-membership-create': create ? 'true' : null,
                'data-membership-shipping-kind': 'free_shipping',
                'data-membership-discount-kind': 'discount'}[name];
        },
        querySelector() { return {checked: shipping}; }
    };
}
function plain(value) { return JSON.parse(JSON.stringify(value)); }
for (const [raw, expected] of [['1000', 100000], ['1000.5', 100050], ['1,000.05', 100005], ['', 0], ['-0.01', -1], ['1000.123', '1000.123'], ['900719925474099.99', '900719925474099.99']]) {
    test('threshold yuan to exact cents: ' + JSON.stringify(raw), () => {
        const result = builders['membership-tier'](form(false, false), null, {thresholdYuan: raw, sortOrder: '-1'});
        assert.equal(result.thresholdAmount, expected);
        assert.equal(result.sortOrder, -1);
        assert.equal(Object.hasOwn(result, 'thresholdYuan'), false);
        assert.equal(Object.hasOwn(result, 'isDefault'), false);
    });
}
test('create preserves explicit default and produces optional entitlements', () => {
    const result = builders['membership-tier'](form(true, true), null, {
        thresholdYuan: '0', sortOrder: '', isDefault: true, freeShipping: '1', discountPercent: '20'
    });
    assert.deepEqual(plain(result), {thresholdAmount: 0, sortOrder: 0, isDefault: true,
        entitlements: [{kind: 'free_shipping', valueInt: 1}, {kind: 'discount', valueInt: 20}]});
});
test('empty final entitlement list clears both previous benefits', () => {
    const result = builders['membership-entitlements'](form(false, false), null, {tierId: 2, projectId: 'p-1', discountPercent: ''});
    assert.deepEqual(plain(result), {tierId: 2, projectId: 'p-1', items: []});
});
test('zero discount remains visible to service validation', () => {
    const result = builders['membership-entitlements'](form(false, false), null, {discountPercent: '0'});
    assert.deepEqual(plain(result.items), [{kind: 'discount', valueInt: 0}]);
});
test('malformed integer remains strict JSON binding input', () => {
    const result = builders['membership-entitlements'](form(false, false), null, {discountPercent: 'abc'});
    assert.equal(result.items[0].valueInt, 'abc');
});
