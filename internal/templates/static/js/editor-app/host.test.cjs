const test = require('node:test');
const assert = require('node:assert/strict');

// 应用宿主适配器：保存/发布/预览/冲突确认的实际载荷与拒绝语义。
function loadHost(meta, options = {}) {
    const requests = [];
    let confirmAnswer = options.confirm !== false;
    const navigations = [];
    const document = {
        getElementById() { return null; },
        createElement() { return { style: {}, elements: {}, appendChild() {} }; },
        body: { appendChild() {} }
    };
    const window = {
        location: { href: 'https://site.test/workbench', origin: 'https://site.test', assign: url => navigations.push(url) },
        confirm: () => confirmAnswer
    };
    const request = async (url, body) => {
        requests.push({ url, body });
        const handler = options.respond && options.respond(url, body, requests.length);
        if (handler instanceof Error) throw handler;
        if (handler) return handler;
        return { version: 2 };
    };
    const csrfHeaders = extra => Object.assign({ 'X-CSRF-Token': 'csrf' }, extra || {});
    const mod = require('node:fs').readFileSync(__dirname + '/host.js', 'utf8').replace('export function createEditorHost', 'function createEditorHost');
    const sandbox = { module: { exports: {} }, exports: {}, URLSearchParams, Promise, JSON };
    const fn = new Function('module', 'exports', 'URL', 'URLSearchParams', 'Promise', 'JSON', 'window', 'document', 'fetch', mod + '\nmodule.exports.createEditorHost = createEditorHost;\nreturn module.exports;');
    const context = fn(sandbox.module, sandbox.exports, URL, URLSearchParams, Promise, JSON, window, document, options.fetch || (() => { throw new Error('unexpected fetch'); }));
    return {
        host: context.createEditorHost({ meta, request, csrfHeaders, csrfToken: () => 'csrf', normalizeMedia: v => v, openBusinessPreset() {} }),
        context,
        requests, navigations,
        setConfirm(v) { confirmAnswer = v; },
        window, document
    };
}

const pageMeta = () => ({ pageId: 'p1', draftPath: '/demo', target: { type: 'page', save: { path: '/api/page/draft/save' }, saveBody: { versionKey: 'expectedVersion' } } });

test('保存载荷只来自 meta 目标描述与当前版本，不猜路径', async () => {
    const { host, requests } = loadHost(pageMeta());
    const wb = { draftVersion: 1, doc: { root: [] } };
    const data = await host.save(wb, JSON.stringify(wb.doc));
    assert.equal(requests[0].url, '/api/page/draft/save');
    assert.equal(requests[0].body.expectedVersion, 1);
    assert.deepEqual(data, { version: 2 });
});

test('实例 409 需确认才带 confirmDetach 重试，取消则原样抛错', async () => {
    const meta = { pageId: 'i1', target: { type: 'instance', save: { path: '/api/instance/save' } } };
    const conflict = () => { const e = new Error('会转为独立文档'); e.status = 409; return e; };
    const { host, requests, setConfirm } = loadHost(meta, { respond: (url, body, n) => n === 1 ? conflict() : { version: 3 } });
    const wb = { draftVersion: 1, doc: { root: [] } };
    const ok = await host.save(wb, JSON.stringify(wb.doc));
    assert.equal(requests.length, 2);
    assert.equal(requests[1].body.confirmDetach, true);
    assert.equal(ok.version, 3);
    setConfirm(false);
    const cancelHost = loadHost(meta, { respond: () => conflict() }).host;
    await assert.rejects(() => cancelHost.save(wb, JSON.stringify(wb.doc)));
});

test('区块保存后仅在文档未变且回跳安全时导航，请求期间输入不回跳', async () => {
    const meta = { pageId: 'b1', returnUrl: '/admin/blocks?page=2', target: { type: 'block', save: { path: '/admin/blocks/save' } } };
    const { host, navigations } = loadHost(meta);
    const wb = { draftVersion: 1, doc: { root: [{ props: { text: '原文' } }] } };
    host.afterSave(wb, JSON.stringify(wb.doc));
    assert.deepEqual(navigations, ['https://site.test/admin/blocks?page=2']);
    wb.doc.root[0].props.text = '保存期间新输入';
    host.afterSave(wb, JSON.stringify({ root: [{ props: { text: '原文' } }] }));
    assert.equal(navigations.length, 1);
});

// 组合链路：工具的 saveDraft 真正调用应用宿主 save/afterSave。
// 两侧各自的单测都过，不代表钩子参数对得上——这里按真实调用顺序走一遍。
function loadToolApi(getHost) {
    const fs = require('node:fs');
    const path = require('node:path');
    const vm = require('node:vm');
    const file = path.join(__dirname, '../../../../../pkg/builder/editor/assets/js/editor/methods/api.js');
    const src = fs.readFileSync(file, 'utf8')
        .replace(/^import[\s\S]*?;\n/m, '').replace('export const apiMethods', 'var apiMethods');
    const ctx = vm.createContext({ getHost, console, JSON, fetch() { throw new Error('工具不得直接 fetch 应用端点'); } });
    vm.runInContext(src, ctx, { filename: 'api.js' });
    return ctx.apiMethods;
}

