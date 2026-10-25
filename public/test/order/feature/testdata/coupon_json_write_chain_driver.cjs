/* coupon_json_write_chain_driver.cjs — task-98 S1 的**链式证据**驱动脚本。
 *
 * 目的：证明「模板里声明的属性 → 真实基座 write-base.js → HTTP 请求体 → 真实 handler」
 * 这条链是通的 —— 静态核验（data-write-number 有没有写）挡不住 Sol 报的 P1：
 * bulk 按钮少一个 data-write-number=status，属性看着齐全、请求却是字符串 status，
 * 服务端整批 400。只有把**真实渲染出来的模板属性**喂给**真实基座**、再让请求真的打到
 * **真实 handler** 上，这条缺陷才会现形。
 *
 * 用法：node coupon_json_write_chain_driver.cjs <fixture.json>
 *   fixture = { uiDir, base, csrf, cases: [ { name, formAttrs, submitterAttrs,
 *               fields:[{name,value,checked,tag,type}], wantsRequest } ] }
 *   · uiDir: internal/templates/static/js/ui 的绝对路径（加载真 _util.js / write-base.js / confirm.js）
 *   · base:  真实 httptest 服务器地址（请求真打过去，不在本地桩掉 fetch）
 *   · fields: 按**渲染出来的**表单字段逐个构造（隐藏域 / 目标状态 select / 已勾选的 ids 复选框）
 * 输出：stdout 一行 JSON：{ results: [{ name, requests:[{url, body}], statusText, reloads, assigns }] }
 *
 * DOM 桩的手法抄自同目录的 json_write.test.cjs（node:test + vm + 极简 DOM，无浏览器、无依赖）：
 * 这里只保留本条链真正用到的入口，桩做到「够用即可」——桩越多，测的就越不是真东西。
 */
'use strict';

const fs = require('node:fs');
const vm = require('node:vm');

const fixture = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const uiDir = fixture.uiDir;

const utilSource = fs.readFileSync(uiDir + '/_util.js', 'utf8');
// 批次 2 之后写基座统一为 write-base.js（取代 json_write + htmx_form_body + write_feedback）。
const source = fs.readFileSync(uiDir + '/write-base.js', 'utf8');
const confirmSource = fs.readFileSync(uiDir + '/confirm.js', 'utf8');

function classesOf(el) {
    return String(el.attrs.class || el.className || '').split(/\s+/).filter(Boolean);
}

