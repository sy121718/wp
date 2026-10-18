const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// 只加载纯函数部分（模块在没有 document 时不挂事件），DOM 交互由页面结构测试兜住。
const sandbox = { window: {} };
vm.runInNewContext(fs.readFileSync(__dirname + '/bundle_members.js', 'utf8'), sandbox);
const m = sandbox.window.WPBundleMembers;

test('planAppend 去空去重并按上限截断', () => {
    const got = m.planAppend(['a', 'b'], [{ variantId: 'a' }, { variantId: 'c' }, { variantId: '' }, { variantId: 'd' }], 3);
    // 跨 vm realm 的数组原型不同，deepEqual 会误判 —— 比较拼接后的字符串。
    assert.equal(got.map(x => x.variantId).join(','), 'c');
});

test('planAppend 已有清单已满时一条都不追加', () => {
    assert.equal(m.planAppend(['a', 'b'], [{ variantId: 'z' }], 2).length, 0);
});

test('planAppend 上限缺省 / 非法时回落到 20', () => {
    const many = [];
    for (let i = 0; i < 30; i++) { many.push({ variantId: 'v' + i }); }
    assert.equal(m.planAppend([], many, 0).length, 20);
    assert.equal(m.planAppend([], many, 'x').length, 20);
    assert.equal(m.planAppend([], many, 5).length, 5);
});

test('planAppend 忽略大小写无关的空白 id', () => {
    assert.equal(m.planAppend([], [{ variantId: '  ' }, { variantId: ' ok ' }], 20).map(x => x.variantId).join('|'), ' ok ');
});

test('receiptText 拼出追加数 + 跳过数与逐条原因', () => {
    const msg = { added: '已追加 %d 行', none: '没有解析出任何候选成员', skipped: '跳过 %d 条：' };
    assert.equal(m.receiptText(2, [], msg), '已追加 2 行');
    assert.equal(m.receiptText(0, [], msg), '没有解析出任何候选成员');
    assert.equal(m.receiptText(1, [{ label: 'SKU-1', text: '库存不足' }, { text: '已存在' }], msg),
        '已追加 1 行 跳过 2 条：SKU-1：库存不足；已存在');
});

// sourceText：来源列三段事实拼成一句，词条来自模板的 data-msg-source-*（宿主元素自带
// getAttribute）；空段不出现，来源种类未知时按「手工指定」那一档。
test('sourceText 按 data-msg 拼来源、仓库 SKU 与外部编码', () => {
    const host = {
        getAttribute: (name) => ({
            'data-msg-source-product': '从商品导入',
            'data-msg-source-warehouse': '从仓库选',
            'data-msg-source-none': '手工指定',
            'data-msg-source-sku': '仓库 SKU',
            'data-msg-source-external': '外部编码'
        })[name] || null
    };
    assert.equal(m.sourceText(host, { sourceKind: 'BundleSourceWarehouse', warehouseSku: 'SKU-1', externalSku: 'EXT-2' }),
        '从仓库选 · 仓库 SKU SKU-1 · 外部编码 EXT-2');
    assert.equal(m.sourceText(host, { sourceKind: 'BundleSourceProduct' }), '从商品导入');
    assert.equal(m.sourceText(host, { sourceKind: 'legacy', warehouseSku: '  ' }), '手工指定');
});

test('模块在无 document 环境下不抛错（SSR / 单测入口）', () => {
    assert.equal(typeof m.bind, 'function');
    assert.equal(typeof m.appendMembers, 'function');
});
