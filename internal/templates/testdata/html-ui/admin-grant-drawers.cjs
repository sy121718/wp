const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require(process.env.GOWP_PUPPETEER_MODULE || 'puppeteer-core');

// 真实 HTTP、Controller、Jet、CSRF 和 HTMX；不替换 fetch 或服务端响应。
test('管理员角色、管理员菜单与角色权限的 HTML 授权闭环', async t => {
    const origin = process.env.GOWP_HTML_UI_BASE_URL;
    assert.ok(origin, '必须由 Go 测试提供服务地址');
    const browser = await puppeteer.launch({ executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome',
        headless: true, args: ['--no-sandbox', '--disable-gpu'] });
    try {
        for (const width of [1440, 768, 375]) {
            await t.test(`${width}px：父子联动、继承禁用、失败保留、保存和清空后重开`, async () => {
                const page = await browser.newPage();
                page.setDefaultTimeout(6000);
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
                });
                await page.goto(origin, { waitUntil: 'networkidle0' });
                for (const [opener, drawer, endpoint, field, chosen, rejected] of [
                    ['admin-role', '/admin/administrators/roles/drawer?user_id=42', '/api/admin/role/save', 'role_codes', 'editor', 'reject'],
                    ['admin-menu', '/admin/administrators/menus/drawer?user_id=42', '/api/admin/menu/save', 'menu_ids', '3', '4'],
                    ['role-menu', '/admin/roles/permissions/drawer?role_id=7', '/api/role/menu/save', 'menu_ids', '3', '4']
                ]) {
                    const form = '#admin-drawer-body form';
                    const input = value => `${form} [name=${field}][value="${value}"]`;
                    async function open() {
                        const response = page.waitForResponse(response => response.url() === origin + drawer);
                        await page.tap('#' + opener);
                        assert.equal((await response).status(), 200);
                        await page.waitForSelector(form, { visible: true });
                        await page.screenshot();
                        await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-drawer]')).transform === 'matrix(1, 0, 0, 1, 0, 0)');
                        if (field === 'menu_ids') await page.click(form + ' [data-perm-expand]');
                    }
                    async function clear() {
                        if (field === 'menu_ids') await page.click(form + ' [data-perm-none]');
                        else {
                            for (const control of await page.$$(form + ' input:checked:not(:disabled)')) await control.click();
                        }
                    }
                    async function saveAndReload() {
                        const response = page.waitForResponse(response => response.url() === origin + endpoint);
                        const navigation = page.waitForNavigation({ waitUntil: 'networkidle0' });
                        await page.click(form + ' [type=submit]');
                        const saved = await response;
                        assert.equal(saved.status(), 200);
                        assert.match(saved.headers()['content-type'], /^text\/html/);
                        await navigation;
                        assert.equal(await page.$eval('[data-drawer]', node => node.hidden), true);
                        assert.equal(await page.$eval('#admin-drawer-body', node => node.children.length), 0);
                    }
                    await open();
                    await clear();
                    await page.tap(input(chosen));
                    if (field === 'menu_ids') {
                        // 子项选中补齐父项；角色树取消父项清空子树，再次选子项仍可联动。
                        assert.equal(await page.$eval(input('1'), node => node.checked), true);
                        if (opener === 'role-menu') {
                            await page.click(input('1'));
                            assert.equal(await page.$eval(input(chosen), node => node.checked), false);
                            await page.click(input(chosen));
                        } else {
                            assert.deepEqual(await page.$eval(input('2'), node => [node.checked, node.disabled]), [true, true]);
                        }
                        await page.$eval(form + ' [data-perm-search]', node => {
                            node.value = 'grant:direct'; node.dispatchEvent(new Event('input', { bubbles: true }));
                        });
                        assert.equal(await page.$eval(form + ' [data-perm-row="3"]', node => node.hidden), false);
                        await page.$eval(form + ' [data-perm-search]', node => {
                            node.value = ''; node.dispatchEvent(new Event('input', { bubbles: true }));
                        });
                        await page.click(form + ' [data-perm-expand]');
                    }
                    await page.click(input(rejected));
                    await page.evaluate(selector => { window.savedForm = document.querySelector(selector); }, form);
                    const before = await page.evaluate(() => sessionStorage.getItem('successes'));
                    const finals = await page.evaluate(() => window.finals);
                    const failed = page.waitForResponse(response => response.url() === origin + endpoint);
                    await page.focus(form + ' [type=submit]');
                    await page.keyboard.press('Enter');
                    assert.equal((await failed).status(), 400);
                    await page.waitForFunction(n => window.finals > n, {}, finals);
                    assert.equal(await page.evaluate(selector => window.savedForm === document.querySelector(selector), form), true);
                    assert.equal(await page.$eval('[data-drawer]', node => node.hidden), false);
                    assert.equal(await page.$eval(input(chosen), node => node.checked), true);
                    assert.equal(await page.$eval(input(rejected), node => node.checked), true);
                    assert.equal(await page.$eval(form + ' [type=submit]', node => node.disabled), false);
                    assert.ok(await page.$eval(form + ' [data-write-status] [data-ui-error]', node => node.textContent.trim()));
                    assert.equal(await page.evaluate(() => sessionStorage.getItem('successes')), before);
                    await page.click(input(rejected));
                    await saveAndReload();
                    assert.equal(await page.evaluate(() => sessionStorage.getItem('successes')), String(Number(before || 0) + 1));
                    await open();
                    assert.equal(await page.$eval(input(chosen), node => node.checked), true);
                    assert.equal(await page.$eval(input(rejected), node => node.checked), false);
                    await clear();
                    await saveAndReload();
                    await open();
                    assert.equal(await page.$$eval(form + ' input:checked:not(:disabled)', nodes => nodes.length), 0);
                    await page.mouse.wheel({ deltaY: 60 });
                    await page.keyboard.press('Escape');
                    assert.equal(await page.$eval('[data-drawer]', node => node.hidden), true);
                    assert.equal(await page.evaluate(() => document.activeElement.id), opener);
                    const writes = requests.filter(request => request.method === 'POST' && request.url === origin + endpoint);
                    assert.equal(writes.length, 3);
                    for (const request of writes) {
                        assert.equal(request.headers['x-csrf-token'], 'browser-csrf');
                        assert.match(request.headers['content-type'], /^application\/x-www-form-urlencoded/);
                        const data = new URLSearchParams(request.body);
                        if (opener === 'admin-menu') {
                            assert.equal(data.getAll('menu_ids').includes('2'), false, '继承项不得提交');
                            assert.equal(data.getAll('menu_ids').includes('1'), false, '禁用祖先不得提交');
                        }
                    }
                    assert.deepEqual(new URLSearchParams(writes[2].body).getAll(field), []);
                }
                assert.ok(requests.filter(request => request.url.includes('/drawer?')).every(request => !request.headers['x-csrf-token']));
                assert.deepEqual(errors, []);
                await page.close();
            });
        }
    } finally { await browser.close(); }
});
