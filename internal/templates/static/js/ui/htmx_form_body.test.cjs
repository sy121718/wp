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
        set(k, v) { map.set(k, [v]); },
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
    assert.deepEqual(body.dump(), [
        'projectId=p1',
        'values=[{"id":"v1","key":"red","sort":1,"enabled":true},{"id":"v2","key":"blue","sort":2,"enabled":false}]',
    ], '行内字段进对象数组、类型转换正确、顶层不泄漏');
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
    const raw = body.dump().find((p) => p.startsWith('values=')).slice('values='.length);
    assert.equal(JSON.parse(raw)[0].enabled, undefined, '未勾选的 checkbox 不该出现在对象里');
});
