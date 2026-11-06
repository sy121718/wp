const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

// 工具层不得依赖 internal：源码里只允许相对模块、pkg 自有模块与浏览器全局。
function walk(dir) {
    return fs.readdirSync(dir, { withFileTypes: true }).flatMap(entry => {
        const full = path.join(dir, entry.name);
        return entry.isDirectory() ? walk(full) : (full.endsWith('.js') ? [full] : []);
    });
}

test('工具源码不出现 internal/ 引用与旧目录回退', () => {
    for (const file of walk(__dirname)) {
        const src = fs.readFileSync(file, 'utf8');
        assert.ok(!/from\s+['"][^'"]*internal\//.test(src), file + ' 引用了 internal');
        assert.ok(!/static\/js\/workbench|js\/workbench\//.test(src), file + ' 仍指向旧 workbench 目录');
    }
});

test('工具入口不自动 boot，宿主必须先配置再 mount', () => {
    const src = fs.readFileSync(path.join(__dirname, 'index.js'), 'utf8');
    assert.ok(src.includes('export function mount('), '缺少显式 mount 入口');
    assert.ok(!/document\.addEventListener\('DOMContentLoaded', boot\)/.test(src), '工具不得自行启动');
    assert.ok(!/if \(window\.__wb\) return;/.test(src), '启动判定应只在 mount 内');
});

test('工具目录数据来自显式注入，不内嵌应用组件清单', () => {
    const catalog = fs.readFileSync(path.join(__dirname, 'catalog.js'), 'utf8');
    assert.ok(catalog.includes('export function configureCatalog('), '缺少目录注入入口');
    assert.ok(!/core\.product(List|Card|Selector)?['"]/.test(catalog), '工具目录内嵌了应用业务组件');
    assert.ok(!catalog.includes('generated-contracts'), '工具不得内置应用生成契约');
});

test('编辑器主入口只经 mount 暴露，不提供页面自动装配', () => {
    const src = fs.readFileSync(path.join(__dirname, 'index.js'), 'utf8');
    const exports = src.match(/export (function|const|let) [A-Za-z0-9_]+/g) || [];
    assert.ok(exports.length <= 3, '入口导出应有界，实际 ' + exports.join(', '));
});
