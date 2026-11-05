const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require(process.env.GOWP_PUPPETEER_MODULE || 'puppeteer-core');

// 页面、脚本与全部响应来自 Go 测试启动的真实 HTTP 服务，不注入 HTML 或替换 fetch。
test('Gin + Jet + HTMX 的真实浏览器链路', async t => {
    const origin = process.env.GOWP_HTML_UI_BASE_URL;
    assert.ok(origin, '必须由 Go 组合测试提供真实服务地址');
    const browser = await puppeteer.launch({ executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome',
        headless: true, args: ['--no-sandbox', '--disable-gpu'] });
    try {
        for (const width of [1440, 768, 375]) {
            await t.test(`${width}px：原生绑定、Jet 交换、提前拒绝和动态抽屉`, async () => {
                const page = await browser.newPage();
                page.setDefaultTimeout(4000);
                const errors = [];
                const requests = [];
                page.on('pageerror', error => errors.push(error.message));
                page.on('request', request => requests.push({ url: request.url(), headers: request.headers() }));
                await page.setViewport({ width, height: 900, hasTouch: true });
                await page.setExtraHTTPHeaders({ 'Accept-Language': 'zh-CN' });
                await page.goto(origin, { waitUntil: 'networkidle0' });
                assert.equal(await page.title(), 'HTML 交互验收');
                assert.equal(await page.$eval('#main [type=submit]', node => node.textContent), '保存');
                await page.evaluate(() => {
                    window.finals = 0; window.successes = 0;
                    document.addEventListener('htmx:finally:request', () => window.finals++);
                    document.addEventListener('write:success', () => window.successes++);
                    WBUI.writeBase.registerPayloadBuilder('document', form => ({ title: form.elements.title.value,
                        rows: [{ id: '9007199254740993', quantity: 0 }] }));
                });
                let finals = 0;
                async function submit(path, prepare) {
                    await page.evaluate((path) => {
                        const form = document.querySelector('#main');
                        form.setAttribute('hx-post', path);
                        form.removeAttribute('data-write-builder');
                        form.removeAttribute('hx-encoding');
                        if (path === '/write/document') form.setAttribute('data-write-builder', 'document');
                        if (path === '/write/upload') form.setAttribute('hx-encoding', 'multipart/form-data');
                        htmx.process(form);
                    }, path);
                    if (prepare) await page.evaluate(prepare);
                    const response = page.waitForResponse(response => response.url() === origin + path);
                    await page.evaluate(() => {
                        const form = document.querySelector('#main'), input = form.elements.title;
                        input.focus(); input.setSelectionRange(1, 3);
                        form.requestSubmit(form.querySelector('[type=submit]'));
                    });
                    const result = await response;
                    await page.waitForFunction(n => window.finals >= n, { timeout: 4000 }, ++finals);
                    assert.equal(await page.$eval('#main', node => node.hasAttribute('aria-busy')), false);
                    assert.equal(await page.$eval('#main [type=submit]', node => node.disabled), false);
                    assert.equal(await page.$eval('#main [data-disabled-button]', node => node.disabled), true);
                    return result;
                }
                async function text(selector) { return page.$eval(selector, node => node.textContent); }

                assert.equal((await submit('/write/form?title=query-must-not-bind')).status(), 200);
                assert.equal(await text('[data-title]'), 'draft <em> & name');
                assert.equal(await page.$$eval('[data-title] em', nodes => nodes.length), 0);
                assert.deepEqual(await page.$$eval('[data-tags] li', nodes => nodes.map(node => node.textContent)), ['a', 'b']);
                for (const [selector, value] of [['[data-enabled]', 'true'], ['[data-unchecked]', 'false'],
                    ['[data-disabled]', ''], ['[data-quantity]', '0'], ['[data-intent]', 'save'], ['[data-external]', 'outside']]) {
                    assert.equal(await text(selector), value);
                }
                const formRequest = requests.find(request => request.url === origin + '/write/form?title=query-must-not-bind');
                assert.equal(formRequest.headers['x-csrf-token'], 'browser-csrf');
                assert.match(formRequest.headers['content-type'], /^application\/x-www-form-urlencoded/);

                assert.equal((await submit('/write/document')).status(), 200);
                assert.equal(await text('[data-id]'), '9007199254740993');
                assert.equal(await text('[data-quantity]'), '0');
                assert.match(requests.find(request => request.url === origin + '/write/document').headers['content-type'], /^application\/json/);

                assert.equal((await submit('/write/upload', () => {
                    const transfer = new DataTransfer(); transfer.items.add(new File(['upload-content'], 'sample.txt'));
                    document.querySelector('#main [type=file]').files = transfer.files;
                })).status(), 200);
                assert.equal(await text('[data-file-name]'), 'sample.txt');
                assert.equal(await text('[data-file-body]'), 'upload-content');
                assert.match(requests.find(request => request.url === origin + '/write/upload').headers['content-type'], /^multipart\/form-data; boundary=/);

                const savedHTML = await page.$eval('#result', node => node.innerHTML);
                const successful = await page.evaluate(() => window.successes);
                for (const [path, status, prepare, message] of [
                    ['/write/reject', 422, null, '输入需要修正'],
                    ['/write/form', 403, () => { document.querySelector('#main [name=csrf_token]').value = 'wrong-token'; }, null],
                    ['/write/form', 413, () => {
                        document.querySelector('#main [name=csrf_token]').value = 'browser-csrf';
                        document.querySelector('#main textarea').value = 'x'.repeat(2048);
                    }, '请求内容超过大小限制'],
                    ['/write/upload', 413, () => {
                        document.querySelector('#main textarea').value = 'unsaved document';
                        const transfer = new DataTransfer(); transfer.items.add(new File(['x'.repeat(8192)], 'sample.txt'));
                        document.querySelector('#main [type=file]').files = transfer.files;
                    }, '请求内容超过大小限制'],
                    ['/write/broken', 500, null, null]
                ]) {
                    const response = await submit(path, prepare);
                    assert.equal(response.status(), status, path);
                    const body = await response.text();
                    assert.match(response.headers()['content-type'], /^text\/html/);
                    assert.match(body, /data-ui-error/);
                    assert.doesNotMatch(body, /<!doctype|<html|private-partial|broken\.html|Missing|SQLSTATE/i);
                    if (message) assert.match(await text('#main [data-write-status]'), new RegExp(message));
                    assert.equal(await page.$eval('#result', node => node.innerHTML), savedHTML, path);
                    assert.equal(await page.$eval('#main [name=title]', node => node.value), 'draft <em> & name', path);
                    assert.equal(await page.$eval('#main [type=file]', node => node.files[0].name), 'sample.txt', path);
                    assert.deepEqual(await page.evaluate(() => [document.activeElement.name,
                        document.activeElement.selectionStart, document.activeElement.selectionEnd]), ['title', 1, 3]);
                    assert.equal(await page.evaluate(() => window.successes), successful);
                }

                const denied = page.waitForResponse(response => response.url() === origin + '/denied');
                await page.evaluate(() => { document.querySelector('#read').setAttribute('hx-get', '/denied'); htmx.process(document.querySelector('#read')); });
                await page.click('#read');
                assert.equal((await denied).status(), 401);
                await page.waitForFunction(n => window.finals >= n, { timeout: 4000 }, ++finals);
                assert.equal(await page.$eval('#result', node => node.innerHTML), savedHTML);
                assert.equal(requests.find(request => request.url === origin + '/denied').headers['x-csrf-token'], undefined);
                await page.evaluate(() => { document.querySelector('#read').setAttribute('hx-get', '/fragment'); htmx.process(document.querySelector('#read')); });
                await page.click('#read');
                await page.waitForFunction(n => window.finals >= n, { timeout: 4000 }, ++finals);
                assert.equal(await text('[data-title]'), 'read');
                assert.equal(await page.evaluate(() => window.successes), successful);

                await page.tap('#open');
                await page.waitForSelector('[data-drawer] .wbs-trigger', { visible: true });
                // 先产生渲染帧，再等抽屉滑入终态，不能把过渡中的离屏控件当成不可点击。
                await page.screenshot();
                await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-drawer]')).transform === 'matrix(1, 0, 0, 1, 0, 0)');
                await page.tap('[data-drawer] .wbs-trigger');
                await page.mouse.wheel({ deltaY: 50 });
                await page.keyboard.press('ArrowDown'); await page.keyboard.press('Enter');
                assert.equal(await page.$eval('[data-drawer] select', node => node.value), 'b');
                await page.focus('[data-drawer] [type=submit]'); await page.keyboard.press('Enter');
                await page.waitForFunction(n => window.finals >= n, { timeout: 4000 }, ++finals);
                assert.equal(await page.$eval('[data-drawer]', node => node.hidden), true);
                assert.equal(await page.$eval('[data-drawer-body]', node => node.children.length), 0);
                assert.equal(await page.evaluate(() => document.activeElement.id), 'open');
                assert.equal(await page.evaluate(() => window.successes), successful + 1);

                for (let round = 0; round < 2; round++) {
                    await page.tap('#remote');
                    await page.waitForFunction(n => window.finals >= n, { timeout: 4000 }, ++finals);
                    await page.waitForSelector('#remote-body .wbs-trigger', { visible: true });
                    await page.screenshot();
                    await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-drawer]')).transform === 'matrix(1, 0, 0, 1, 0, 0)');
                    assert.equal(await page.$$eval('#remote-body .wbs-trigger', nodes => nodes.length), 1);
                    await page.tap('#remote-body .wbs-trigger');
                    await page.mouse.wheel({ deltaY: 50 });
                    await page.keyboard.press('ArrowDown'); await page.keyboard.press('Enter');
                    await page.evaluate(() => {
                        const form = document.querySelector('#remote-body form');
                        form.setAttribute('hx-post', '/write/reject');
                        htmx.process(form); form.requestSubmit(form.querySelector('[type=submit]'));
                    });
                    await page.waitForFunction(n => window.finals >= n, { timeout: 4000 }, ++finals);
                    assert.match(await text('#remote-body [data-write-status]'), /输入需要修正/);
                    assert.equal(await page.$eval('#remote-body select', node => node.value), 'b');
                    assert.equal(await page.$eval('[data-drawer]', node => node.hidden), false);
                    await page.evaluate(() => {
                        const form = document.querySelector('#remote-body form');
                        form.setAttribute('hx-post', '/write/form');
                        htmx.process(form); form.requestSubmit(form.querySelector('[type=submit]'));
                    });
                    await page.waitForFunction(n => window.finals >= n, { timeout: 4000 }, ++finals);
                    assert.equal(await page.$eval('[data-drawer]', node => node.hidden), true);
                    assert.equal(await page.$eval('#remote-body', node => node.children.length), 0);
                    assert.equal(await page.evaluate(() => document.activeElement.id), 'remote');
                    assert.equal(await page.evaluate(() => window.successes), successful + 2 + round);
                }
                assert.equal(requests.find(request => request.url === origin + '/drawer').headers['x-csrf-token'], undefined);
                assert.deepEqual(errors, []);
                await page.close();
            });
        }
        await t.test('并发页面的请求语言在整页与 Jet 错误片段之间隔离', async () => {
            const pages = await Promise.all(['zh-CN', 'en-US'].map(async lang => {
                const page = await browser.newPage();
                await page.setExtraHTTPHeaders({ 'Accept-Language': lang });
                await page.goto(origin, { waitUntil: 'networkidle0' });
                return page;
            }));
            await Promise.all(pages.map(async (page, index) => {
                assert.equal(await page.title(), index ? 'HTML interaction check' : 'HTML 交互验收');
                const response = page.waitForResponse(response => response.url() === origin + '/write/reject');
                await page.evaluate(() => {
                    const form = document.querySelector('#main'); form.setAttribute('hx-post', '/write/reject');
                    htmx.process(form); form.requestSubmit(form.querySelector('[type=submit]'));
                });
                assert.equal((await response).status(), 422);
                await page.waitForFunction(expected => document.querySelector('#main [data-write-status]').textContent.includes(expected),
                    { timeout: 4000 }, index ? 'Correct the input' : '输入需要修正');
                await page.close();
            }));
        });
    } finally { await browser.close(); }
});