function toolEditor(api) {
    return Object.assign({
        doc: { root: [{ props: { text: '原文' } }] }, draftVersion: 1,
        busy: false, notice: '', saveState: '',
        clearBackup() { this.backupCleared = true; }, backupDoc() { this.backupPending = true; },
        renderUI() {}, setNotice(text) { this.notice = text; }
    }, api);
}

test('工具 saveDraft 组合应用宿主：区块保存按本次快照回跳，页面保存不回跳', async () => {
    const block = loadHost({ pageId: 'b1', returnUrl: '/admin/blocks?page=2', target: { type: 'block', save: { path: '/b/save' } } });
    const blockEditor = toolEditor(loadToolApi(() => block.host));
    assert.equal(await blockEditor.saveDraft(), true);
    assert.equal(block.requests.length, 1);
    assert.deepEqual(block.navigations, ['https://site.test/admin/blocks?page=2'], '区块保存成功后必须回跳');
    assert.equal(blockEditor.saveState, 'saved');

    const page = loadHost(pageMeta());
    const pageEditor = toolEditor(loadToolApi(() => page.host));
    assert.equal(await pageEditor.saveDraft(), true);
    assert.deepEqual(page.navigations, [], '页面保存不导航');
});

test('不安全回跳不导航', async () => {
    for (const path of ['//evil.test/x', 'https://evil.test/x', '/\\evil', "/admin\nblocks"]) {
        const { host, navigations } = loadHost({ pageId: 'b', returnUrl: path, target: { type: 'block', save: { path: '/b/save' } } });
        const wb = { draftVersion: 1, doc: { root: [] } };
        host.afterSave(wb, JSON.stringify(wb.doc));
        assert.equal(navigations.length, 0, path);
    }
});

test('发布按保存→构建→发布顺序，失败保留错误并恢复忙碌态', async () => {
    const { host, requests } = loadHost(pageMeta(), { respond: (url) => url.includes('/build') ? Object.assign(new Error('构建失败'), { status: 500 }) : { draftVersion: 2 } });
    const wb = { draftVersion: 1, doc: { root: [] }, busy: false, saveState: 'dirty', renderUI() {}, setNotice(t) { this.notice = t; },
        persistDraft(doc) { return host.save(this, doc); }, afterSave() {} };
    assert.equal(await host.publish(wb), false);
    assert.deepEqual(requests.map(r => r.url), ['/api/page/draft/save', '/api/page/build']);
    assert.equal(wb.busy, false);
    assert.equal(wb.saveState, 'error');
});

test('模板/块发布退化为保存草稿', () => {
    const { host } = loadHost({ pageId: 'b', saveBase: 'block', target: { type: 'block', save: { path: '/b' } } });
    let saved = 0;
    host.confirmPublish({ saveDraft() { saved++; } }, () => {});
    assert.equal(saved, 1);
});

test('预览按宿主模式提交：页面/块走 preview 系，模板/实例走模板端点且状态只在 query', async () => {
    const cases = [
        { base: 'page', url: '/workbench/preview', id: 'p1' },
        { base: 'block', url: '/workbench/block/preview', id: 'b1' },
        { base: 'template', url: '/workbench/template/preview', id: 't1' },
        { base: 'instance', url: '/workbench/template/preview', id: 'i1' }
    ];
    for (const c of cases) {
        const meta = { pageId: c.id, saveBase: c.base, projectId: 'proj', entityType: 'product', entityId: 'prod' };
        let request;
        const { host } = loadHost(meta, { fetch: async (url, options) => { request = { url, options }; return { ok: true, text: async () => 'PREVIEW' }; } });
        const wb = { previewState: 'loading', draftVersion: 1, doc: { root: [] } };
        assert.equal(await host.fetchPreview(wb), 'PREVIEW');
        assert.equal(request.url, c.url + '?previewState=loading');
        const body = new URLSearchParams(request.options.body);
        assert.equal(body.get('draftDocument'), JSON.stringify(wb.doc));
        assert.equal(body.get('csrf_token'), 'csrf');
        if (c.base === 'instance') assert.equal(body.get('instance'), c.id);
        else assert.equal(body.get('id'), c.id);
    }
});

test('已发布目标按工程与语言过滤，单侧失败仍保留另一侧', async () => {
    const meta = { pageId: 'p', projectId: 'proj', target: { type: 'page', save: { path: '/p/save' } } };
    const published = (id, project, lang) => ({ id, projectId: project, publications: [{ lang, activePath: '/zh/' + id, artifactId: 'a', artifactHash: 'h' }] });
    const fetchImpl = async (url) => {
        if (url.startsWith('/api/page/list')) return { ok: true, json: async () => ({ code: 200, data: [published('ok', 'proj', 'zh-CN'), published('other', 'x', 'zh-CN')] }) };
        return { ok: false, status: 403, json: async () => ({ code: 403, message: '无权限' }) };
    };
    const { host } = loadHost(meta, { fetch: fetchImpl });
    const result = await host.loadActionTargets({}, 'zh-CN');
    assert.deepEqual(result.targets, [{ kind: 'page', id: 'ok', label: '/zh/ok' }]);
    assert.equal(result.failures, 1);
});
