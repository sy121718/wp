const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require(process.env.GOWP_PUPPETEER_MODULE || 'puppeteer-core');

// 不替换 fetch、不注入响应 HTML；所有片段都由真实 Controller 与 Jet 返回。
test('菜单与权限编辑抽屉的真实 HTML 闭环', async t => {
    const origin = process.env.GOWP_HTML_UI_BASE_URL;
    assert.ok(origin, '必须由 Go 测试提供服务地址');
    const browser = await puppeteer.launch({ executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome',
        headless: true, args: ['--no-sandbox', '--disable-gpu'] });
    try {
        for (const width of [1440, 768, 375]) {
            await t.test(`${width}px：失败保留、成功关闭刷新、再次打开`, async () => {
                const page = await browser.newPage();
                page.setDefaultTimeout(5000);
                await page.setViewport({ width, height: 900, hasTouch: true });
                const errors = [], requests = [];
                page.on('pageerror', error => errors.push(error.message));
                page.on('request', request => requests.push({ url: request.url(), method: request.method(),
                    headers: request.headers(), body: request.postData() }));
                await page.evaluateOnNewDocument(() => {
                    window.finals = 0;
                    document.addEventListener('htmx:finally:request', () => window.finals++);
                    document.addEventListener('write:success', () => sessionStorage.setItem('successes',
                        String(Number(sessionStorage.getItem('successes') || 0) + 1)));
                    document.addEventListener('wbui:drawer-close', () => sessionStorage.setItem('closes',
                        String(Number(sessionStorage.getItem('closes') || 0) + 1)));
                });
                await page.goto(origin, { waitUntil: 'networkidle0' });
                for (const [opener, path, field, label] of [
                    ['menu', 'menus', 'title', '菜单'], ['permission', 'permissions', 'permission_name', '权限']
                ]) {
                    const selector = '#admin-drawer-body form';
                    const value = `${label} ${width}`;
                    async function open() {
                        const response = page.waitForResponse(response => response.url() === `${origin}/admin/${path}/edit?id=42`);
                        await page.tap('#' + opener);
                        assert.equal((await response).status(), 200);
                        await page.waitForSelector(selector + ' .wbs-trigger', { visible: true });
                        await page.screenshot();
                        await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-drawer]')).transform === 'matrix(1, 0, 0, 1, 0, 0)');
                    }
                    await open();
                    assert.equal(await page.$eval(selector + ' [name=id]', el => el.value), '42');
                    assert.equal(await page.$eval(selector + ' [name=status]', el => el.value), '0');
                    if (opener === 'menu') {
                        assert.equal(await page.$eval(selector + ' [name=parent_id]', el => el.value), '7');
                        assert.equal(await page.$eval(selector + ' [name=parent_id] [value="42"]', el => el.disabled), true);
                        await page.tap(selector + ' [data-perm-picker-none]');
                        assert.equal(await page.$$eval(selector + ' [name=permission_codes]:checked', els => els.length), 0);
                        await page.waitForSelector(selector + ' [data-icon-field][data-icon-ready="1"]');
                    }
                    await page.$eval(selector + ` [name=${field}]`, (el, value) => { el.value = value; }, value);
                    await page.$eval(selector + ' [name=remark]', el => { el.value = 'reject'; });
                    await page.evaluate(selector => { window.savedForm = document.querySelector(selector); }, selector);
                    const before = await page.evaluate(() => [sessionStorage.getItem('successes'), sessionStorage.getItem('closes')]);
                    const finals = await page.evaluate(() => window.finals);
                    const rejected = page.waitForResponse(response => response.url() === `${origin}/admin/${path}/update`);
                    await page.focus(selector + ` [name=${field}]`);
                    await page.$eval(selector + ` [name=${field}]`, el => el.setSelectionRange(1, 3));
                    await page.keyboard.press('Enter');
                    const response = await rejected;
                    assert.equal(response.status(), 400);
                    assert.match(response.headers()['content-type'], /^text\/html/);
                    await page.waitForFunction(n => window.finals > n, {}, finals);
                    assert.equal(await page.evaluate(selector => window.savedForm === document.querySelector(selector), selector), true);
                    assert.equal(await page.$eval(selector + ` [name=${field}]`, el => el.value), value);
                    assert.equal(await page.$eval('[data-drawer]', el => el.hidden), false);
                    assert.equal(await page.$eval(selector, el => el.hasAttribute('aria-busy')), false);
                    assert.equal(await page.$eval(selector + ' [type=submit]', el => el.disabled), false);
                    assert.ok(await page.$eval(selector + ' [data-write-status] [data-ui-error]', el => el.textContent.trim()));
                    assert.equal(await page.$eval(selector + ' [data-write-result]', el => el.children.length), 0);
                    assert.deepEqual(await page.evaluate(() => [document.activeElement.name,
                        document.activeElement.selectionStart, document.activeElement.selectionEnd]), [field, 1, 3]);
                    assert.deepEqual(await page.evaluate(() => [sessionStorage.getItem('successes'), sessionStorage.getItem('closes')]), before);
                    // 原表单上修正输入再提交，真实 location.reload 必须完成，不能以 mock 成功动作代替。
                    await page.$eval(selector + ' [name=remark]', el => { el.value = ''; });
                    const updated = page.waitForResponse(response => response.url() === `${origin}/admin/${path}/update`);
                    const navigation = page.waitForNavigation({ waitUntil: 'networkidle0' });
                    await page.click(selector + ' [type=submit]');
                    assert.equal((await updated).status(), 200);
                    await navigation;
                    assert.deepEqual(await page.evaluate(() => [sessionStorage.getItem('successes'), sessionStorage.getItem('closes')]),
                        [String(Number(before[0] || 0) + 1), String(Number(before[1] || 0) + 1)]);
                    assert.equal(await page.$eval('[data-drawer]', el => el.hidden), true);
                    assert.equal(await page.$eval('#admin-drawer-body', el => el.children.length), 0);
                    const writes = requests.filter(request => request.method === 'POST' && request.url === `${origin}/admin/${path}/update`);
                    assert.equal(writes.length, 2);
                    for (const request of writes) {
                        assert.equal(request.headers['x-csrf-token'], 'browser-csrf');
                        assert.match(request.headers['content-type'], /^application\/x-www-form-urlencoded/);
                        const body = new URLSearchParams(request.body);
                        assert.equal(body.get(field), value);
                        assert.equal(body.get('status'), '0');
                        if (opener === 'menu') assert.deepEqual(body.getAll('permission_codes'), []);
                    }
                    await open();
                    assert.equal(await page.$eval(selector + ` [name=${field}]`, el => el.value), value);
                    assert.equal(await page.$$eval(selector + ' .form-group:has(select[name=status]) .wbs-trigger', els => els.length), 1);
                    await page.mouse.wheel({ deltaY: 60 });
                    await page.keyboard.press('Escape');
                    assert.equal(await page.$eval('[data-drawer]', el => el.hidden), true);
                    assert.equal(await page.evaluate(() => document.activeElement.id), opener);
                }
                // GET 不继承写令牌，成功的表单写入也没有退化成原生文档导航。
                assert.ok(requests.filter(request => request.url.includes('/edit?')).every(request => !request.headers['x-csrf-token']));
                assert.deepEqual(errors, []);
                await page.close();
            });
        }
    } finally { await browser.close(); }
});
