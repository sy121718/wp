const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// write-base.js 的 DOM 依赖必须被桩住：这里只测**请求体整形**这一层（②③ 层靠页面结构测试）。
//
// 为什么用 vm 而不是真浏览器：整形是纯函数（FormData 进、FormData 出），
// 用最小 DOM 桩就能覆盖全部分支，且能在 CI 里跑（无浏览器）。
// 支付：桩要够真实 —— 尤其 FormData 的多值语义（append 累加、set 覆盖），
// 那正是「同名多值收数组」与「删空值键」两条判据的落点。

// ── 最小 DOM 桩 ─────────────────────────────────────────────────────────────

class FakeFormData {
    constructor() { this._v = []; }
    append(k, v) { this._v.push([k, String(v)]); }
    set(k, v) {
        this._v = this._v.filter(([kk]) => kk !== k);
        this._v.push([k, String(v)]);
    }
    delete(k) { this._v = this._v.filter(([kk]) => kk !== k); }
    get(k) {
        const hit = this._v.find(([kk]) => kk === k);
        return hit ? hit[1] : null;
    }
    getAll(k) { return this._v.filter(([kk]) => kk === k).map(([, v]) => v); }
    has(k) { return this._v.some(([kk]) => kk === k); }
    entries() { return this._v.slice(); }
}

// 元素桩：只实现 collect() 用到的属性。
//
// getAttribute 必须认识 name —— collect 用 `el.getAttribute('name')` 取字段名，
// 桩里返回 null 会让所有字段被当成无名字段静默跳过（实测踩过：body 全空）。
function el(props) {
    const attrs = {};
    if (props && props.name) attrs.name = props.name;
    return Object.assign({
        type: 'text',
        value: '',
        checked: false,
        tagName: 'INPUT',
        hasAttribute: (n) => n === 'data-keep-empty' && !!(props && props.keepEmpty),
        getAttribute: (n) => (Object.prototype.hasOwnProperty.call(attrs, n) ? attrs[n] : null),
    }, props, {
        // 覆盖 props 里可能带进来的同名实现
        hasAttribute: (n) => (n === 'data-keep-empty'
            ? !!(props && props.keepEmpty)
            : (props && props.hasAttribute ? props.hasAttribute(n) : false)),
        getAttribute: (n) => (Object.prototype.hasOwnProperty.call(attrs, n) && !(props && props.getAttribute) 
            ? attrs[n]
            : (props && props.getAttribute ? props.getAttribute(n) : null)),
    });
}

// 表单桩：querySelectorAll 按 name 过滤。
function form(attrs, fields, opts) {
    opts = opts || {};
    const a = attrs || {};
    return {
        tagName: 'FORM',
        _fields: fields || [],
        _templates: opts.templates || [],
        hasAttribute: (n) => Object.prototype.hasOwnProperty.call(a, n),
        getAttribute: (n) => (Object.prototype.hasOwnProperty.call(a, n) ? a[n] : null),
        querySelectorAll: (sel) => {
            if (sel === '[name]') return fields || [];
            if (sel.indexOf('template[') === 0) {
                const m = sel.match(/template\[name="(.*?)"\]/);
                const name = m ? m[1].replace(/\\(.)/g, '$1') : '';
                return (opts.templates || []).filter((t) => t.name === name);
            }
            if (sel.indexOf('[data-keep-empty]') === 0) {
                return (fields || []).filter((f) => f.hasAttribute('data-keep-empty'));
            }
            if (sel === '[data-write-status]') return opts.status ? [opts.status] : [];
            return [];
        },
        querySelector: (sel) => {
            const all = [];
            return all[0] || null;
        },
        setAttribute: () => {},
        getAttributeNames: () => Object.keys(a),
    };
}

// template 桩：content 是 DocumentFragment 的替身，直接支持 querySelectorAll。
//
// 基座读的是 tpl.content.querySelectorAll（不 cloneNode）—— 与真实 DOM 一致：
// template.content 本身就是 DocumentFragment，可以直接查询。
function tpl(name, rowFields) {
    return {
        name,
        content: {
            querySelectorAll: (sel) => (sel === '[name]' ? rowFields : []),
        },
    };
}

// ── 加载被测模块 ────────────────────────────────────────────────────────────

function load() {
    const sandbox = {
        window: {},
        document: {
            addEventListener: () => {},
            readyState: 'complete',
            querySelector: () => null,
            createElement: () => ({
                setAttribute: () => {}, appendChild: () => {}, remove: () => {},
                querySelector: () => null, textContent: '', hidden: false,
            }),
        },
        FormData: FakeFormData,
        CustomEvent: class { constructor(t) { this.type = t; } },
        console,
    };
    sandbox.window.htmx = {};
    sandbox.window.location = { assign: () => {}, reload: () => {} };
    sandbox.window.CSS = { escape: (s) => s };
    vm.createContext(sandbox);
    vm.runInContext(
        fs.readFileSync(__dirname + '/write-base.js', 'utf8'),
        sandbox,
    );
    return sandbox.window.WBUI.writeBase;
}