function matches(el, selector) {
    return String(selector).trim().split(/(?=\[|\.)/).every((part) => {
        if (!part) { return true; }
        if (part.startsWith('.')) { return classesOf(el).indexOf(part.slice(1)) >= 0; }
        if (part.startsWith('[')) {
            const m = part.match(/^\[([^\]=]+)(?:="([^"]*)")?\]$/);
            if (!m) { return false; }
            const [, name, want] = m;
            if (!el.hasAttribute(name)) { return false; }
            return want === undefined || String(el.getAttribute(name)) === want;
        }
        return el.tagName === part.toUpperCase();
    });
}

function descendants(el) {
    const out = [];
    (el.children || []).forEach((child) => { out.push(child, ...descendants(child)); });
    return out;
}

function makeEl(tag, attrs, props) {
    const el = {
        tagName: String(tag).toUpperCase(),
        attrs: Object.assign({}, attrs || {}),
        dataset: {},
        children: [],
        parentNode: null,
        listeners: {},
        hidden: false,
        disabled: false,
        checked: false,
        open: false,
        returnValue: '',
        type: (props && props.type) || '',
        getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null; },
        setAttribute(name, value) { this.attrs[name] = String(value); },
        removeAttribute(name) { delete this.attrs[name]; },
        hasAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attrs, name); },
        addEventListener(type, fn) { (this.listeners[type] = this.listeners[type] || []).push(fn); },
        removeEventListener(type, fn) { this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== fn); },
        appendChild(child) { this.children.push(child); child.parentNode = this; return child; },
        removeChild(child) { this.children = this.children.filter((c) => c !== child); child.parentNode = null; return child; },
        querySelectorAll(sel) { return descendants(this).filter((x) => matches(x, sel)); },
        querySelector(sel) { return this.querySelectorAll(sel)[0] || null; },
        closest(sel) { let node = this; while (node) { if (matches(node, sel)) { return node; } node = node.parentNode; } return null; },
        focus() {},
        click() { (this.listeners.click || []).forEach((fn) => fn({ preventDefault() {} })); },
        showModal() { this.open = true; },
        close() { this.open = false; (this.listeners.close || []).forEach((fn) => fn({})); },
    };
    Object.defineProperty(el, 'classList', {
        get() {
            const self = el;
            const write = (list) => { self.attrs.class = list.join(' '); self.className = self.attrs.class; };
            return {
                add(...names) { const l = classesOf(self); names.forEach((n) => { if (l.indexOf(n) < 0) { l.push(n); } }); write(l); },
                remove(...names) { write(classesOf(self).filter((n) => names.indexOf(n) < 0)); },
                contains(name) { return classesOf(self).indexOf(name) >= 0; },
                toggle(name, force) {
                    const has = classesOf(self).indexOf(name) >= 0;
                    const want = force === undefined ? !has : !!force;
                    if (want && !has) { this.add(name); } else if (!want && has) { this.remove(name); }
                    return want;
                },
            };
        },
        configurable: true,
    });
    // confirm.js 往 <dialog> 里写骨架 HTML（标签 + 属性），所以 innerHTML 只解析骨架。
    Object.defineProperty(el, 'innerHTML', {
        set(html) {
            const root = { children: [] };
            const stack = [root];
            const tagRe = /<(\/?)(\w+)([^>]*)>/g;
            let m;
            while ((m = tagRe.exec(String(html)))) {
                if (m[1]) { if (stack.length > 1) { stack.pop(); } continue; }
                const attrs = {};
                const attrRe = /([\w-]+)(?:="([^"]*)")?/g;
                let a;
                while ((a = attrRe.exec(m[3]))) { attrs[a[1]] = a[2] === undefined ? '' : a[2]; }
                const child = makeEl(m[2], attrs);
                child.parentNode = stack[stack.length - 1];
                stack[stack.length - 1].children.push(child);
                stack.push(child);
            }
            this.children = root.children;
        },
        get() { return ''; },
        configurable: true,
    });
    Object.defineProperty(el, 'textContent', {
        get() {
            if (this.children.length > 0) { return this.children.map((c) => c.textContent).join(''); }
            return this._text === undefined ? '' : this._text;
        },
        set(v) { this.children = []; this._text = String(v); },
        configurable: true,
    });
    Object.defineProperty(el, 'value', {
        get() { return el.attrs.value === undefined ? '' : el.attrs.value; },
        set(v) { el.attrs.value = String(v); },
        configurable: true,
    });
    return el;
}

