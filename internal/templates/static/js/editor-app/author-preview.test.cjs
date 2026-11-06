const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// 应用宿主接线下的动作目标/预览选择：工具只消费宿主提供的候选与端点行为。
const editorRoot = __dirname + '/../../../../../pkg/builder/editor/assets/js/editor';
const source = name => fs.readFileSync(editorRoot + '/' + name + '.js', 'utf8')
    .replace(/^import[\s\S]*?;\n/gm, '').replace(/export (function|const) /g, '$1 ');

function env(responses = {}) {
    class Element {
        constructor(tag) { this.tagName = tag.toUpperCase(); this.children = []; this.attributes = {}; this.events = {}; this.value = ''; this.style = {}; this.elements = {}; }
        appendChild(node) { node.parent = this; node.parentNode = this; this.children.push(node); if (node.name) this.elements[node.name] = node; return node; }
        setAttribute(k, v) { this.attributes[k] = v; } getAttribute(k) { return this.attributes[k] ?? null; }
        addEventListener(k, fn) { this.events[k] = fn; } contains(node) { return node === this || this.children.some(child => child.contains(node)); }
        get options() { return this.children; } submit() { this.submitted = true; }
    }
    const body = new Element('body'), panel = new Element('div');
    const frame = { name: 'wb-canvas', contentDocument: { documentElement: { lang: 'zh-CN' } } };
    body.appendChild(panel);
    const elements = { 'wb-canvas': frame };
    const document = { body, createElement: tag => new Element(tag), getElementById: id => elements[id] || walk(body).find(node => node.id === id) || null };
    const requests = [];
    const meta = { projectId: 'project-A', pageId: 'block-A', saveBase: 'block' };
    function active(lang, row) {
        const publication = (row.publications || []).find(pub => pub.lang === lang && pub.artifactId && pub.artifactHash && pub.activePath);
        return publication ? { kind: row.kind, id: row.id, label: publication.activePath } : null;
    }
    const host = {
        loadActionTargets(wb, lang) {
            return Promise.allSettled([
                read('/api/page/list'), read('/api/presentation/list')
            ]).then(results => {
                const failures = results.filter(r => r.status === 'rejected');
                if (failures.length === results.length) throw failures[0].reason;
                const data = results.map(r => r.status === 'fulfilled' ? r.value : []);
                const targets = [];
                data[0].forEach(row => { if (row.projectId === meta.projectId) { const t = active(lang, { ...row, kind: 'page' }); if (t) targets.push(t); } });
                data[1].forEach(row => { if (row.projectId === meta.projectId) { const t = active(lang, { ...row, kind: 'presentation' }); if (t) targets.push(t); } });
                return { targets, failures: failures.length };
            });
        },
        fetchPreview(wb) {
            requests.push({ preview: true, doc: wb.doc });
            return Promise.resolve('<html>preview</html>');
        }
    };
    function read(path) {
        return new Promise((resolve, reject) => {
            requests.push(path);
            const data = responses[path];
            if (data instanceof Error) reject(data); else resolve(data || []);
        });
    }
    const ctx = vm.createContext({ window: { location: { href: 'https://editor.test/workbench', origin: 'https://editor.test' } }, document, URL, URLSearchParams, Promise, JSON, meta,
        getHost: () => host, csrfHeaders: x => x });
    vm.runInContext(source('actions') + '\n' + source('preview') + '\n' + source('methods/canvas') + '\nthis.previewMethods=previewMethods;this.canvasMethods=canvasMethods;', ctx);
    return { ctx, document, body, panel, frame, elements, requests, host, meta, Element };
}
function walk(root) { return [root, ...(root.children || []).flatMap(walk)]; }
const published = (id, project = 'project-A', lang = 'zh-CN') => ({ id, projectId: project, publications: [{ lang, activePath: '/zh/' + id, artifactId: 'artifact-' + id, artifactHash: 'hash-' + id }] });
const settle = async () => { await new Promise(setImmediate); await new Promise(setImmediate); };

test('目标只列当前工程精确语言的真实发布，当前商品加购不生成选择器', () => {
    const e = env();
    const node = { id: 'buy', type: 'core.addToCart' }, self = { tab: 'content' };
    e.ctx.renderActionTargets({ self, node, panel: e.panel }, { projectId: 'project-A', componentActions: {} });
    assert.equal(e.panel.children.length, 0);
    assert.equal(e.requests.length, 0);
});

