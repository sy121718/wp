const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require(process.env.GOWP_PUPPETEER_MODULE || 'puppeteer-core');

// 不替换 fetch 或业务响应：完整页面与抽屉均由生产 Controller / Jet 输出。
test('词条标准表单、精确语言编辑、单删和批量删除的真实闭环', async t => {
    const origin = process.env.GOWP_HTML_UI_BASE_URL;
    assert.ok(origin);
    const browser = await puppeteer.launch({ executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome',
        headless: true, args: ['--no-sandbox', '--disable-gpu'] });
    try {
        for (const width of [1440, 768, 375]) {
            await t.test(`${width}px：失败保留、转义、drop、确认取消与删除`, async () => {
                const page = await browser.newPage();
                page.setDefaultTimeout(7000);
                await page.setViewport({ width, height: 900, hasTouch: true });
                const errors = [], writes = [], reads = [];
                page.on('pageerror', error => errors.push(error.message));
                page.on('request', request => {
                    if (request.method() === 'POST' && request.url() === origin + '/api/i18n/save') {
                        writes.push({ body: new URLSearchParams(request.postData()), headers: request.headers() });
                    }
                    if (request.url().startsWith(origin + '/admin/i18n/edit?')) reads.push(request);
                });
                const cookies = process.env.GOWP_HTML_UI_COOKIE.split(';').map(part => {
                    const [name, ...value] = part.trim().split('=');
                    return { name, value: value.join('='), url: origin };
                });
                await page.setCookie(...cookies);
                await page.evaluateOnNewDocument(() => {
                    window.finals = 0;
                    document.addEventListener('htmx:finally:request', () => window.finals++);
                    document.addEventListener('write:success', () => sessionStorage.setItem('successes',
                        String(Number(sessionStorage.getItem('successes') || 0) + 1)));
                });
                const prefix = `browser.i18n.${width}`;
                const key = prefix + '.<&\"\'+?/#';
                const value = '  </textarea><script>window.i18nXSS=1</script>&\"\'\n下一行  ';
                const path = '/admin/i18n?' + new URLSearchParams({ keyword: prefix });
                const initial = await page.goto(origin + path, { waitUntil: 'networkidle0' });
                assert.equal(initial.status(), 200);
                const csrf = await page.$eval('meta[name="csrf-token"]', node => node.content);
                assert.ok(csrf);
                const form = '#admin-drawer-body form';
                async function ready() {
                    await page.waitForSelector(form, { visible: true });
                    await page.screenshot();
                    await page.waitForFunction(() => getComputedStyle(document.querySelector('[data-drawer]')).transform === 'matrix(1, 0, 0, 1, 0, 0)');
                }
                async function fill(name, text) {
                    await page.$eval(`${form} [name="${name}"]`, (node, value) => {
                        node.value = value;
                        node.dispatchEvent(new Event('input', { bubbles: true }));
                    }, text);
                }
                async function save() {
                    const response = page.waitForResponse(response => response.url() === origin + '/api/i18n/save');
                    const navigation = page.waitForNavigation({ waitUntil: 'networkidle0' });
                    await page.click(form + ' [type="submit"]');
                    const result = await response;
                    assert.equal(result.status(), 200);
                    assert.match(result.headers()['content-type'], /^text\/html/);
                    await navigation;
                    assert.equal(page.url(), origin + path);
                    assert.equal(await page.$eval('[data-drawer]', node => node.hidden), true);
                }
                async function edit() {
                    const response = page.waitForResponse(response => response.url().startsWith(origin + '/admin/i18n/edit?'));
                    const opener = await page.evaluateHandle(() => Array.from(document.querySelectorAll('tbody tr'))
                        .find(row => row.children[2].textContent === 'en-US').querySelector('[data-drawer-open]'));
                    await opener.asElement().tap();
                    assert.equal((await response).status(), 200);
                    await ready();
                }
                await page.tap('[data-drawer-open="#tpl-i18n-create"]');
                await ready();
                await fill('key', key);
                await page.select(form + ' [name="lang"]', 'en-US');
                await fill('value', '   ');
                await fill('category', 'browser');
                await fill('remark', '备注<&\"\'');
                await page.focus(form + ' [name="value"]');
                await page.evaluate(selector => {
                    window.savedForm = document.querySelector(selector);
                    window.savedFocus = document.activeElement;
                    window.savedForm.requestSubmit();
                    window.savedForm.requestSubmit();
                }, form);
                await page.waitForFunction(() => window.finals > 0);
                assert.equal(writes.length, 1, '在途重复提交必须 drop');
                assert.equal(await page.evaluate(selector => window.savedForm === document.querySelector(selector), form), true);
                assert.equal(await page.evaluate(() => document.activeElement === window.savedFocus), true);
                assert.equal(await page.$eval(form + ' [name="value"]', node => node.value), '   ');
                assert.equal(await page.$eval(form + ' [type="submit"]', node => node.disabled), false);
                assert.ok(await page.$eval(form + ' [data-write-status] [data-ui-error]', node => node.textContent.trim()));
                assert.equal(await page.evaluate(() => sessionStorage.getItem('successes')), null);
                await fill('value', value);
                await save();
                assert.equal(await page.evaluate(() => window.i18nXSS), undefined);
                await edit();
                assert.equal(await page.$eval(form + ' [name="key"]', node => node.value), key);
                assert.equal(await page.$eval(form + ' [name="lang"]', node => node.value), 'en-US');
                assert.equal(await page.$eval(form + ' [name="value"]', node => node.value), value);
                assert.equal(await page.$eval(form + ' [name="remark"]', node => node.value), '备注<&\"\'');
                await fill('value', value + ' updated');
                await fill('category', '');
                await fill('remark', '');
                await save();
                await edit();
                assert.equal(await page.$eval(form + ' [name="value"]', node => node.value), value + ' updated');
                assert.equal(await page.$eval(form + ' [name="category"]', node => node.value), '');
                assert.equal(await page.$eval(form + ' [name="remark"]', node => node.value), '');
                await page.mouse.wheel({ deltaY: 60 });
                await page.keyboard.press('Escape');
                await page.waitForFunction(() => document.querySelector('[data-drawer]').hidden);
                const deleteButton = await page.evaluateHandle(() => Array.from(document.querySelectorAll('tbody tr'))
                    .find(row => row.children[2].textContent === 'en-US').querySelector('button[form]'));
                assert.equal(await page.$eval('[data-drawer]', node => getComputedStyle(node).display), 'none');
                assert.equal(await page.$eval('[data-drawer-mask]', node => getComputedStyle(node).display), 'none');
                assert.equal(await deleteButton.evaluate(node => {
                    const rect = node.getBoundingClientRect();
                    return node.contains(document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2));
                }), true, '关闭后的抽屉不能截获行按钮点击');
                async function confirmClick(click, accept) {
                    let timer;
                    const handled = new Promise((resolve, reject) => {
                        const onDialog = async dialog => {
                            clearTimeout(timer);
                            try {
                                assert.equal(dialog.type(), 'confirm');
                                assert.ok(dialog.message());
                                if (accept) await dialog.accept(); else await dialog.dismiss();
                                resolve();
                            } catch (error) { reject(error); }
                        };
                        page.once('dialog', onDialog);
                        timer = setTimeout(() => {
                            page.off('dialog', onDialog);
                            reject(new Error('真实按钮没有触发确认对话框'));
                        }, 7000);
                    });
                    await Promise.all([handled, click()]);
                }
                await confirmClick(() => deleteButton.asElement().click(), false);
                assert.equal(writes.length, 3, '取消确认不得发请求');
                const deleted = page.waitForResponse(response => response.url() === origin + '/api/i18n/save');
                const reloaded = page.waitForNavigation({ waitUntil: 'networkidle0' });
                await confirmClick(() => deleteButton.asElement().click(), true);
                assert.equal((await deleted).status(), 200);
                await reloaded;
                assert.equal(await page.$$eval('tbody [name="ids"]', nodes => nodes.length), 2);
                await page.click('[data-check-all]');
                assert.equal(await page.$eval('[data-bulk-bar]', node => node.hidden), false);
                assert.equal(await page.$eval('[data-bulk-count]', node => node.textContent.includes('2')), true);
                await confirmClick(() => page.click('[data-bulk-bar] [type="submit"]'), false);
                assert.equal(writes.length, 4, '取消批量确认不得发请求');
                const bulk = page.waitForResponse(response => response.url() === origin + '/api/i18n/save');
                const empty = page.waitForNavigation({ waitUntil: 'networkidle0' });
                await confirmClick(() => page.click('[data-bulk-bar] [type="submit"]'), true);
                const bulkResponse = await bulk;
                assert.equal(bulkResponse.status(), 200);
                const bulkText = await bulkResponse.text();
                assert.match(bulkText, /<p role="status">[^<]+<\/p>/);
                assert.doesNotMatch(bulkText, /：2/);
                await empty;
                assert.equal(await page.$$eval('tbody [name="ids"]', nodes => nodes.length), 0);
                assert.equal(writes.length, 5);
                assert.equal(writes[3].body.get('action'), 'delete');
                assert.deepEqual(writes[3].body.getAll('ids'), [], '单删不能带上外层批量表单字段');
                assert.equal(writes[3].body.get('key'), key);
                assert.equal(writes[3].body.get('lang'), 'en-US');
                assert.equal(writes[4].body.get('action'), 'bulkDelete');
                assert.deepEqual(writes[4].body.getAll('ids').sort(), [key + '|ja-JP', key + '|zh-CN']);
                for (const request of writes) {
                    assert.ok(request.headers['x-csrf-token'] === csrf, '标准写请求必须携带页面 CSRF');
                    assert.match(request.headers['content-type'], /^application\/x-www-form-urlencoded/);
                }
                for (const request of reads) {
                    assert.equal(new URL(request.url()).searchParams.get('key'), key);
                    assert.equal(new URL(request.url()).searchParams.get('lang'), 'en-US');
                    assert.equal(request.headers()['x-csrf-token'], undefined);
                }
                assert.equal(await page.evaluate(() => sessionStorage.getItem('successes')), '4');
                assert.equal(await page.evaluate(() => window.i18nXSS), undefined);
                assert.deepEqual(errors, []);
                await page.close();
            });
        }
    } finally { await browser.close(); }
});