// ── 判据 ────────────────────────────────────────────────────────────────────

test('buildBody 删空值键 —— `sort=` 不能让后端 *int 绑成 0', () => {
    const m = load();
    const f = form({ 'data-write': 'update' }, [
        el({ name: 'name', value: '颜色' }),
        el({ name: 'sort', value: '' }),      // 空 → 不发
        el({ name: 'qty', value: '0' }),      // 0 是有效值 → 必发
    ]);
    const body = m.buildBody(f, null);
    assert.equal(body.get('name'), '颜色');
    assert.equal(body.has('sort'), false, '空值键必须删掉');
    assert.equal(body.get('qty'), '0', '0 是有效值，不能当成空');
});

test('buildBody 同名多值保留为多个键（服务端绑成 []string）', () => {
    const m = load();
    const f = form({ 'data-write': 'update' }, [
        el({ name: 'tags', value: 'a' }),
        el({ name: 'tags', value: 'b' }),
    ]);
    const body = m.buildBody(f, null);
    assert.equal(body.getAll('tags').join(','), 'a,b');
});

test('buildBody 标量不转换 —— `qty=3` 原样发字符串', () => {
    const m = load();
    const f = form({ 'data-write': 'update' }, [el({ name: 'qty', value: '3' })]);
    const body = m.buildBody(f, null);
    assert.equal(body.get('qty'), '3');
    assert.equal(typeof body.get('qty'), 'string');
});

test('buildBody 未勾选的 checkbox 不发键', () => {
    const m = load();
    const f = form({ 'data-write': 'update' }, [
        el({ name: 'on', type: 'checkbox', checked: false, value: 'on' }),
        el({ name: 'off', type: 'checkbox', checked: true, value: 'on' }),
    ]);
    const body = m.buildBody(f, null);
    assert.equal(body.has('on'), false);
    assert.equal(body.get('off'), 'on');
});

test('buildBody data-keep-empty 字段的空串必须保留', () => {
    const m = load();
    const emptyKeep = el({ name: 'parentId', value: '', keepEmpty: true });
    const f = form({ 'data-write': 'update' }, [emptyKeep]);
    const body = m.buildBody(f, null);
    assert.equal(body.has('parentId'), true, '空串有语义的字段不能被删掉');
    assert.equal(body.get('parentId'), '');
});

test('buildBody data-write-json 把多行 template 组装成数组', () => {
    const m = load();
    const f = form({ 'data-write': 'update', 'data-write-json': 'values' }, [
        el({ name: 'name', value: '颜色' }),
    ], {
        templates: [
            tpl('values', [el({ name: 'key', value: '色号' }), el({ name: 'value', value: '红' })]),
            tpl('values', [el({ name: 'key', value: '尺码' }), el({ name: 'value', value: 'L' })]),
        ],
    });
    const body = m.buildBody(f, null);
    const raw = body.get('values');
    assert.ok(raw, 'values 必须发出去');
    assert.deepEqual(JSON.parse(raw), [
        { key: '色号', value: '红' },
        { key: '尺码', value: 'L' },
    ]);
});

test('buildBody data-write-json-single 声明的一行 template 发成对象', () => {
    const m = load();
    const f = form({
        'data-write': 'update', 'data-write-json': 'ruleParams',
        'data-write-json-single': 'ruleParams',
    }, [], {
        templates: [tpl('ruleParams', [el({ name: 'days', value: '30' })])],
    });
    const body = m.buildBody(f, null);
    assert.deepEqual(JSON.parse(body.get('ruleParams')), { days: '30' });
});

test('buildBody 全空的行被跳过（不产生空对象）', () => {
    const m = load();
    const f = form({ 'data-write': 'update', 'data-write-json': 'values' }, [], {
        templates: [
            tpl('values', [el({ name: 'key', value: '' })]),
            tpl('values', [el({ name: 'key', value: '有值' })]),
        ],
    });
    const body = m.buildBody(f, null);
    assert.deepEqual(JSON.parse(body.get('values')), [{ key: '有值' }]);
});

test('buildBody 数组型 data-write-json 一行都没有时发空数组（清空是明确意图）', () => {
    const m = load();
    const f = form({ 'data-write': 'update', 'data-write-json': 'values' }, [], {});
    const body = m.buildBody(f, null);
    // 「删光所有行」= 清空；「键不存在」= 本次不改。两者混同的后果是
    // 用户删完所有行后保存什么都没发生，而界面已显示成功。
    assert.equal(body.has('values'), true, '清空必须发空数组，而不是省略该键');
    assert.equal(body.get('values'), '[]');
});

