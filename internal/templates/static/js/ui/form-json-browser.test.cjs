const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const puppeteer = require('/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core');

// 使用真实 HTMX、表单与网络，避免 DOM 桩掩盖 disabled、submitter 和数组形状问题。
test('form-json v4 与本地 HTMX 的请求编码契约', async t => {
    const server = http.createServer((req, res) => {
        req.resume();
        res.setHeader('Content-Type', req.url === '/' ? 'text/html' : 'application/json');
        res.end(req.url === '/' ? '<html><body></body></html>' : '{"code":200,"message":"ok","data":{}}');
    });
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    const origin = `http://127.0.0.1:${server.address().port}`;
    const browser = await puppeteer.launch({ executablePath: '/usr/bin/google-chrome', headless: true,
        args: ['--no-sandbox', '--disable-gpu'] });
    try {
        async function setup(fields, attrs = '') {
            const page = await browser.newPage();
            await page.goto(origin);
            await page.setContent(`<form id="f" hx-post="/api/test" hx-ext="form-json" hx-swap="none" ${attrs}>${fields}<button id="save" name="action" value="save">save</button></form><input form="f" name="outside" value="linked">`);
            await page.addScriptTag({ content: fs.readFileSync(__dirname + '/htmx.min.js', 'utf8') });
            await page.addScriptTag({ content: fs.readFileSync(__dirname + '/../../vendor/form-json/form-json-v4.js', 'utf8') });
            await page.evaluate(() => {
                window.errors = [];
                document.addEventListener('htmx:error', e => window.errors.push(String(e.detail.error)));
                htmx.process(document.body);
            });
            return page;
        }
        async function send(page) {
            const pending = page.waitForRequest(r => r.url().includes('/api/test'));
            await page.click('#save');
            const request = await pending;
            return { body: JSON.parse(request.postData()), headers: request.headers() };
        }
        await t.test('原生控件、多值、嵌套和提交按钮', async () => {
            const p = await setup('<input name="title" value=""><input type="number" name="quantity" value="0"><input name="id" value="9007199254740993"><input name="tags[]" value="one"><input name="rows[0].name" value="red"><input type="number" name="rows[0].sort" value="3">');
            const r = await send(p);
            assert.match(r.headers['content-type'], /^application\/json/);
            assert.deepEqual(r.body, { title: '', quantity: 0, id: '9007199254740993', tags: ['one'], rows: [{ name: 'red', sort: 3 }], action: 'save', outside: 'linked' });
            await p.close();
        });
        await t.test('disabled checkbox 与 fieldset 不发送，未选布尔为 false', async () => {
            const p = await setup('<input type="checkbox" name="disabled" value="bad" checked disabled><fieldset disabled><input type="checkbox" name="fieldset" checked></fieldset><input type="checkbox" name="enabled">', 'ignore-drop-false-option');
            const r = await send(p);
            assert.equal(r.body.disabled, undefined);
            assert.equal(r.body.fieldset, undefined);
            assert.equal(r.body.enabled, false);
            await p.close();
        });
        await t.test('checkbox 单元素及空集合始终为数组', async () => {
            const p = await setup('<input type="checkbox" name="tags[]" value="red" checked><input type="checkbox" name="empty[]" value="x"><select multiple name="options[]"><option value="a" selected>a</option></select><select multiple name="cleared[]"></select>');
            assert.deepEqual((await send(p)).body, { tags: ['red'], empty: [], options: ['a'], cleared: [], action: 'save', outside: 'linked' });
            await p.close();
        });
        await t.test('可选空数字省略，零不省略，显式数字 select 不猜测 id', async () => {
            const p = await setup('<input type="number" name="optional"><input type="number" name="zero" value="0"><select name="status" data-json-type="number"><option value="2">two</option></select>');
            const r = await send(p);
            assert.equal(r.body.optional, undefined);
            assert.equal(r.body.zero, 0);
            assert.equal(r.body.status, 2);
            await p.close();
        });
        await t.test('multipart 不被 JSON 扩展覆盖', async () => {
            const p = await setup('<input name="title" value="upload"><input type="file" name="file">', 'enctype="multipart/form-data"');
            await p.evaluate(() => {
                const data = new DataTransfer();
                data.items.add(new File(['contents'], 'sample.txt', { type: 'text/plain' }));
                document.querySelector('[type=file]').files = data.files;
            });
            const pending = p.waitForRequest(r => r.url().includes('/api/test'));
            await p.click('#save');
            const r = await pending;
            assert.match(r.headers()['content-type'], /^multipart\/form-data; boundary=/);
            await p.close();
        });
        await t.test('稀疏索引和标量/对象冲突必须拒绝，不能静默改形', async () => {
            for (const fields of ['<input name="rows[2].name" value="gap">', '<input name="rule" value="flat"><input name="rule.name" value="nested">']) {
                const p = await setup(fields);
                let sent = 0;
                p.on('request', r => { if (r.url().includes('/api/test')) sent++; });
                await p.click('#save');
                await p.waitForFunction(() => window.errors.length > 0, { timeout: 1500 }).catch(() => {});
                assert.equal(sent, 0, fields);
                assert.ok((await p.evaluate(() => window.errors)).length);
                await p.close();
            }
        });
        await t.test('hx-include 复用 HTMX 选择器并保留类型、空集合与 disabled 语义', async () => {
            const p = await setup('', 'hx-include="#extra"');
            await p.evaluate(() => {
                const box = document.createElement('div'); box.id = 'extra';
                box.innerHTML = '<input type="number" name="quantity" value="3"><input type="checkbox" name="empty[]"><input type="checkbox" name="excluded" checked disabled><select name="none[]" multiple></select>';
                document.body.append(box);
            });
            const r = await send(p);
            assert.equal(r.body.quantity, 3); assert.deepEqual(r.body.empty, []); assert.deepEqual(r.body.none, []);
            assert.equal(r.body.excluded, undefined);
            await p.close();
        });
        await t.test('动态删行后连续编号；非法路径及反向容器冲突不发送', async () => {
            const p = await setup('<input name="rows[0].name" value="red"><input name="rows[1].name" value="blue">');
            await p.evaluate(() => {
                document.querySelector('[name="rows[0].name"]').remove();
                document.querySelector('[name="rows[1].name"]').name = 'rows[0].name';
            });
            assert.deepEqual((await send(p)).body.rows, [{ name: 'blue' }]);
            await p.close();
            for (const fields of ['<input name="rule..name" value="bad">', '<input name="rows[0].name" value="row"><input name="rows" value="scalar">', '<input name="rows.name" value="obj"><input name="rows[0].name" value="array">']) {
                const p = await setup(fields);
                let sent = 0;
                p.on('request', r => { if (r.url().includes('/api/test')) sent++; });
                await p.click('#save');
                await p.waitForFunction(() => window.errors.length > 0, { timeout: 1500 });
                assert.equal(sent, 0);
                await p.close();
            }
        });
        await t.test('GET 保持查询串，原型键不会污染 Object.prototype', async () => {
            const p = await setup('<input name="__proto__.polluted" value="yes">');
            await send(p);
            assert.equal(await p.evaluate(() => ({}).polluted), undefined);
            await p.evaluate(() => { const f = document.querySelector('form'); f.removeAttribute('hx-post'); f.setAttribute('hx-get', '/api/test'); htmx.process(f); });
            const pending = p.waitForRequest(r => r.url().includes('/api/test?'));
            await p.click('#save');
            const r = await pending;
            assert.equal(r.method(), 'GET');
            assert.equal(r.postData(), undefined);
            await p.close();
        });
    } finally {
        await browser.close();
        await new Promise(resolve => server.close(resolve));
    }
});
