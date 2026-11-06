const test = require('node:test');
const assert = require('node:assert/strict');

// 宿主适配必须显式注入：缺方法时抛可定位错误，不静默回落应用旧路径。
async function core() { return import('./core.js'); }

test('未配置宿主时保存/预览等操作明确失败，而不是猜测端点', async () => {
    const m = await core();
    assert.equal(m.hasHost(), false);
    assert.throws(() => m.getHost(), /宿主适配器未配置/);
    assert.throws(() => m.hostCall('save'), /宿主适配器未配置/);
});

test('注入宿主后按名调用窄操作，缺方法时报具体操作名', async () => {
    const m = await core();
    m.configureHost({ save(wb, doc) { return { document: doc }; } });
    assert.equal(m.hasHost(), true);
    assert.deepEqual(m.hostCall('save', {}, 'DOC'), { document: 'DOC' });
    assert.throws(() => m.hostCall('publish'), /宿主未实现操作：publish/);
});

test('媒体地址归一与全局块事实只经宿主，不内置业务表或媒体路径', async () => {
    const m = await core();
    m.configureHost({
        normalizeAssetURL(value) { return value ? 'https://media.test/' + value : ''; },
        blocks() { return [{ id: 'b1', name: '页脚' }]; }
    });
    assert.equal(m.normalizeAssetURL('a.jpg'), 'https://media.test/a.jpg');
    assert.deepEqual(m.hostBlocks().map(b => b.id), ['b1']);
});

test('configureHostMeta 用应用 bootstrap 覆盖 meta 与 schema 读取面', async () => {
    const m = await core();
    m.configureHostMeta({ pageId: 'p1', version: 3 }, { root: [], settings: {} }, { 'core.text': [] });
    assert.equal(m.meta.pageId, 'p1');
    assert.deepEqual(m.initialDoc.root, []);
    assert.deepEqual(m.componentSchemas['core.text'], []);
});
