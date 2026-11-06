const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
// 公共控件属主在 pkg/view/ui；本测试读取真实源，不复制到旧位置。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');

// 网络、交换、失败保留与迟到响应由真实 HTTP/Chrome 用例验证；这里反查已退役的第二套实现。
test('抽屉不保留 fetch、JSON 绑定解释器、旧属性或直接 HTMX 调用', () => {
    const source = fs.readFileSync(path.join(UI_SRC, 'drawer.js'), 'utf8');
    assert.doesNotMatch(source, /\bfetch\s*\(|\bAbortController\b|\bJSON\.|data-bind|fillTemplate|bindValue|data-drawer-url|data-drawer-template|data-drawer-retry|\bhtmx\s*\./);
    assert.doesNotMatch(source, /createElement\s*\(|innerHTML\s*=/);
    assert.match(source, /wb:content-ready/);
});

test('Jet 外壳声明远程抽屉目标和独立错误区', () => {
    const shell = fs.readFileSync(__dirname + '/../../../admin/layout.html', 'utf8');
    assert.match(shell, /<aside\b[^>]*data-drawer\b[^>]*data-write-host\b/);
    assert.match(shell, /id="admin-drawer-body"[^>]*data-drawer-body/);
    assert.match(shell, /data-write-status role="alert" hidden/);
});
