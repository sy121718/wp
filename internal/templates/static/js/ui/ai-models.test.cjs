/* ai-models.test.cjs — 模型目录区的草稿行编辑与请求对象组装（node:test + vm，无浏览器）。
 *
 * 保存成功后的界面由服务端回 Jet 片段（HTMX 交换），前端不再重绘行；这里守的是剩余那部分
 * 「生产上不报错、只是静默变坏」的地方：
 *
 *   1. 增删草稿行不重排行内带索引的字段名（input_text_N / input_image_N）——
 *      服务端按 index 取 checkbox，索引错位等于把「图片」存成「文本」。
 *   2. payload builder 不把草稿行转成 models[]、或带上表单里的平铺字段 ——
 *      服务端拿不到目录，保存会把模型清空。
 *   3. 空 id 的草稿行没被丢弃 —— 服务端要求有 ID，空行会让整批保存失败。
 *   4. 删最后一行后空态不刷新。
 *
 * 运行：node --test internal/templates/static/js/ui/ai-models.test.cjs
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
// 公共控件属主在 pkg/view/ui；本测试读取真实源，不复制到旧位置。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');

const MODELS_SRC = path.join(UI_SRC, 'ai-models.js');

// ── 极简 DOM 桩（与 mcp-token.test.cjs 同源，只实现用到的部分） ────────────────
function makeNode(tag) {
    const el = {
        tagName: String(tag || 'div').toUpperCase(),
        children: [],
        attrs: {},
        _text: '',
        _hidden: false,
        value: '',
        checked: false,
        parentNode: null,
        get textContent() { return this._text; },
        set textContent(v) { this._text = String(v); },
        set innerHTML(_v) { throw new Error('禁止用 innerHTML 拼内容'); },
        get hidden() { return this._hidden; },
        set hidden(v) {
            this._hidden = !!v;
            if (this._hidden) { this.attrs.hidden = ''; } else { delete this.attrs.hidden; }
        },
        setAttribute(k, v) {
            this.attrs[k] = String(v);
            if (k === 'hidden') { this._hidden = true; }
        },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; },
        hasAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k); },
        removeAttribute(k) {
            delete this.attrs[k];
            if (k === 'hidden') { this._hidden = false; }
        },
        appendChild(c) {
            if (c && String(c.tagName).toUpperCase() === '#FRAGMENT') {
                c.children.slice().forEach((ch) => { this.appendChild(ch); });
                return c;
            }
            // HTML 语义：把已经在 DOM 里的节点再 append 到别处 = 移动，不是复制。
            // 少了这一步，「先 appendChild 再 appendChild」会在两处各留一份，
            // 删除时只摘掉其中一份 —— 测试看起来绿，浏览器里却是两行。
            if (c && c.parentNode && c.parentNode !== this) {
                c.parentNode.removeChild(c);
            }
            this.children.push(c);
            c.parentNode = this;
            return c;
        },
        insertBefore(c, ref) {
            const i = ref ? this.children.indexOf(ref) : -1;
            if (i < 0) { this.children.push(c); } else { this.children.splice(i, 0, c); }
            c.parentNode = this;
            return c;
        },
        removeChild(c) {
            const i = this.children.indexOf(c);
            if (i >= 0) { this.children.splice(i, 1); c.parentNode = null; }
            return c;
        },
        querySelector(sel) { return this._find(sel)[0] || null; },
        querySelectorAll(sel) { return this._find(sel); },
        closest(sel) { return this._closest(sel); },
        _closest(sel) {
            let n = this;
            while (n) {
                if (matches(n, sel)) { return n; }
                n = n.parentNode;
            }
            return null;
        },
        _find(sel) {
            const group = String(sel).split(',').map((x) => x.trim()).filter(Boolean);
            const out = [];
            const walk = (n) => {
                (n.children || []).forEach((c) => {
                    if (group.some((g) => matches(c, g))) { out.push(c); }
                    walk(c);
                });
            };
            walk(this);
            return out;
        },
        get firstElementChild() { return this.children[0] || null; },
        get firstChild() { return this.children[0] || null; }
    };
    return el;
}

function matches(node, sel) {
    let s = String(sel).trim();
    // 生产代码用 ':scope > .ai-model-row' 数行、用 '.ai-model-row' 找祖先。
    // 桩里没有真实选择器引擎，这里只把它归一成「类名匹配」这一种语义：
    // 去掉 :scope 前缀与子代组合符，剩下的 .class 参与匹配。
    if (s.indexOf(':scope') === 0) { s = s.slice(':scope'.length).trim(); }
    if (s.indexOf('>') === 0) { s = s.slice(1).trim(); }
    if (s[0] === '.') {
        const want = s.slice(1);
        const classes = String(node.getAttribute('class') || '').split(/\s+/).filter(Boolean);
        return classes.indexOf(want) >= 0;
    }
    // 只支持本文件用到的三种前缀形式：标签、[attr]、[attr="v"]，以及标签+属性的组合。
    const tagPart = /^([a-zA-Z][a-zA-Z0-9]*)/.exec(s);
    if (tagPart) {
        if (String(node.tagName).toLowerCase() !== tagPart[1].toLowerCase()) { return false; }
        s = s.slice(tagPart[1].length);
    }
    const attrs = s.match(/\[[^\]]+\]/g) || [];
    return attrs.every((a) => {
        const body = a.slice(1, -1);
        const eq = body.indexOf('=');
        if (eq < 0) { return node.hasAttribute(body); }
        const name = body.slice(0, eq);
        const want = body.slice(eq + 1).replace(/^["']|["']$/g, '');
        return node.getAttribute(name) === want;
    });
}

function deepClone(node) {
    const c = makeNode(node.tagName);
    c._text = node._text;
    c.value = node.value;
    c.checked = node.checked;
    c._hidden = node._hidden;
    Object.keys(node.attrs).forEach((k) => { c.attrs[k] = node.attrs[k]; });
    (node.children || []).forEach((ch) => c.appendChild(deepClone(ch)));
    return c;
}

function templateOf(id, root) {
    const content = makeNode('#fragment');
    content.appendChild(root);
    return {
        tagName: 'TEMPLATE',
        id: id,
        content: Object.assign(content, {
            cloneNode() {
                const f = makeNode('#fragment');
                f.appendChild(deepClone(root));
                return f;
            }
        })
    };
}

// 与 admin/ai/provider_models.html 的 #tpl-ai-model-row 同一组 data-field / data-check /
// data-index-name 指令。值是空的：真实数据由处理器填。
function modelRowTemplate() {
    const row = makeNode('div');
    // 生产模板的行根节点就带这个类：JS 靠它数行、靠它找要删的那一行。
    row.setAttribute('class', 'ai-model-row');
    row.setAttribute('data-row', '');

    const idInput = makeNode('input'); idInput.setAttribute('type', 'text'); idInput.setAttribute('name', 'modelId'); idInput.setAttribute('data-field', 'id');
    const nameInput = makeNode('input'); nameInput.setAttribute('type', 'text'); nameInput.setAttribute('name', 'modelDisplayName'); nameInput.setAttribute('data-field', 'displayName');
    const ctxInput = makeNode('input'); ctxInput.setAttribute('type', 'number'); ctxInput.setAttribute('name', 'modelContextWindow'); ctxInput.setAttribute('data-field', 'contextWindow');
    const maxInput = makeNode('input'); maxInput.setAttribute('type', 'number'); maxInput.setAttribute('name', 'modelMaxOutputTokens'); maxInput.setAttribute('data-field', 'maxOutputTokens');

    const delBtn = makeNode('button'); delBtn.setAttribute('type', 'button'); delBtn.setAttribute('data-row-remove', '');

    // 与服务端直出的行同一套标记：SSR 行的勾选框也带 data-index-name / data-check
    // （provider_models.html 里两处一字不差），否则 reindex 与取值只对克隆行有效。
    const textBox = makeNode('input');
    textBox.setAttribute('type', 'checkbox');
    textBox.setAttribute('name', 'input_text_0');
    textBox.setAttribute('data-index-name', 'input_text');
    textBox.setAttribute('data-check', 'text');
    textBox.value = 'text';

    const imageBox = makeNode('input');
    imageBox.setAttribute('type', 'checkbox');
    imageBox.setAttribute('name', 'input_image_0');
    imageBox.setAttribute('data-index-name', 'input_image');
    imageBox.setAttribute('data-check', 'image');
    imageBox.value = 'image';

    [idInput, nameInput, ctxInput, maxInput, delBtn, textBox, imageBox].forEach((n) => row.appendChild(n));
    return row;
}

function makeEnv(nodes) {
    const clicks = [];
    const doc = {
        getElementById: (id) => nodes[id] || null,
        createElement: (t) => makeNode(t),
        createDocumentFragment: () => makeNode('#fragment'),
        // 生产代码在加载时就用委托监听行内的「加一行 / 移除一行」。
        addEventListener: (type, fn) => { if (type === 'click') { clicks.push(fn); } },
        querySelector: (sel) => {
            const all = Object.keys(nodes).map((k) => nodes[k]);
            for (let i = 0; i < all.length; i++) {
                const n = all[i];
                if (n && typeof n.matchesSelf === 'function' && n.matchesSelf(sel)) { return n; }
            }
            return null;
        }
    };
    const builders = {};
    const win = {
        document: doc,
        WBUI: {
            writeBase: {
                // 写基座提交前按 data-write-builder 查注册表；这里只收不调，
                // 具体调用在用例里显式做，好断言「payload 到底长什么样」。
                registerPayloadBuilder: (name, fn) => {
                    assert.equal(typeof fn, 'function');
                    builders[name] = fn;
                }
            }
        }
    };
    win.window = win;
    const ctx = vm.createContext(win);
    vm.runInContext(fs.readFileSync(MODELS_SRC, 'utf8'), ctx, { filename: MODELS_SRC });
    return { builders, clicks, api: win.WBUI.aiModels };
}

// 造一个模型区：宿主节点 + 表单 + 行容器 + 空态段落 + 结果位 + 行模板。
function modelsHost() {
    const host = makeNode('div');
    host.setAttribute('data-ai-models-host', '');

    // 生产模板里行容器在**保存表单内部**：SaveModelsReq 的 models[] 由写基座的组装器
    // 从这张表单的 DOM 里读出来，所以桩也必须把 rowsHost 挂进 form —— 挂在宿主上
    // 会让 collectModels 一路找空。
    const form = makeNode('form');
    const versionInput = makeNode('input'); versionInput.setAttribute('type', 'hidden'); versionInput.setAttribute('name', 'version'); versionInput.value = '3';
    const rowsHost = makeNode('div'); rowsHost.setAttribute('data-models-rows', '');
    const providerIdInput = makeNode('input'); providerIdInput.setAttribute('type', 'hidden'); providerIdInput.setAttribute('name', 'providerId'); providerIdInput.value = '5';
    const empty = makeNode('p'); empty.setAttribute('data-models-empty', '');
    const status = makeNode('p'); status.setAttribute('data-models-status', '');

    form.appendChild(versionInput);
    form.appendChild(providerIdInput);
    form.appendChild(rowsHost);
    host.appendChild(form);
    host.appendChild(empty);
    host.appendChild(status);

    const tpl = templateOf('tpl-ai-model-row', modelRowTemplate());
    return { host, form, rowsHost, empty, status, versionInput, nodes: { 'tpl-ai-model-row': tpl } };
}

// 服务端 ModelRowResp 的真实 JSON 形状。
//
// 只有事实：行序号与「该不该打勾」都是显示派生，服务端不再下发（Index / TextChecked /
// ImageChecked 已经从 DTO 里删掉）。所以夹具里也不许带它们 —— 带着会让本文件的断言
// 通过、真实响应却取不到值。
function modelRow(over) {
    return Object.assign({
        id: 'deepseek-chat',
        displayName: 'DeepSeek Chat',
        contextWindow: 64000,
        maxOutputTokens: 8192,
        inputTypes: ['text']
    }, over || {});
}

function providerFact(over) {
    return Object.assign({
        id: 5,
        providerKey: 'deepseek',
        displayName: '深度求索',
        baseUrl: 'https://api.deepseek.com',
        protocol: 'openai_chat_completions',
        status: 1,
        sort: 0,
        models: [],
        hasApiKey: true,
        version: 4,
        updateTime: 1767225600000
    }, over || {});
}


// ── 草稿行的本地增删 ─────────────────────────────────────────────────────────
//
// 这两件事**没有接口**：草稿行是用户还没保存的表单状态。所以这里守的是
// 「本地改完之后，表单看上去还像一份能提交的表单」——行序号（input_text_N 的 N）
// 必须跟着重排，否则服务端按 index 取值会把「图片」存成「文本」。

// 服务端直出的行：与模板完全同形（行根节点带 .ai-model-row 与 data-* 指令）。
function ssrRow(row) {
    const node = modelRowTemplate();
    node.setAttribute('data-field-id', '');
    node.querySelector('[data-field="id"]').value = row.id;
    node.querySelector('[data-field="displayName"]').value = row.displayName || '';
    node.querySelector('[data-field="contextWindow"]').value = String(row.contextWindow || '');
    node.querySelector('[data-field="maxOutputTokens"]').value = String(row.maxOutputTokens || '');
    const types = Array.isArray(row.inputTypes) ? row.inputTypes : [];
    node.querySelector('[data-check="text"]').checked = types.indexOf('text') >= 0;
    node.querySelector('[data-check="image"]').checked = types.indexOf('image') >= 0;
    return node;
}

function seedRows(h, rows) {
    rows.forEach((row, i) => {
        const node = ssrRow(row);
        // SSR 行与克隆行同一套标记：服务端按表单里第几条取值。
        node.querySelectorAll('[data-index-name]').forEach((input) => {
            input.setAttribute('name', input.getAttribute('data-index-name') + '_' + i);
        });
        h.rowsHost.appendChild(node);
    });
    if (rows.length) { h.empty.setAttribute('hidden', ''); }
}

test('addRow 在末尾加一行空白草稿并重排序号', () => {
    const h = modelsHost();
    const env = makeEnv(h.nodes);
    seedRows(h, [modelRow()]);

    assert.equal(env.api.addRow(h.host), true, '加行应成功');
    assert.equal(h.rowsHost.children.length, 2, '应有第二行');
    const added = h.rowsHost.children[1];
    assert.equal(added.querySelector('[data-field="id"]').value, '', '新行应是空白草稿');
    assert.equal(added.querySelector('[data-check="text"]').getAttribute('name'), 'input_text_1', '新行勾选框名应为 input_text_1');
    assert.equal(h.empty.hidden, true, '有行时空态应隐藏');
});

test('removeRow 摘掉中间一行并把后面各行序号前移', () => {
    const h = modelsHost();
    const env = makeEnv(h.nodes);
    seedRows(h, [modelRow({ id: 'a' }), modelRow({ id: 'b' }), modelRow({ id: 'c' })]);
    assert.equal(h.rowsHost.children.length, 3);

    const middle = h.rowsHost.children[1];
    const button = middle.querySelector('[data-row-remove]');
    assert.ok(button, '行里应有移除按钮');

    assert.equal(env.api.removeRow(button), true, '移除应成功');
    assert.equal(h.rowsHost.children.length, 2, '应剩两行');

    // 原来的第三行现在是第二行：字段名必须跟着改，否则服务端按 index 取错值。
    const shifted = h.rowsHost.children[1];
    assert.equal(shifted.querySelector('[data-field="id"]').value, 'c', '剩下的应是原来第三行');
    assert.equal(shifted.querySelector('[data-check="text"]').getAttribute('name'), 'input_text_1', '前移后序号应为 1');
});

test('移除最后一行后露出空态', () => {
    const h = modelsHost();
    const env = makeEnv(h.nodes);
    seedRows(h, [modelRow()]);
    assert.equal(h.empty.hidden, true);

    const button = h.rowsHost.children[0].querySelector('[data-row-remove]');
    assert.equal(env.api.removeRow(button), true);
    assert.equal(h.rowsHost.children.length, 0);
    assert.equal(h.empty.hidden, false, '一行都没有时应露出空态');
});

// ── 保存 payload（data-write-builder） ───────────────────────────────────────
//
// 模板里的行是**平铺字段**，服务端 SaveModelsReq 要的是 models[]。少了组装器，
// 写基座只能把平铺字段原样提交，服务端拿不到 models —— 目录会被清空。
// 这几条守的就是「提交出去的那份 JSON 到底长什么样」。

function payloadOf(env, form) {
    assert.ok(env.builders['ai-provider-models'], '应注册 ai-provider-models 组装器');
    return env.builders['ai-provider-models'](form);
}

test('保存 payload 把编辑过的现有行与新增行一起转成 models[]', () => {
    const h = modelsHost();
    const env = makeEnv(h.nodes);
    seedRows(h, [modelRow({ inputTypes: ['text', 'image'] })]);

    // ① 编辑现有行：改 ID 与上下文窗口，取消图片勾选。
    const first = h.rowsHost.children[0];
    first.querySelector('[name="modelId"]').value = ' deepseek-chat-v3 ';
    first.querySelector('[name="modelDisplayName"]').value = 'V3';
    first.querySelector('[name="modelContextWindow"]').value = '128000';
    first.querySelector('[name="modelMaxOutputTokens"]').value = '4096';
    first.querySelector('[name="input_image_0"]').checked = false;

    // ② 本地新增一行（草稿），填写后应一并提交。
    assert.equal(env.api.addRow(h.host), true);
    const second = h.rowsHost.children[1];
    second.querySelector('[name="modelId"]').value = 'reasoner';
    second.querySelector('[name="modelContextWindow"]').value = '';
    second.querySelector('[name="modelMaxOutputTokens"]').value = 'not-a-number';
    second.querySelector('[name="input_text_1"]').checked = true;

    // ③ 再加一行只选了「输入类型」、ID 还空着的草稿：整行丢弃。
    assert.equal(env.api.addRow(h.host), true);
    h.rowsHost.children[2].querySelector('[name="input_text_2"]').checked = true;

    const body = payloadOf(env, h.form);

    // 精准等式：数字是 number 不是字符串，勾选按 checkbox 的真实状态，
    // ID 去首尾空格，空 ID 行整行不提交。
    assert.deepEqual(JSON.parse(JSON.stringify(body)), {
        providerId: 5,
        version: 3,
        models: [
            { id: 'deepseek-chat-v3', displayName: 'V3', contextWindow: 128000, maxOutputTokens: 4096, inputTypes: ['text'] },
            // 本地新增的草稿行没填显示名：显示名是可选文案，空串照实提交。
            { id: 'reasoner', displayName: '', contextWindow: 0, maxOutputTokens: 0, inputTypes: ['text'] }
        ]
    });
});

test('保存 payload 里本地删除的行不再出现', () => {
    const h = modelsHost();
    const env = makeEnv(h.nodes);
    seedRows(h, [modelRow({ id: 'a' }), modelRow({ id: 'b' }), modelRow({ id: 'c' })]);

    const middle = h.rowsHost.children[1];
    assert.equal(env.api.removeRow(middle.querySelector('[data-row-remove]')), true);

    const body = payloadOf(env, h.form);
    assert.deepEqual(JSON.parse(JSON.stringify(body.models.map((m) => m.id))), ['a', 'c'],
        '删掉的 b 不应再出现在 payload 里');
    // 序号前移后，剩下的行仍按新顺序取值。
    assert.equal(body.models[1].inputTypes[0], 'text');
});

test('保存 payload 在没有行时是空数组而不是 null', () => {
    const h = modelsHost();
    const env = makeEnv(h.nodes);
    const body = payloadOf(env, h.form);
    assert.deepEqual(JSON.parse(JSON.stringify(body.models)), [],
        '一行都没有时必须是 []（清空目录是合法意图），不能是 null/undefined');
});

test('删掉 SSR 第一行后，第二行的图片标记不丢且序号前移', () => {
    const h = modelsHost();
    const env = makeEnv(h.nodes);
    seedRows(h, [
        modelRow({ id: 'first', inputTypes: ['text'] }),
        modelRow({ id: 'second', inputTypes: ['text', 'image'] })
    ]);

    assert.equal(env.api.removeRow(h.rowsHost.children[0].querySelector('[data-row-remove]')), true);
    const body = payloadOf(env, h.form);
    assert.equal(body.models.length, 1);
    assert.equal(body.models[0].id, 'second');
    assert.deepEqual(JSON.parse(JSON.stringify(body.models[0].inputTypes.slice().sort())), ['image', 'text'],
        '图片标记不能因为序号重排而丢');
});
