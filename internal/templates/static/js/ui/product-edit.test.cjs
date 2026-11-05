const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function collect(overrides = {}, images = []) {
    let builder;
    const values = {
        id: ['9007199254740993'], projectId: ['project'], name: ['商品'], subtitle: [''],
        status: ['draft'], unit: ['件'], defaultImage: [''], brandId: [''], primaryCategoryId: [''],
        descriptionHtml: ['<p>真实富文本</p>'], sku: [''], slug: [''],
        attributeIds: [], categoryIds: [], tagIds: [], ...overrides
    };
    const numbers = Object.fromEntries(['defaultPrice', 'weight'].map(name => {
        const value = values[name]?.[0] ?? '';
        return [name, { value, valueAsNumber: value === '' ? NaN : Number(value), validationMessage: '数值无效' }];
    }));
    class FormData {
        get(name) { return values[name]?.[0] ?? null; }
        getAll(name) { return values[name] || []; }
    }
    const form = {
        elements: { namedItem: name => numbers[name] },
        querySelectorAll(selector) {
            assert.equal(selector, '[data-media-gallery] [data-gallery-url]');
            return images.map(([url, alt]) => ({
                getAttribute(name) { assert.equal(name, 'data-gallery-url'); return url; },
                querySelector(name) { assert.equal(name, '[data-gallery-alt]'); return { value: alt }; }
            }));
        }
    };
    vm.runInNewContext(fs.readFileSync(__dirname + '/product-edit.js', 'utf8'), {
        window: { WBUI: { writeBase: { registerPayloadBuilder(name, fn) {
            // 同一脚本还注册标签与批量改价的对象提供器；编辑用例只取自己的。
            if (name === 'product-edit') builder = fn;
        } } } }, FormData
    });
    return JSON.parse(JSON.stringify(builder(form)));
}

test('主体对象保留字符串ID、真实富文本、空关联与全取消集合', () => {
    const p = collect();
    assert.equal(p.id, '9007199254740993');
    assert.deepEqual(p.description, { html: '<p>真实富文本</p>' });
    assert.deepEqual([p.attributeIds, p.categoryIds, p.tagIds, p.images, p.imageAlts], [[], [], [], [], []]);
    assert.equal(p.brandId, '');
    assert.equal(p.primaryCategoryId, '');
    assert.equal(p.clearDefaultPrice, true);
    assert.equal(p.clearWeight, true);
    for (const key of ['sku', 'slug', 'defaultPrice', 'weight', 'operatorId', 'metadata']) assert.equal(Object.hasOwn(p, key), false, key);
});

test('原生数值零不变成清空，非零值仅编码不做业务计算', () => {
    const p = collect({ defaultPrice: ['0'], weight: ['12.50'], sku: ['主体SKU'], slug: ['product-slug'] });
    assert.equal(p.defaultPrice, 0);
    assert.equal(p.weight, 12.5);
    assert.equal(p.sku, '主体SKU');
    assert.equal(p.slug, 'product-slug');
    assert.equal(Object.hasOwn(p, 'clearDefaultPrice'), false);
    assert.equal(Object.hasOwn(p, 'clearWeight'), false);
});

test('图片与空alt严格按当前图项位置对应，复选框保持多值', () => {
    const p = collect({ attributeIds: ['a', 'b'], categoryIds: ['c'], tagIds: ['t'] }, [['/one.png', ''], ['/two.png', '第二张']]);
    assert.deepEqual(p.images, ['/one.png', '/two.png']);
    assert.deepEqual(p.imageAlts, ['', '第二张']);
    assert.deepEqual(p.attributeIds, ['a', 'b']);
    assert.deepEqual(p.categoryIds, ['c']);
    assert.deepEqual(p.tagIds, ['t']);
});

test('无法编码的数值拒绝发送而不是JSON.stringify静默写为null', () => {
    assert.throws(() => collect({ defaultPrice: ['not-a-number'] }), /数值无效/);
});
