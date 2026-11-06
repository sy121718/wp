const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// 加载工具内核（core 的依赖目录需要桩）；只验通用状态语义，不触应用端点。
function loadCore() {
    const source = fs.readFileSync(__dirname + '/core.js', 'utf8')
        .replace(/^export\s*\{[^}]*\}\s*from\s*'[^']*';?/gm, '')
        .replace(/^export /gm, '');
    const document = { getElementById() { return null; }, querySelector() { return null; } };
    const ctx = vm.createContext({ document, console, JSON, URL, URLSearchParams, window: {}, sessionStorage: { getItem() { return null; } } });
    vm.runInContext(source, ctx, { filename: 'core.js' });
    return ctx;
}

function loadApi(core) {
    const source = fs.readFileSync(__dirname + '/methods/api.js', 'utf8')
        .replace(/^import[\s\S]*?;\n/gm, '').replace('export const apiMethods', 'var apiMethods');
    // fetch 桩只在工具误用全局取端点时被命中；宿主已提供 save 时不应走到。
    let fetchCalls = 0;
    const ctx = vm.createContext({ getHost: core.getHost, console, JSON, setTimeout, clearTimeout,
        fetch() { fetchCalls++; throw new Error('工具不得直接 fetch 应用端点'); } });
    vm.runInContext(source, ctx);
    return { api: ctx.apiMethods, fetchCalls: () => fetchCalls };
}

function editor(api, host) {
    let saveCalls = 0;
    const wb = Object.assign({
        doc: { root: [{ id: 'h', props: { text: '原文' } }] }, draftVersion: 1,
        busy: false, saveState: 'dirty', undoStack: [], redoStack: [], notice: '',
        backupCleared: 0, backupCount: 0,
        clearBackup() { this.backupCleared++; }, backupDoc() { this.backupCount++; },
        renderUI() {}, setNotice(text) { this.notice = text; },
        afterSave: api.afterSave
    }, api);
    return { wb, saveCalls: () => saveCalls };
}

test('保存成功后版本与状态更新，迟到的新输入不被标记为已保存', async () => {
    let resolve;
    const host = {
        save(wb, savedDocument) { return new Promise(r => { resolve = () => r({ draftVersion: 2 }); }); },
        afterSave() {}, onSaved() {}
    };
    const core = loadCore();
    core.configureHost(host);
    const { api } = loadApi(core);
    const { wb } = editor(api, host);
    const saving = wb.saveDraft();
    wb.doc.root[0].props.text = '保存期间的新文字';
    resolve();
    assert.equal(await saving, true);
    assert.equal(wb.draftVersion, 2);
    assert.equal(wb.saveState, 'dirty');
    assert.equal(wb.backupCleared, 0);
    assert.equal(wb.doc.root[0].props.text, '保存期间的新文字');
});

test('保存失败保留版本与输入，并恢复忙碌态', async () => {
    const host = {
        save() { const e = new Error('版本冲突'); e.status = 409; return Promise.reject(e); },
        afterSave() {}, onSaved() {}
    };
    const core = loadCore();
    core.configureHost(host);
    const { api } = loadApi(core);
    const { wb } = editor(api, host);
    const before = JSON.stringify(wb.doc);
    assert.equal(await wb.saveDraft(), false);
    assert.equal(wb.busy, false);
    assert.equal(wb.saveState, 'error');
    assert.equal(wb.draftVersion, 1);
    assert.equal(JSON.stringify(wb.doc), before);
    assert.match(wb.notice, /版本冲突/);
});

test('保存与发布只经宿主操作，工具不自行拼端点', async () => {
    let published = 0;
    const host = { save() { return Promise.resolve({ version: 2 }); }, afterSave() {}, onSaved() {}, publish() { published++; return Promise.resolve(true); } };
    const core = loadCore();
    core.configureHost(host);
    const { api, fetchCalls } = loadApi(core);
    const { wb } = editor(api, host);
    await wb.saveDraft();
    await wb.publishFlow();
    assert.equal(published, 1);
    assert.equal(fetchCalls(), 0);
});
