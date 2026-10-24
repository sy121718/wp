/* product_bundle_write.test.cjs — 捆绑表单 → 请求体的投影（node:test + vm，无浏览器）。
 *
 * 基座（ui/write-base.js）管传输与回执，这个 builder 只管一件事：把成员表的行与三个配置
 * 数值装配成 {productId, config:{…, options:[…]}}。它守的是三类不会报错的错装配：
 *
 *   1. 空行被当成成员提交 —— 服务端会把它当一条没有 SKU 的选项拒掉，整次保存失败；
 *   2. 行内具名字段漏删 —— 顶层多出 variantId / defaultQty 等键，与 config 里的值不一致；
 *   3. 数量按字符串发 —— 严格 JSON 端点上 defaultQty:"2" 会解析失败。
 *
 * 运行：node --test internal/templates/static/js/ui/product_bundle_write.test.cjs
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(__dirname + '/product_bundle_write.js', 'utf8');

// row 造一行成员：fields 是该行里具名字段的取值（缺键 = 该名字没有控件）。
// 同一名字多个控件时按 DOM 顺序给（隐藏域在前、候选下拉在后）。
function row(fields) {
    return {
        querySelectorAll(selector) {
            const match = /\[name="([^"]+)"\]/.exec(selector);
            const value = fields[match[1]];
            if (value === undefined) { return []; }
            return Array.isArray(value) ? value.map((v) => ({ value: v })) : [{ value }];
        },
    };
}

function build(rows, scalars) {
    let builder = null;
    const sandbox = { WPJsonWrite: { registerPayloadBuilder(name, fn) { builder = fn; } } };
    sandbox.window = sandbox;
    vm.runInNewContext(source, sandbox);
    assert.equal(typeof builder, 'function', 'builder 应注册到 product-bundle-config');
    const form = { querySelectorAll(selector) { assert.equal(selector, '[data-bundle-member-row]'); return rows; } };
    return builder(form, null, Object.assign({ productId: 'p-1' }, scalars));
}

test('成员行装配成 config.options，行内中转键不回顶层', () => {
    const payload = build([
        row({
            variantId: 'v-1', required: '1', defaultQty: '2', minQty: '1', maxQty: '3',
            sourceKind: 'BundleSourceWarehouse', memberWarehouseId: 'wh-1',
            memberWarehouseSku: 'WSKU-1', memberExternalSku: 'EXT-1',
        }),
    ], { maxOptions: '7', minTotalQty: '2', maxTotalQty: '9', memberRows: [{}, {}] });

    // 跨 vm 上下文的对象原型不同，deepEqual 会判不等 —— 走一次 JSON 往返再比结构。
    assert.deepEqual(JSON.parse(JSON.stringify(payload)), {
        productId: 'p-1',
        config: {
            maxOptions: 7,
            minTotalQty: 2,
            maxTotalQty: 9,
            options: [{
                variantId: 'v-1', required: true, defaultQty: 2, minQty: 1, maxQty: 3,
                sourceKind: 'BundleSourceWarehouse', warehouseId: 'wh-1',
                warehouseSku: 'WSKU-1', externalSku: 'EXT-1',
            }],
        },
    });
    assert.equal('memberRows' in payload, false);
    assert.equal('maxOptions' in payload, false);
});

test('没有 SKU 的空行不是成员（尾部候选行不提交）', () => {
    const payload = build([
        row({ variantId: 'v-1', required: '1', defaultQty: '1', minQty: '1', maxQty: '0' }),
        row({ variantId: '', required: '1', defaultQty: '1', minQty: '1', maxQty: '0' }),
    ], { maxOptions: '20', minTotalQty: '0', maxTotalQty: '0' });

    assert.equal(payload.config.options.length, 1);
});

test('required 只有 "1" 才算必选，数量非数字按 0（交给服务端判区间）', () => {
    const payload = build([
        row({ variantId: 'v-2', required: '0', defaultQty: 'abc', minQty: '', maxQty: '5' }),
    ], { maxOptions: '', minTotalQty: 'x', maxTotalQty: '0' });

    const option = payload.config.options[0];
    assert.equal(option.required, false);
    assert.equal(option.defaultQty, 0);
    assert.equal(option.minQty, 0);
    assert.equal(option.maxQty, 5);
    assert.equal(payload.config.maxOptions, 0);
    assert.equal(payload.config.minTotalQty, 0);
});

test('同一名字有多个控件时取第一个非空值（隐藏身份优先于候选下拉）', () => {
    const payload = build([
        row({ variantId: ['', 'v-3'], required: '1' }),
    ], { maxOptions: '20' });

    assert.equal(payload.config.options[0].variantId, 'v-3');
});
