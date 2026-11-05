const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');

// 定价页的试算结果现在由 Jet 片段返回并交给 HTMX 交换，不再有前端 JSON→DOM 处理器。
test('定价页预览与应用都走 HTMX HTML 表单', () => {
    const template = fs.readFileSync(__dirname + '/../../../admin/product/product_pricing.html', 'utf8');
    assert.match(template, /<form\b[^>]*hx-post="\/api\/product\/pricing\/preview"[^>]*>/);
    assert.match(template, /<button\b[^>]*hx-post="\/api\/product\/pricing\/apply"[^>]*>/);
    assert.doesNotMatch(template, /data-write-handler/);
    assert.doesNotMatch(template, /data-write-url=/);
    assert.ok(!fs.existsSync(__dirname + '/product-pricing.js'), '不应保留 JSON→DOM 定价脚本');
});
