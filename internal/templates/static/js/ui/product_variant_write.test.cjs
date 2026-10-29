/**
 * product_variant_write.js 的 builder 契约测试。
 *
 * 为什么必须有这条：check-write-fields.sh 对 `data-write-builder="..."` 的表单
 * **跳过字段对账**（页面字段名 → DTO 字段的映射只有页面脚本知道），而它自己的注释
 * 曾把守门责任挂在 product_bundle_write.test.cjs 上 —— 那个文件从来不存在
 * （bundle 概念已下线）。于是「商品变体行的组装由测试守」这句话一直是假的。
 *
 * 本文件守的是变体行唯一那条组装路径：
 *
 *   清单 DOM（[data-variant-row]） → rows[] → POST /api/product/* 的 rows 字段
 *
 * 服务端会重算组合合法性与唯一性，所以这里不测「合法」——测的是**形状**：
 * 服务端拿到的 rows 里到底有什么键、什么时候带 stocks、空清单是不是 []。
 *
 * 手法与 membership_write.test.cjs 一致：node:test + vm + 极简 DOM 桩，无浏览器。
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const builders = {};

// —— 极简 DOM 桩 ——
//
// 只实现脚本实际用到的三件事：querySelector / querySelectorAll / 属性与 value。
// 不去模拟完整 DOM：桩越厚，测试越容易变成"测桩"而不是测组装逻辑。

function el(props) {
    const self = {
        value: props.value,
        content: props.content,
        _attrs: props.attrs || {},
        getAttribute(name) {
            return Object.prototype.hasOwnProperty.call(self._attrs, name) ? self._attrs[name] : null;
        },
        setAttribute(name, value) {
            self._attrs[name] = String(value);
        },
        hasAttribute(name) {
            return Object.prototype.hasOwnProperty.call(self._attrs, name);
        },
        querySelector(sel) {
            const found = (props.query || {})[sel];
            return found === undefined ? null : found;
        },
        querySelectorAll() {
            return [];
        },
        appendChild() {},
        cloneNode() {
            return el(props);
        }
    };
    return self;
}

/**
 * variantRow 造一条清单行。
 *
 * 行的结构（product_edit.html 的 data-variant-row）：
 *   <tr data-variant-row data-variant-id="..." data-option-values='{...}'>
 *     <input data-variant-sku value="...">
 *   紧随其后的 <tr data-variant-stocks-row> 里是仓库明细面板。
 */
function variantRow({ id, sku, optionValues, stocksPanel }) {
    const attrs = {};
    if (id) {
        attrs['data-variant-id'] = id;
    }
    if (optionValues !== undefined) {
        attrs['data-option-values'] = optionValues;
    }
    const skuInput = el({ value: sku === undefined ? '' : sku });
    const row = el({ attrs, query: { '[data-variant-sku]': skuInput } });

    const next = stocksPanel === undefined
        ? null
        : el({ attrs: { 'data-variant-stocks-row': '' }, query: { '[data-stock-panel]': stocksPanel } });
    row.nextElementSibling = next;
    return row;
}

function install({ rows = [], stockRows = {} } = {}) {
    const doc = {
        // 事件是 document 级委托（抽屉内容从 <template> 克隆，逐个绑会失效），
        // 这里只吞掉注册 —— 本文件测的是组装形状，不是事件路由。
        addEventListener() {},
        createElement() {
            return {style: {}, setAttribute() {}, appendChild() {}, querySelector() { return null; }};
        },
        querySelector() {
            return null;
        },
        querySelectorAll(sel) {
            if (sel === '[data-variant-row]') {
                return rows;
            }
            return [];
        }
    };
    // 仓库明细的面板按行取：stocksOf 会用 row.nextElementSibling 找面板。
    for (const r of rows) {
        if (r.nextElementSibling) {
            r.nextElementSibling.querySelector = (sel) => stockRows[sel] === undefined ? null : stockRows[sel];
        }
    }
    const win = {
        document: doc,
        WPJsonWrite: {
            registerPayloadBuilder(name, fn) {
                builders[name] = fn;
            }
        }
    };
    vm.runInNewContext(fs.readFileSync(__dirname + '/product_variant_write.js', 'utf8'), {window: win});
    return win;
}

function builder() {
    const fn = builders['product-variant-rows'];
    assert.ok(fn, 'product-variant-rows 必须注册到写基座（否则 rows 根本发不出去）');
    return fn;
}

function plain(value) {
    return JSON.parse(JSON.stringify(value));
}

test('空清单提交空数组而不是缺键', () => {
    install({rows: []});
    const payload = builder()(null, null, {});
    assert.deepEqual(plain(payload.rows), []);
    assert.ok(Object.hasOwn(payload, 'rows'), 'rows 必须在载荷里：服务端按"整批替换"读它，缺键会被当成不改');
});

test('一行一个对象，只带服务端认的三个键', () => {
    install({rows: [variantRow({id: 'v-1', sku: 'SKU-1', optionValues: '{"尺寸":"M"}'})]});
    const payload = builder()(null, null, {});
    assert.deepEqual(plain(payload.rows), [
        {variantId: 'v-1', skuCode: 'SKU-1', optionValues: {'尺寸': 'M'}}
    ]);
});

test('新行没有 variantId 时给空串而不是缺键', () => {
    install({rows: [variantRow({sku: 'NEW-1', optionValues: '{}'})]});
    const payload = builder()(null, null, {});
    assert.equal(payload.rows[0].variantId, '');
    assert.equal(payload.rows[0].skuCode, 'NEW-1');
});

test('optionValues 解析失败时退化成空对象，不把坏 JSON 发给服务端', () => {
    install({rows: [variantRow({id: 'v-1', sku: 'S', optionValues: '{坏掉了'})]});
    const payload = builder()(null, null, {});
    assert.deepEqual(plain(payload.rows[0].optionValues), {});
});

test('optionValues 缺省时是空对象', () => {
    install({rows: [variantRow({id: 'v-1', sku: 'S'})]});
    const payload = builder()(null, null, {});
    assert.deepEqual(plain(payload.rows[0].optionValues), {});
});

test('已有载荷的其它字段不被 rows 覆盖', () => {
    install({rows: []});
    const payload = builder()(null, null, {productId: 42, title: '标题'});
    assert.equal(payload.productId, 42);
    assert.equal(payload.title, '标题');
    assert.deepEqual(plain(payload.rows), []);
});

test('多行顺序按 DOM 顺序，不去重不排序', () => {
    install({
        rows: [
            variantRow({id: 'b', sku: 'SKU-B', optionValues: '{}'}),
            variantRow({id: 'a', sku: 'SKU-A', optionValues: '{}'})
        ]
    });
    const payload = builder()(null, null, {});
    assert.deepEqual(plain(payload.rows.map((r) => r.variantId)), ['b', 'a']);
});
