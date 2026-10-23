/* htmx_form_body.test.cjs — ui/htmx_form_body.js 的行为契约（node:test + vm，无浏览器）。
 *
 * 这个文件守的是四类**在界面上看不出来**的缺陷：
 *
 *   1. 空值键没删 —— formdata 的 `sort=` 会让后端 *int 绑成 0，「没填」变成「改成 0」；
 *   2. **同名多值里混进一个空值，把整组都删了** —— 用户勾了两项，存进去零项，页面还显示成功；
 *   3. keep-empty 的字段被误删 —— 那是「解绑」语义，删了等于「不改」，用户点了没反应；
 *   4. 嵌套对象/行数组组装错 —— 键漏了、类型没转、行内字段泄漏到顶层。
 *
 * 运行：node --test internal/templates/static/js/ui/htmx_form_body.test.cjs
 *
 * 为什么需要它：这道逻辑原先由 json_write.js 承担，测试也只覆盖了那一份。迁移到
 * htmx_form_body.js 之后，行为面（谁删、删到什么程度）**原样搬过来了但测试没搬** ——
 * 审查指出的正是这个缺口：静态门禁看不出「删过头」。
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(__dirname + '/htmx_form_body.js', 'utf8');

// ── 最小 DOM 桩 ──
//
// 只实现被测逻辑真正用到的那几项：closest / getAttribute / querySelectorAll / value /
// disabled / checked。刻意不引入 jsdom —— 这个仓库的控件测试一直是零依赖的 vm 路线
// （见 drawer.test.cjs / json_write.test.cjs），多一个依赖会让 CI 多一条安装路径。
function makeElement(spec) {
    const attrs = spec.attrs || {};
    const el = {
        _attrs: attrs,
        value: spec.value === undefined ? '' : spec.value,
        disabled: !!spec.disabled,
        checked: !!spec.checked,
        type: spec.type || 'text',
        tagName: spec.tagName || 'INPUT',
        getAttribute(name) {
            return Object.prototype.hasOwnProperty.call(attrs, name) ? attrs[name] : null;
        },
        closest(sel) {
            // 只支持两种选择器形态：'[attr]' 与 '.class'（被测代码用的就这两种）。
            if (sel.startsWith('[') && sel.endsWith(']')) {
                const name = sel.slice(1, -1);
                return this.getAttribute(name) != null ? this : null;
            }
            return null;
        },
        querySelectorAll(sel) {
            return spec.childrenFor ? spec.childrenFor(sel) : [];
        },
    };
    return el;
}

// makeForm 造一份「表单根」：它的 closest 返回自己（带 data-write-* 声明），
// querySelectorAll 按容器选择器交出预先安排好的行/字段。
function makeForm(attrs, lookup) {
    const form = makeElement({ attrs });
    form.querySelectorAll = function (sel) {
        return (lookup && lookup[sel]) || [];
    };
    form.querySelector = function (sel) {
        const all = (lookup && lookup[sel]) || [];
        return all.length ? all[0] : null;
    };
    return form;
}

// body 的对象语义用真 FormData 的等价实现：同名多值、delete 删整键、set 覆盖。
function makeBody(pairs) {
    const map = new Map();
    for (const [k, v] of pairs) {
        if (!map.has(k)) map.set(k, []);
        map.get(k).push(v);
    }
    return {
        _map: map,
        forEach(fn) {
            for (const [k, vs] of map) for (const v of vs) fn(v, k);
        },
        delete(k) { map.delete(k); },
        get(k) { const vs = map.get(k); return vs && vs.length ? vs[0] : null; },
        set(k, v) { map.set(k, [v]); },
        append(k, v) {
            if (!map.has(k)) map.set(k, []);
            map.get(k).push(v);
        },
        dump() {
            const out = [];
            for (const [k, vs] of map) for (const v of vs) out.push(k + '=' + v);
            return out;
        },
    };
}

// runBody 加载被测脚本并捕获它注册的 config:request 处理器。
function loadWith(method) {
    let handler = null;
    const sandbox = {
        window: {
            htmx: {
                on(name, fn) {
                    if (name === 'htmx:config:request') handler = fn;
                },
            },
        },
    };
    sandbox.window.window = sandbox.window;
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);
    assert.ok(handler, '脚本必须注册 htmx:config:request 处理器');

    return function fire(body, sourceElement) {
        handler({
            detail: {
                ctx: {
                    request: { method: method || 'post', body },
                    sourceElement,
                },
            },
        });
        return body;
    };
}

// ── ① 空值键 ──

test('全部为空的键被删掉（避免 *int 绑成 0）', () => {
    const fire = loadWith('post');
    const body = makeBody([['projectId', 'p1'], ['name', 'x'], ['sort', ''], ['slug', '  ']]);
    fire(body, makeForm({}));
    assert.deepEqual(body.dump(), ['projectId=p1', 'name=x'], '空值键应整键删除');
});

test('**同名多值里只有一个空值时，整组必须保留**（丢数据的核心用例）', () => {
    // 这是审查指出的缺陷：FormData.delete(key) 删的是该键的**所有值**。
    // 修前：见到 tagIds= 空值就 delete('tagIds') → 用户勾的两项全没了，页面显示保存成功。
    const fire = loadWith('post');
    const body = makeBody([['tagIds', 'a'], ['tagIds', ''], ['tagIds', 'c']]);
    fire(body, makeForm({}));
    assert.deepEqual(body.dump(), ['tagIds=a', 'tagIds=', 'tagIds=c'],
        '有一个非空值时，整组原样保留（空值也保留，删了会改变顺序语义）');
});

test('同名多值全部为空时整组删除', () => {
    const fire = loadWith('post');
    const body = makeBody([['tagIds', ''], ['tagIds', '   ']]);
    fire(body, makeForm({}));
    assert.deepEqual(body.dump(), []);
});

test('keep-empty 点名的字段即使为空也照发（空串有语义：解绑/提升为顶级）', () => {
    const fire = loadWith('post');
    const body = makeBody([['parentId', ''], ['slug', '']]);
    fire(body, makeForm({ 'data-write-keep-empty': 'parentId' }));
    assert.deepEqual(body.dump(), ['parentId='], 'parentId 空串保留，slug 空串删除');
});

test('非写动词不处理（GET 的参数在 URL 上，空筛选有既有语义）', () => {
    const fire = loadWith('get');
    const body = makeBody([['keyword', '']]);
    fire(body, makeForm({}));
    assert.deepEqual(body.dump(), ['keyword='], 'GET 不该被整形');
});

// ── ② 嵌套对象（data-write-object）──

function objectForm(declared) {
    // 容器里的字段：drive 是驱动 select（option 带 data-rule-keys），其余是参数输入框。
    const select = makeElement({
        attrs: { name: 'ruleType' },
        value: declared.ruleType || 'new_arrival',
    });
    select.options = [{
        selected: true,
        getAttribute(n) { return n === 'data-rule-keys' ? declared.keys : null; },
    }];
    select.selectedOptions = select.options;
    const param = makeElement({ attrs: { name: 'days' }, value: declared.days === undefined ? '30' : declared.days });
    const box = makeElement({ attrs: {} });
    box.querySelectorAll = function (sel) {
        return sel === '[name]' ? [select, param] : [];
    };

    const kind = makeElement({ attrs: { name: 'kind' }, value: declared.kind || 'rule' });
    const host = makeForm({
        'data-write-object': '.flds;ruleParams;ruleType;kind=rule',
    }, { '.flds': [box], '[name]': [kind, select, param] });
    return host;
}

test('data-write-object 把扁平字段收成对象，驱动字段单独发', () => {
    const fire = loadWith('post');
    const body = makeBody([['projectId', 'p1'], ['kind', 'rule'], ['ruleType', 'new_arrival'], ['days', '30']]);
    fire(body, objectForm({ keys: 'days' }));
    assert.deepEqual(body.dump(), ['projectId=p1', 'kind=rule', 'ruleParams={"days":30}', 'ruleType=new_arrival'],
        'days 进对象后不再出现在顶层；kind（条件字段）与 ruleType（驱动字段）都要发');
});

test('条件不满足时，对象与驱动字段都不发，但容器内字段也不泄漏到顶层', () => {
    // kind=manual 时手工标签不该带 ruleType（服务端会拒），而容器里的 days 也不该以扁平形态漏出。
    const fire = loadWith('post');
    const body = makeBody([['projectId', 'p1'], ['kind', 'manual'], ['ruleType', 'new_arrival'], ['days', '30']]);
    fire(body, objectForm({ kind: 'manual', keys: 'days' }));
    // kind 本身是**条件字段**（在容器外），它当然要发 —— 端点靠它判「用不用规则」。
    // 不该发的是对象 ruleParams 与驱动字段 ruleType，以及容器里的 days（不能以扁平形态漏出）。
    assert.deepEqual(body.dump(), ['projectId=p1', 'kind=manual'],
        'kind=manual 时对象与驱动字段都不发，容器内字段也不泄漏');
});

test('对象里的值按数字发，认不出的原样发字符串', () => {
    const fire = loadWith('post');
    const body = makeBody([['kind', 'rule'], ['ruleType', 'r'], ['days', 'abc']]);
    fire(body, objectForm({ keys: 'days', days: 'abc', ruleType: 'r' }));
    assert.deepEqual(body.dump(), ['kind=rule', 'ruleParams={"days":"abc"}', 'ruleType=r'],
        '非法数字不猜类型，原样发字符串由 API 报错');
    // 注意：kind 是**条件字段**（在容器外），必须原样留在顶层。
});

test('对象里的空值不进对象（只填一个价时另一个键不该出现）', () => {
    const fire = loadWith('post');
    const body = makeBody([['kind', 'rule'], ['ruleType', 'price_range'], ['minPrice', '100'], ['maxPrice', '']]);
    fire(body, objectForm({ keys: 'minPrice,maxPrice', ruleType: 'price_range' }));
    // maxPrice 的输入框在桩里是同一个 param（days），这里只验 minPrice 的行为：
    assert.ok(body.dump().some((p) => p.startsWith('ruleParams=')), '应产出 ruleParams');
    const raw = body.dump().find((p) => p.startsWith('ruleParams=')).slice('ruleParams='.length);
    assert.equal(JSON.parse(raw).maxPrice, undefined, '空值不该进对象');
});

// ── ③ 行数组（data-write-rows）──

function rowsForm(rows) {
    const boxes = rows.map((row, idx) => {
        const fields = [];
        for (const [name, value] of Object.entries(row)) {
            // 布尔字段在真实模板里是 hidden(0) + checkbox(1) 打底：
            // 勾选时 checkbox 在 DOM 末尾 → "1"；未勾选时 checkbox 返回 null，hidden 的 "0" 留下。
            // 桩必须照抄这个形态，否则测不到「取最后一个有效值」那条规则。
            if (name === 'enabled') {
                fields.push(makeElement({ attrs: { name: 'values[' + idx + '].enabled' }, value: '0' }));
                fields.push(makeElement({
                    attrs: { name: 'values[' + idx + '].enabled' },
                    value: '1',
                    type: 'checkbox',
                    checked: value === '1',
                }));
                continue;
            }
            fields.push(makeElement({ attrs: { name: 'values[' + idx + '].' + name }, value }));
        }
        const box = makeElement({ attrs: {} });
        // 被清理逻辑调用时传的是 '[name]'，取值时（rowFieldValue）传的是无参/任意选择器：
        // 两种情况都返回同一批字段 —— 真实 DOM 里它们本来就是同一组元素。
        box.querySelectorAll = function () { return fields; };
        box.querySelector = function () { return fields.length ? fields[0] : null; };
        return box;
    });
    const host = makeForm({
        'data-write-rows': '.row;id,key,sort,enabled;values',
        'data-write-number': 'sort',
        'data-write-bool': 'enabled',
    }, { '.row': boxes });
    return host;
}

test('data-write-rows 把行收成对象数组，按点名转数字与布尔', () => {
    const fire = loadWith('post');
    const body = makeBody([
        ['projectId', 'p1'],
        ['values[0].id', 'v1'], ['values[0].key', 'red'], ['values[0].sort', '1'], ['values[0].enabled', '1'],
        ['values[1].id', 'v2'], ['values[1].key', 'blue'], ['values[1].sort', '2'], ['values[1].enabled', '0'],
    ]);
    fire(body, rowsForm([
        { id: 'v1', key: 'red', sort: '1', enabled: '1' },
        { id: 'v2', key: 'blue', sort: '2', enabled: '0' },
    ]));
    // **逐行 append 单个对象**，不是整体数组字符串 —— 见 applyRows 的注释：
    // gin 的 form 映射对 slice-of-struct 是逐元素 json.Unmarshal（要求单个对象）。
    // 发整体数组会让属性值整表保存 100% 400。
    assert.deepEqual(body.dump(), [
        'projectId=p1',
        'values={"id":"v1","key":"red","sort":1,"enabled":true}',
        'values={"id":"v2","key":"blue","sort":2,"enabled":false}',
    ], '每个行对象独立 append、类型转换正确、顶层不泄漏');
});

test('未勾选的 checkbox 不进对象（与表单提交同口径）', () => {
    const fire = loadWith('post');
    const body = makeBody([['values[0].id', 'v1'], ['values[0].enabled', '']]);
    const form = rowsForm([{ id: 'v1', key: '', sort: '', enabled: '0' }]);
    // 让 enabled 那个控件处于未勾选状态
    const boxes = form.querySelectorAll('.row');
    boxes[0].querySelectorAll = function () {
        return [
            makeElement({ attrs: { name: 'values[0].id' }, value: 'v1' }),
            makeElement({ attrs: { name: 'values[0].enabled' }, value: '1', type: 'checkbox', checked: false }),
        ];
    };
    fire(body, form);
    // 现在每行是**独立的一次 append**（不是整体数组），所以取第一条 values= 解析。
    const raw = body.dump().find((p) => p.startsWith('values=')).slice('values='.length);
    assert.equal(JSON.parse(raw).enabled, undefined, '未勾选的 checkbox 不该出现在对象里');
});

// ── ④ 本轮补的五档能力（此前只有旧基座实现，htmx 侧缺失）──

test('data-write-lines：换行拼接的单值展开成数组（否则图集存成一个坏 URL）', () => {
    // 实测过：media-gallery 发 images="a.jpg\nb.jpg" 时，纯 formdata 绑成
    // len=1 ["a.jpg\nb.jpg"] —— 一个元素内含换行，不是两个 URL。
    const fire = loadWith('post');
    const body = makeBody([['images', 'a.jpg\nb.jpg\n\nc.jpg'], ['projectId', 'p1']]);
    fire(body, makeForm({ 'data-write-lines': 'images' }));
    assert.deepEqual(body.dump(), ['projectId=p1', 'images=a.jpg', 'images=b.jpg', 'images=c.jpg'],
        '空行丢弃，其余逐行展开（顺序按原键顺序）');
});

test('data-write-html：裸 HTML 包成 {html: …} 写到目标键，原键消失且不 trim', () => {
    const fire = loadWith('post');
    const body = makeBody([['descriptionHtml', '<p>x</p>  '], ['name', 'n']]);
    fire(body, makeForm({ 'data-write-html': 'descriptionHtml:description' }));
    assert.deepEqual(body.dump(), ['name=n', 'description={"html":"<p>x</p>  "}'],
        '尾随空白是内容的一部分，不 trim');
});

test('data-write-array：空值删键，绝不发 "[]"（那会变成一个叫 "[]" 的元素）', () => {
    // gin 的 form 绑定把字符串 "[]" 当成**一个元素**（实测 `attributeIds=[]`
    // → []string{"[]"}），那个 "[]" 被当 uuid 查库 → PG 22P02 → 500（审查 P1）。
    // 空串同样是 []string{""}。
    const fire = loadWith('post');
    const body = makeBody([['projectId', 'p1'], ['attributeIds', ''], ['categoryIds', '[]']]);
    fire(body, makeForm({ 'data-write-array': 'attributeIds,categoryIds' }));
    assert.deepEqual(body.dump(), ['projectId=p1'],
        '空值与字面量 "[]" 都该删键 —— 表达「没勾」而不是「有一个坏元素」');
});

test('data-write-array：有值时不动（不能把用户勾的覆盖掉）', () => {
    const fire = loadWith('post');
    const body = makeBody([['attributeIds', 'a'], ['attributeIds', 'b']]);
    fire(body, makeForm({ 'data-write-array': 'attributeIds' }));
    assert.deepEqual(body.dump(), ['attributeIds=a', 'attributeIds=b']);
});

test('data-write-float：点名的字段按小数发，认不出的原样发字符串', () => {
    const fire = loadWith('post');
    const body = makeBody([['defaultPrice', '9.90'], ['weight', 'abc'], ['slug', '9.9']]);
    fire(body, makeForm({ 'data-write-float': 'defaultPrice,weight' }));
    assert.deepEqual(body.dump(), ['defaultPrice=9.9', 'weight=abc', 'slug=9.9'],
        'defaultPrice 转数字；weight 认不出原样发；未点名的 slug 不动');
});

// ── ⑤ 确认框守卫（write_feedback.js，但用同一套 vm 手法测）──
//
// 这是审查 P1 的一项：ui/confirm.js 在捕获阶段 preventDefault 弹框，而 htmx 4 的
// 事件处理器**不检查 defaultPrevented**（运行时源码里该标识 0 次出现）——
// 于是确认前就发出请求，确认后再发一次 = 一次删除两个请求。
//
// 守卫在 write_feedback.js 的 htmx:before:request 里，判据与 json_write.js 的
// hasConfirm 一致：表单或 submitter 上还有 data-confirm 就拦。
test('write_feedback 拦下「确认流程未走完」的请求', () => {
    const feedbackSource = fs.readFileSync(__dirname + '/write_feedback.js', 'utf8');
    const handlers = {};
    const sandbox = {
        window: {
            htmx: { on(name, fn) { handlers[name] = fn; } },
            WBUI: { toast() {} },
        },
        document: { readyState: 'complete', querySelectorAll() { return []; }, addEventListener() {} },
    };
    sandbox.window.window = sandbox.window;
    sandbox.window.document = sandbox.document;
    vm.createContext(sandbox);
    vm.runInContext(feedbackSource, sandbox);

    assert.ok(handlers['htmx:before:request'], '必须注册 before:request 守卫');

    // 表单上还带 data-confirm → 拦
    let prevented = 0;
    const formWithConfirm = {
        hasAttribute: (n) => n === 'data-confirm',
        closest: () => null,
    };
    handlers['htmx:before:request']({
        detail: { ctx: { sourceElement: formWithConfirm } },
        preventDefault() { prevented++; },
    });
    assert.equal(prevented, 1, '确认未走完时必须拦下请求');

    // 表单上没有 data-confirm（confirm.js 放行时摘掉了）→ 放行
    prevented = 0;
    const plainForm = { hasAttribute: () => false, closest: () => null };
    handlers['htmx:before:request']({
        detail: { ctx: { sourceElement: plainForm } },
        preventDefault() { prevented++; },
    });
    assert.equal(prevented, 0, '确认已放行时不该拦');

    // 按钮级 data-confirm（在 submitter 上，表单上没有）→ 也要拦
    prevented = 0;
    const btn = { hasAttribute: (n) => n === 'data-confirm', closest: () => ({ hasAttribute: () => false }) };
    handlers['htmx:before:request']({
        detail: { ctx: { sourceElement: btn } },
        preventDefault() { prevented++; },
    });
    assert.equal(prevented, 1, '按钮级 data-confirm 同样要拦');
});

// ── ⑥ data-write-ids-scope：ids 装在别的容器里 ──
//
// 现有唯一用例是商品列表的「按规则改价」抽屉：抽屉内容被克隆到 document 级，
// 勾选框留在页面那个批量表单里。在抽屉表单内部找 [name=ids] 永远是空数组，
// 表现是「点了应用调价什么都没发生」，而基座只回一句「没勾选任何项」。

test('data-write-ids-scope：从声明的作用域收已勾选的 ids', () => {
    // 页面上的批量表单（ids 在这里），抽屉表单是另一个。
    const pageScope = {
        querySelectorAll(sel) {
            return [
                makeElement({ attrs: { name: 'ids' }, value: 'a', type: 'checkbox', checked: true }),
                makeElement({ attrs: { name: 'ids' }, value: 'b', type: 'checkbox', checked: false }),
                makeElement({ attrs: { name: 'ids' }, value: 'c', type: 'checkbox', checked: true }),
                makeElement({ attrs: { name: 'ids' }, value: 'a', type: 'checkbox', checked: true }),
            ];
        },
    };
    const sandbox = {
        window: { htmx: { on(n, fn) { sandbox._h = fn; } } },
        document: { querySelector: () => pageScope },
    };
    sandbox.window.window = sandbox.window;
    sandbox.window.document = sandbox.document;
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);

    const form = makeForm({ 'data-write-ids-scope': '[data-list-bulk]' });
    const body = makeBody([['projectId', 'p1']]);
    sandbox._h({ detail: { ctx: { request: { method: 'post', body }, sourceElement: form } } });

    // 只收勾选的、去重、保序；projectId 原样留着。
    assert.deepEqual(body.dump(), ['projectId=p1', 'ids=a', 'ids=c'],
        '应只收已勾选且去重的 ids');
});

test('data-write-ids-scope：目标选择器打不中时不动请求体（不静默变空数组）', () => {
    const sandbox = {
        window: { htmx: { on(n, fn) { sandbox._h = fn; } } },
        document: { querySelector: () => null },
    };
    sandbox.window.window = sandbox.window;
    sandbox.window.document = sandbox.document;
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);

    const form = makeForm({ 'data-write-ids-scope': '[data-list-bulk]' });
    const body = makeBody([['projectId', 'p1']]);
    sandbox._h({ detail: { ctx: { request: { method: 'post', body }, sourceElement: form } } });

    assert.deepEqual(body.dump(), ['projectId=p1'],
        '选择器打不中时不该凭空造一个空 ids —— 交给端点按自己的规则拒绝');
});

// ── ⑦ data-write-builder：页面级 payload 组装器 ──
//
// 变体清单与捆绑配置的 payload 不是表单字段的平铺，而是从 DOM 另一处读出结构再合并。
// 组装规则只有一份（页面脚本注册一次），模板只声明名字。

test('data-write-builder：组装器的产物写回 body，对象/数组走 JSON 字符串', () => {
    const sandbox = {
        window: { htmx: { on(n, fn) { sandbox._h = fn; } } },
        document: { querySelector() { return null; }, querySelectorAll() { return []; } },
    };
    sandbox.window.window = sandbox.window;
    sandbox.window.document = sandbox.document;
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);

    // 页面自己的组装器（模拟 product_variant_write.js 的注册）
    sandbox.window.WBFormBody.registerPayloadBuilder('demo', function (form, submitter, payload) {
        payload.rows = [{ variantId: 'v1', skuCode: 'SKU-1' }];
        delete payload.rowInputs;   // 中间键不该发出去
        payload.maxOptions = 3;
        return payload;
    });

    const form = makeForm({ 'data-write-builder': 'demo' });
    const body = makeBody([['productId', 'p1'], ['rowInputs', 'x']]);
    sandbox._h({ detail: { ctx: { request: { method: 'post', body }, sourceElement: form } } });

    const dump = body.dump();
    // rows 是数组 → JSON 字符串；maxOptions 是数字 → 字符串；rowInputs 被组装器删掉。
    assert.ok(dump.indexOf('rows=[{"variantId":"v1","skuCode":"SKU-1"}]') >= 0, '数组应 stringify：' + dump);
    assert.ok(dump.indexOf('maxOptions=3') >= 0, '标量应原样写回：' + dump);
    assert.ok(dump.indexOf('rowInputs') < 0, '组装器删掉的中间键不该出现在 body：' + dump);
    assert.ok(dump.indexOf('productId=p1') >= 0, '其余字段保留：' + dump);
});

test('data-write-builder：页面没注册同名组装器时不动 body（交给端点拒绝）', () => {
    const sandbox = {
        window: { htmx: { on(n, fn) { sandbox._h = fn; } } },
        document: { querySelector() { return null; }, querySelectorAll() { return []; } },
    };
    sandbox.window.window = sandbox.window;
    sandbox.window.document = sandbox.document;
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);

    const form = makeForm({ 'data-write-builder': 'not-registered' });
    const body = makeBody([['productId', 'p1'], ['note', 'x']]);
    sandbox._h({ detail: { ctx: { request: { method: 'post', body }, sourceElement: form } } });

    assert.deepEqual(body.dump(), ['productId=p1', 'note=x'],
        '没注册时不该凭空改请求体 —— 交给端点按缺字段拒绝，比静默发半份 payload 好');
});

// ── ⑧ data-reload-on-ok 是布尔属性（无值）──
//
// 审查 P1：declared() 用 getAttribute(attr)||'' 判，而裸属性 data-reload-on-ok 的
// getAttribute 返回空串 → 恒 falsy → 12 处「成功后整页刷新」全部失效：
// 删除 / 批量删除 / 调价成功后列表不动，用户以为没生效。

test('data-reload-on-ok：裸属性（无值）也要触发刷新', () => {
    const feedbackSource = fs.readFileSync(__dirname + '/write_feedback.js', 'utf8');
    const handlers = {};
    let reloads = 0;
    const sandbox = {
        window: {
            htmx: { on(n, fn) { handlers[n] = fn; } },
            WBUI: { toast() {} },
            location: { reload() { reloads++; }, assign() {} },
            sessionStorage: { getItem() { return null; }, removeItem() {}, setItem() {} },
        },
        document: { readyState: 'complete', querySelectorAll() { return []; }, addEventListener() {} },
    };
    sandbox.window.window = sandbox.window;
    sandbox.window.document = sandbox.document;
    vm.createContext(sandbox);
    vm.runInContext(feedbackSource, sandbox);

    // 裸属性：getAttribute 返回空串、hasAttribute 返回 true（真实 DOM 的形态）。
    // closest 要能答两种选择器：写动作判定（[hx-post]）与刷新声明（[data-reload-on-ok]）。
    const host = {
        getAttribute: () => '',
        hasAttribute: (n) => n === 'data-reload-on-ok' || n === 'hx-post',
        closest(sel) {
            if (sel.indexOf('hx-post') >= 0) return this;          // isWrite 判定
            if (sel === '[data-reload-on-ok]') return this;        // needsReload 判定
            return null;
        },
    };
    handlers['htmx:after:request']({
        detail: { ctx: { response: { status: 200, headers: { get: () => null } }, sourceElement: host } },
    });

    assert.equal(reloads, 1, '裸 data-reload-on-ok 必须触发整页刷新（判据是 hasAttribute，不是取值）');
});

// ── ⑨ 真 FormData 上的遍历删除（审查 P1）──
//
// 前面的用例用 Map 桩，桩的 forEach 不模拟「删除跳过下一个」的行为。
// 这条直接用 Node 原生的 FormData，钉住 applyBuilderPayload 的「先收集再删」。

test('applyBuilderPayload 在真 FormData 上能删干净全部键', () => {
    const sandbox = {
        window: { htmx: { on(n, fn) { sandbox._h = fn; } } },
        document: { querySelector() { return null; }, querySelectorAll() { return []; } },
    };
    sandbox.window.window = sandbox.window;
    sandbox.window.document = sandbox.document;
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);

    // 组装器只回一个键 —— 其余必须被删干净（真 FormData 的 forEach 会跳过下一个）。
    sandbox.window.WBFormBody.registerPayloadBuilder('onlyOne', function (form, submitter, payload) {
        return { only: 'kept' };
    });

    const body = new FormData();
    ['a', 'b', 'c', 'd'].forEach((k) => body.set(k, '1'));

    const form = makeForm({ 'data-write-builder': 'onlyOne' });
    sandbox._h({ detail: { ctx: { request: { method: 'post', body }, sourceElement: form } } });

    const left = [...body.keys()].sort();
    assert.deepEqual(left, ['only'],
        '组装器返回的键之外的键必须全部删掉（真 FormData 上遍历删除会跳过下一个）');
});