// runCase：按一个用例的字段清单造表单，派发真实 submit 事件，跑完确认框协议后等请求落地。
async function runCase(spec) {
    let dialog = null;
    const meta = makeEl('meta', { name: 'csrf-token', content: fixture.csrf });
    const body = makeEl('body');
    const document = {
        listeners: {},
        readyState: 'complete',
        body,
        querySelector(sel) { return matches(meta, sel) ? meta : body.querySelector(sel); },
        querySelectorAll(sel) { return body.querySelectorAll(sel); },
        addEventListener(type, fn, capture) { (this.listeners[type] = this.listeners[type] || []).push({ fn, capture: !!capture }); },
        dispatch(type, event) {
            event.preventDefault = event.preventDefault || function () { this.prevented = true; };
            (this.listeners[type] || []).slice()
                .sort((a, b) => (a.capture === b.capture ? 0 : (a.capture ? -1 : 1)))
                .forEach((entry) => entry.fn(event));
            return event;
        },
        dispatchEvent() {},
        createElement(tag) { const el = makeEl(tag); if (tag === 'dialog') { dialog = el; } return el; },
    };

    const form = makeEl('form', spec.formAttrs || {});
    const status = makeEl('p', { 'data-write-status': '', role: 'status', hidden: '' });
    form.appendChild(status);
    (spec.fields || []).forEach((f) => {
        const el = makeEl(f.tag || 'input', { name: f.name }, { type: f.type || 'text' });
        el.value = f.value === undefined ? '' : f.value;
        el.checked = !!f.checked;
        form.appendChild(el);
    });
    const submit = makeEl('button', Object.assign({ type: 'submit' }, spec.submitterAttrs || {}));
    form.appendChild(submit);
    body.appendChild(form);
    // 真实 DOM 的按钮反向引用：表单内的按钮自带 `form`；**表单外的按钮靠 form 属性指回所属表单**
    // （浏览器就是这么解析的，confirm.js 的 activateConfirmed 也依赖它去找「提交哪张表」）。
    // 模板里行内启停/删除的按钮正是「在表单外 + form="coupon-toggle-<id>"」这种形态。
    descendants(body).forEach((el) => {
        if (el.tagName !== 'BUTTON') { return; }
        const ownerID = el.getAttribute('form');
        if (!ownerID) { el.form = form; return; }
        el.form = descendants(body).filter((x) => x.getAttribute('id') === ownerID)[0] || null;
    });

    const requests = [];
    // done[i] 在**响应读完之后**才置真：只等 requests.length 会在请求刚发出、
    // 网络还没往返时就往下走，随后 process.exit 把连接掐掉 —— 服务端一个字节都收不到，
    // 而这边的 payload 断言照样"看见"了请求（实测踩过：真实链路测试因此假绿）。
    const done = [];
    // fetch 失败必须上报：只报「请求发出去了」会让服务端没收到时难以定位
    // （真把 body 送到 router 才算链路通，客户端一厢情愿不算）。
    const fetchErrors = [];
    const calls = { assign: [], reload: 0 };
    // 真 fetch 必须在这里先抓住：sandbox 里的同名方法一旦直接用 `fetch(...)`，
    // 标识符会解析到 sandbox 自己 → 无限递归（这坑实测踩过一次）。
    const realFetch = globalThis.fetch;
    const sandbox = {
        console,
        document,
        setTimeout,
        clearTimeout,
        // 真打 HTTP：chain 的价值就在「请求真的到了 router（中间件 → handler）」，
        // 在本地桩掉 fetch 只证明「属性读出来了」。
        async fetch(url, options) {
            const target = new URL(url, fixture.base).toString();
            let body2 = options && options.body;
            try { body2 = JSON.parse(body2); } catch (e) { /* 非 JSON body 原样上报 */ }
            requests.push({ url: target, body: body2, headers: (options && options.headers) || {} });
            const slot = done.push(false) - 1;
            const entry = requests[slot];
            try {
                const res = await realFetch(target, {
                    method: (options && options.method) || 'POST',
                    headers: (options && options.headers) || {},
                    body: options && options.body,
                });
                const text = await res.text();
                entry.resStatus = res.status;
                return { ok: res.ok, status: res.status, text: () => Promise.resolve(text) };
            } catch (err) {
                fetchErrors.push(String((err && err.message) || err));
                throw err;
            } finally {
                done[slot] = true;
            }
        },
        sessionStorage: { getItem: () => null },
    };
    // 新基座用 FormData 组装请求体（formdata 传输），sandbox 必须提供它。
    // Node 18+ 自带 FormData，直接借过来即可（它是实现的真相，不是桩）。
    sandbox.FormData = FormData;
    sandbox.CustomEvent = class { constructor(t) { this.type = t; } };
    sandbox.CSS = { escape: (x) => String(x) };
    sandbox.window = sandbox;
    sandbox.global = sandbox;
    sandbox.location = {
        reload() { calls.reload += 1; },
        assign(url) { calls.assign.push(url); },
    };
    vm.runInNewContext(utilSource, sandbox);
    vm.runInNewContext(confirmSource, sandbox);
    vm.runInNewContext(source, sandbox);

    // requestSubmit() 不带参数时，浏览器用的是表单的**默认提交按钮**（树序第一个 submit 按钮）——
    // confirm.js 的表单级确认走的就是这条（submitConfirmed 不传按钮），必须照实现，否则
    // 表单级 data-confirm 的场景会退化成「没有 submitter」，按钮级声明全被丢掉。
    form.requestSubmit = function (el) {
        let submitter = el || null;
        if (!submitter) {
            submitter = descendants(form).filter((x) => x.tagName === 'BUTTON' && x.type === 'submit')[0] || null;
        }
        return document.dispatch('submit', { target: form, submitter: submitter, preventDefault() { this.prevented = true; } });
    };
    form.submit = function () { form.nativeSubmitted = true; };

    // fire 走**真实用户路径**：点按钮 → confirm.js 的 click 委托（按钮级 data-confirm 只在这里拦）→
    // 确认后 activateConfirmed → form.requestSubmit(被点的按钮) → submit 事件 → json_write 发请求。
    // 早先版本直接派发 submit，于是：json_write 看到按钮上还有 data-confirm 就「让给确认框」，
    // 而 confirm.js 的 click 委托根本没跑 —— 两边都放行、请求一个也不发（实测就是这个现象）。
    const fire = () => {
        const ev = { target: submit, preventDefault() { this.prevented = true; } };
        document.dispatch('click', ev);
        // 没有 data-confirm 的按钮不会被 click 委托接管：此时按真实浏览器行为走表单提交。
        if (!requests.length && dialog === null) {
            document.dispatch('submit', { target: form, submitter: submit, preventDefault() { this.prevented = true; } });
        }
        return ev;
    };

    // TEMP DEBUG
    try {
        const WB = sandbox.window.WBUI && sandbox.window.WBUI.writeBase;
        console.error('DEBUG forms attrs=', JSON.stringify(form.attrs));
        console.error('DEBUG hasWB=', !!WB, 'bulkIDs=', WB ? JSON.stringify(WB.bulkIDs(form)) : 'n/a');
        const qsa = form.querySelectorAll('[name]');
        console.error('DEBUG qsa len=', qsa.length, 'names=', qsa.map((x)=>x.getAttribute('name')+':'+x.checked).join(','));
    } catch (e) { console.error('DEBUG err', e.message); }
    fire();
    // 带 data-confirm 的按钮会被 confirm.js 拦下：不弹框、先不发请求。
    // 这里模拟用户点「确认」（<dialog>.returnValue='ok' + close），再由 confirm.js 重派发。
    if (!requests.length && dialog && dialog.open) {
        dialog.returnValue = 'ok';
        dialog.close();
    }
    // 等真实网络往返结束（最多 2s）—— 不猜时长，按请求落地收紧循环。
    const deadline = Date.now() + 2000;
    while ((!requests.length || done.some((d) => !d)) && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 20));
    }
    await new Promise((r) => setTimeout(r, 120));

    return {
        name: spec.name,
        fetchErrors,
        requests,
        statusText: String(status.textContent || ''),
        rowRemoved: !!(form.querySelector('[name="ids"]') || {}).parentNode,
        reloads: calls.reload,
        assigns: calls.assign,
    };
}

(async () => {
    const results = [];
    for (const spec of fixture.cases) {
        results.push(await runCase(spec));
    }
    process.stdout.write(JSON.stringify({ results }));
    // 真实 HTTP 打了连接池，事件循环会一直有活跃句柄；跑完就退，别让调用方（Go 的 exec）等超时。
    process.exit(0);
})().catch((err) => {
    process.stdout.write(JSON.stringify({ error: String((err && err.stack) || err) }));
    process.exitCode = 1;
});
