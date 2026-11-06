const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
// 公共控件属主在 pkg/view/ui；本测试读取真实源，不复制到旧位置。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');

function element() {
    const attrs = {}, listeners = {};
    return {
        children: [], value: '',
        setAttribute(key, value) { attrs[key] = value; },
        getAttribute(key) { return attrs[key] ?? null; },
        addEventListener(key, fn) { listeners[key] = fn; },
        appendChild(child) { this.children.push(child); },
        querySelector(selector) { return this.children.find(child => child.getAttribute(selector.slice(1, -1)) !== null) || null; },
        dispatch(key) { listeners[key](); }
    };
}

test('图库初始渲染与编辑保留空alt占位，不把后图说明移到前图', () => {
    const grid = element(), values = { value: '/one.png\n/two.png' }, alts = { value: '\n第二张' };
    const root = {
        querySelector(selector) {
            return { '[data-gallery-grid]': grid, '[data-gallery-values]': values, '[data-gallery-alts]': alts }[selector] || null;
        }, addEventListener() {}
    };
    let init;
    vm.runInNewContext(fs.readFileSync(path.join(UI_SRC, 'media-gallery.js'), 'utf8'), {
        window: { WBUI: { register(fn) { init = fn; }, $$() { return [root]; }, markOnce() { return true; } } },
        document: { createElement: element }
    });
    init(root);
    const first = grid.children[0].querySelector('[data-gallery-alt]');
    const second = grid.children[1].querySelector('[data-gallery-alt]');
    assert.equal(first.value, '');
    assert.equal(second.value, '第二张');
    first.value = '第一张';
    first.dispatch('input');
    assert.equal(alts.value, '第一张\n第二张');
});