test('buildBody 单对象型 data-write-json 清空时不发键（没有空对象的合法形态）', () => {
    const m = load();
    const f = form({
        'data-write': 'update', 'data-write-json': 'ruleParams',
        'data-write-json-single': 'ruleParams',
    }, [], {});
    const body = m.buildBody(f, null);
    // {} 会被后端当成「填了个空结构」，与「本次不改」不同，所以清空时省略。
    assert.equal(body.has('ruleParams'), false);
});

test('bulkIDs 去重并跳过空值', () => {
    const m = load();
    const f = form({ 'data-write': 'bulk' }, []);
    f.querySelectorAll = () => [
        el({ name: 'ids', value: 'a', checked: true }),
        el({ name: 'ids', value: 'a', checked: true }),
        el({ name: 'ids', value: '  ', checked: true }),
        el({ name: 'ids', value: 'b', checked: true }),
        el({ name: 'other', value: 'c', checked: true }),   // 不叫 ids 的不该被收
        el({ name: 'ids', value: 'd', checked: false }),    // 未勾选的不该被收
    ];
    // 跨 vm realm 的数组原型不同，deepEqual 会误判 —— 比较拼接后的字符串。
    assert.equal(m.bulkIDs(f).join(','), 'a,b');
});

test('buildBody data-write-lines 把多行文本切成数组并丢空行', () => {
    const m = load();
    const f = form({ 'data-write': 'update', 'data-write-lines': 'images,imageAlts' }, [
        el({ name: 'images', value: '/a.jpg\n\n/b.jpg\n  ' }),
        el({ name: 'imageAlts', value: '图一' }),
    ]);
    const body = m.buildBody(f, null);
    assert.equal(body.getAll('images').join('|'), '/a.jpg|/b.jpg');
    assert.equal(body.getAll('imageAlts').join('|'), '图一');
});

test('buildBody data-write-lines 空文本按空数组发（清空是明确意图）', () => {
    const m = load();
    const f = form({ 'data-write': 'update', 'data-write-lines': 'images' }, [
        el({ name: 'images', value: '' }),
    ]);
    const body = m.buildBody(f, null);
    // 空串本身被 dropEmpty 删掉了 → 键不存在即「没改」。这与旧基座一致：
    // 空文本先被 dropEmpty 删掉，applyLinesFields 就看不到它了。
    assert.equal(body.has('images'), false);
});

test('buildBody data-write-json-wrap 把裸 HTML 包成 {html}', () => {
    const m = load();
    const f = form({ 'data-write': 'update', 'data-write-json-wrap': 'descriptionHtml:description' }, [
        el({ name: 'descriptionHtml', value: '<p>正文</p>' }),
    ]);
    const body = m.buildBody(f, null);
    assert.equal(body.has('descriptionHtml'), false);
    assert.deepEqual(JSON.parse(body.get('description')), { html: '<p>正文</p>' });
});

test('buildBody 表单级 data-write-keep-empty 保留空串（提升为顶级）', () => {
    const m = load();
    // 商品分类表单的真实形态：父级下拉选「（顶级分类）」时值是空串，
    // 后端语义「nil = 本次不改，空串 = 提升为顶级」。
    //
    // 曾经的真实缺陷：模板写的是**表单级** data-write-keep-empty，
    // 而基座只读控件级 data-keep-empty —— 属性写了从不生效，
    // 门禁认它合规，于是用户点「提升为顶级」什么都没改，界面却显示保存成功。
    const f = form({ 'data-write': 'update', 'data-write-keep-empty': 'parentId' }, [
        el({ name: 'parentId', value: '' }),
        el({ name: 'name', value: '子分类' }),
    ]);
    const body = m.buildBody(f, null);
    assert.equal(body.has('parentId'), true, '表单级 keep-empty 必须保留空串');
    assert.equal(body.get('parentId'), '');
    // 没点名的字段仍按「空 = 没填」删掉。
    assert.equal(body.has('name'), true);
});

test('buildBody 表单级 keep-empty 支持逗号分隔多个字段', () => {
    const m = load();
    const f = form({ 'data-write': 'update', 'data-write-keep-empty': 'parentId, primaryCategoryId' }, [
        el({ name: 'parentId', value: '' }),
        el({ name: 'primaryCategoryId', value: '' }),
        el({ name: 'sort', value: '' }),
    ]);
    const body = m.buildBody(f, null);
    assert.equal(body.has('parentId'), true);
    assert.equal(body.has('primaryCategoryId'), true);
    assert.equal(body.has('sort'), false, '未点名且为空的字段仍删掉');
});

test('buildBody 控件级与表单级 keep-empty 同时生效', () => {
    const m = load();
    const f = form({ 'data-write': 'update', 'data-write-keep-empty': 'parentId' }, [
        el({ name: 'parentId', value: '' }),
        el({ name: 'legacyId', value: '', keepEmpty: true }),
    ]);
    const body = m.buildBody(f, null);
    assert.equal(body.has('parentId'), true, '表单级');
    assert.equal(body.has('legacyId'), true, '控件级');
});