test('单侧列表无权限仍能选另一侧，动作只保存 ID 并进入原快照链', async () => {
    const e = env({ '/api/page/list': [published('shop')], '/api/presentation/list': new Error('无读取权限') });
    const node = { id: 'products', type: 'core.productList', actions: { list: { kind: 'page', pageId: 'old' }, other: { kind: 'local', partKey: 'list' } } };
    const snapshots = [];
    const self = { tab: 'content', findNode: () => node, snapshot: () => snapshots.push(JSON.stringify(node)), renderTree() {}, refreshCanvas() {}, renderUI() {} };
    e.ctx.renderActionTargets({ self, node, panel: e.panel }, { projectId: 'project-A', componentActions: { 'core.productList': [{ key: 'list', allowedKinds: ['page', 'presentation'] }] } });
    await settle();
    const select = walk(e.panel).find(n => n.tagName === 'SELECT');
    assert.equal(select.disabled, false);
    assert.ok(walk(e.panel).some(n => (n.textContent || '').includes('部分目标无法读取')));
    select.value = 'page:shop';
    select.events.change();
    assert.equal(snapshots.length, 1);
    assert.deepEqual(JSON.parse(JSON.stringify(node.actions)), { list: { kind: 'page', pageId: 'shop' }, other: { kind: 'local', partKey: 'list' } });
});

test('全部读取失败保留目标与可重试状态', async () => {
    const e = env({ '/api/page/list': new Error('稍后重试'), '/api/presentation/list': new Error('稍后重试') });
    const node = { id: 'account', type: 'core.userForms', actions: { authenticated: { kind: 'instance', nodeId: 'same', partKey: 'account' } } };
    let retries = 0;
    const self = { tab: 'content', findNode: () => node, syncInspector: () => retries++ };
    const meta = { projectId: 'project-A', componentActions: { 'core.userForms': [{ key: 'authenticated', allowedKinds: ['page', 'presentation', 'instance'] }] } };
    e.ctx.renderActionTargets({ self, node, panel: e.panel }, meta);
    await settle();
    assert.equal(walk(e.panel).find(n => n.tagName === 'SELECT').value, 'keep');
    const retry = walk(e.panel).find(n => n.tagName === 'BUTTON');
    assert.ok(retry);
    retry.events.click();
    assert.equal(retries, 1);
});

test('工具预览请求只经宿主 fetchPreview/fetchCanvasHTML，不自行拼端点', async () => {
    const e = env();
    const self = { doc: { settings: {}, root: [] }, draftVersion: 4, previewState: 'empty' };
    assert.equal(await e.ctx.canvasMethods.fetchCanvasHTML.call(self), '<html>preview</html>');
    assert.equal(e.requests[0].preview, true);
});

test('下拉只显示实际支持的状态，切换不触碰草稿和撤销栈', () => {
    const e = env(), select = new e.Element('select'), wrapper = new e.Element('label');
    select.children = [{ value: 'default' }, { value: 'empty' }, { value: 'success' }];
    e.elements['wb-preview-state'] = select; e.elements['wb-preview-states'] = wrapper;
    e.frame.contentWindow = { WPSitePreview: { active: true, state: 'default' }, WPSiteData: { supportedPreviewStates: () => ['default', 'empty'] } };
    let reloads = 0;
    const self = { previewState: 'default', doc: { root: [] }, undoStack: ['original'], flushCanvas: () => reloads++ };
    e.ctx.previewMethods.syncPreviewControls.call(self);
    assert.equal(wrapper.hidden, false);
    assert.equal(select.options[2].hidden, true);
    e.ctx.previewMethods.setPreviewState.call(self, 'success');
    assert.equal(reloads, 0);
    e.ctx.previewMethods.setPreviewState.call(self, 'empty');
    assert.equal(reloads, 1);
    assert.deepEqual(self.doc, { root: [] });
    assert.deepEqual(self.undoStack, ['original']);
    assert.equal(self.previewState, 'empty');
});

test('存量无效目标只提示不改绑定，默认流程可恢复', async () => {
    const e = env({ '/api/page/list': [] });
    const props = { collectionSource: { kind: 'products' }, titleField: 'product.title' };
    const node = { id: 'list', type: 'core.productList', props, actions: { list: { kind: 'page', pageId: 'directory-target' } } };
    let snapshots = 0;
    const self = { tab: 'content', findNode: () => node, snapshot: () => snapshots++, renderTree() {}, refreshCanvas() {}, renderUI() {} };
    e.ctx.renderActionTargets({ self, node, panel: e.panel }, { projectId: 'project-A', componentActions: { 'core.productList': [{ key: 'list', allowedKinds: ['page', 'presentation'] }] } });
    await settle();
    const select = walk(e.panel).find(n => n.tagName === 'SELECT');
    assert.equal(select.value, 'page:directory-target');
    assert.ok(select.options.some(option => (option.textContent || '').includes('当前目标不可用')));
    assert.equal(node.actions.list.pageId, 'directory-target');
    assert.equal(snapshots, 0);
    select.value = '';
    select.events.change();
    assert.equal(node.actions, undefined);
    assert.equal(snapshots, 1);
});
