const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require(process.env.GOWP_PUPPETEER_MODULE || 'puppeteer-core');

// 业务响应不做拦截；失败分别来自真实 404、CSRF 与浏览器离线。
test('SEO 三入口只交换 Jet 结果，保留未保存状态', async t => {
    const origin = process.env.GOWP_PRODUCT_BASE_URL;
    const project = process.env.GOWP_PRODUCT_PROJECT_ID;
    const browser = await puppeteer.launch({ executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--disable-gpu'] });
    try {
        for (const width of [375, 768, 1440]) {
            await t.test(`${width}px：独立表单、错误隔离、防重入与交换后复用`, async t => {
                const page = await browser.newPage();
                page.setDefaultTimeout(7000);
                await page.setViewport({ width, height: 900, hasTouch: true });
                await page.setExtraHTTPHeaders({ 'Accept-Language': 'zh-CN' });
                const errors = [], writes = [];
                let stage = '';
                page.on('pageerror', error => errors.push(error.message));
                page.on('request', request => { if (request.method() === 'POST') writes.push(request); });
                // 诊断只记录请求地址，不记录 CSRF 等请求凭据。
                t.after(async () => {
                    t.diagnostic(JSON.stringify({stage, errors, writes: writes.map(r => ({url: r.url()}))}));
                    await page.close();
                });
                async function click(selector) {
                    if (width !== 375) {
                        await page.locator(selector).click();
                        return;
                    }
                    await page.$eval(selector, async node => {
                        const drawer = node.closest('[data-drawer]');
                        if (drawer) await Promise.all(drawer.getAnimations().map(animation => animation.finished));
                    });
                    // 用真实滚轮把目标带进视口，稳定后再触摸，不用程序滚动后的旧坐标。
                    const deltaY = await page.$eval(selector, node => {
                        const r = node.getBoundingClientRect();
                        return r.top < 60 || r.bottom > innerHeight - 20 ? r.y - innerHeight / 2 : 0;
                    });
                    if (deltaY) {
                        await page.mouse.move(200, 450);
                        await page.mouse.wheel({deltaY});
                    }
                    await page.$eval(selector, node => new Promise((resolve, reject) => {
                        const deadline = performance.now() + 7000;
                        let previous = node.getBoundingClientRect().y, stableSince = performance.now();
                        function stable(now) {
                            if (now >= deadline) { reject(new Error('触摸目标在 7 秒内未稳定')); return; }
                            const y = node.getBoundingClientRect().y;
                            if (Math.abs(y - previous) > 0.1) stableSince = now;
                            previous = y;
                            if (now - stableSince >= 150) resolve();
                            else requestAnimationFrame(stable);
                        }
                        requestAnimationFrame(stable);
                    }));
                    const point = await page.$eval(selector, node => {
                        const r = node.getBoundingClientRect();
                        const x = r.x + r.width / 2, y = r.y + r.height / 2;
                        const hit = document.elementFromPoint(x, y);
                        return {x, y, target: hit?.tagName, hit: hit === node || node.contains(hit)};
                    });
                    assert.equal(point.hit, true, `真实触摸不得被遮挡：${selector} ${JSON.stringify(point)}`);
                    await page.touchscreen.tap(point.x, point.y);
                }
                for (const kind of ['product', 'category', 'brand']) {
                    stage = `${kind} 加载页面`;
                    const id = process.env[`GOWP_SEO_${kind.toUpperCase()}`];
                    const path = `/api/product/${kind === 'product' ? '' : kind + '/'}seo-score`;
                    const selector = `[data-drawer-body] form[hx-post="${path}"]`;
                    // 分类/品牌的实体保存表单：独立评分表单之外的未保存状态载体。
                    const editForm = `[data-drawer-body] form[hx-post="/api/product/${kind}/update"]`;
                    const button = `${selector} button[type=submit]`;
                    const panel = `${selector} [data-product-seo-panel]`;
                    const status = `${selector} [data-write-status]`;
                    const url = kind === 'product' ? `/admin/products/edit?project=${project}&product=${id}` : `/admin/product-${kind === 'category' ? 'categories' : 'brands'}?project=${project}`;
                    await page.goto(origin + url, {waitUntil: 'networkidle0'});
                    if (kind === 'product') {
                        await page.$eval('#product-edit-main [name=name]', node => { node.value = ''; });
                    }
                    // 分类/品牌用页面上真实存在的行入口：夹具创建的实体不一定排在列表首行，
                    // 按 env 里的 id 拼选择器会把「列表没渲染这一行」误判成交互失败。
                    const opener = kind === 'product'
                        ? '[data-drawer-open="#tpl-product-seo"]'
                        : `[data-drawer-open^="#tpl-${kind}-edit-"]`;
                    const beforeOpen = writes.length;
                    await click(opener);
                    await page.waitForSelector(button + ':not([hidden]):not([disabled])');
                    if (kind === 'product') {
                        await page.waitForSelector(panel);
                        await page.waitForFunction(s => !document.querySelector(s).hasAttribute('aria-busy'), {}, selector);
                        assert.equal(writes.length, beforeOpen + 1, '商品抽屉应自动评分一次');
                    } else {
                        assert.equal(writes.length, beforeOpen, '分类品牌不应自动提交保存或评分');
                        await page.$eval(editForm, node => { node.querySelector('[name=name]').value = ''; });
                        await page.$eval('[data-drawer-body] trix-editor', (node, kind) => node.editor.loadHTML(`<p>未保存-${kind}</p>`), kind);
                    }
                    async function checkPreserved() {
                        assert.equal(await page.$eval('[data-drawer]', n => n.hidden), false, '评分不得关闭抽屉');
                        if (kind === 'product') assert.equal(await page.$eval('#product-edit-main [name=name]', n => n.value), '');
                        else {
                            assert.equal(await page.$eval(editForm, n => n.querySelector('[name=name]').value), '');
                            assert.match(await page.$eval('[data-drawer-body] trix-editor', n => n.editor.getDocument().toString()), new RegExp(`未保存-${kind}`));
                            assert.equal(await page.$eval(`${editForm} [data-write-status]`, n => n.hidden), true, '不能串入保存表单错误区');
                        }
                    }
                    stage = `${kind} 成功、防重复和忙碌恢复`;
                    const before = writes.length;
                    const response = page.waitForResponse(r => r.url().endsWith(path) && r.request().method() === 'POST');
                    await click(button);
                    await page.waitForFunction(s => document.querySelector(s).getAttribute('aria-busy') === 'true', {}, selector);
                    assert.equal(await page.$eval(button, n => n.disabled), true);
                    await page.$eval(selector, form => form.requestSubmit());
                    assert.equal((await response).status(), 200);
                    await page.waitForSelector(panel);
                    await page.waitForFunction(s => !document.querySelector(s).hasAttribute('aria-busy'), {}, selector);
                    assert.equal(writes.length, before + 1, '进行中的重复 submit 应 drop');
                    assert.equal(await page.$eval(button, n => n.disabled), false);
                    await checkPreserved();
                    const req = writes.at(-1);
                    assert.match(req.headers()['content-type'], /^application\/x-www-form-urlencoded/);
                    assert.ok(req.headers()['x-csrf-token']);
                    assert.equal(new URLSearchParams(req.postData()).get('id'), id);
                    const result = await page.$eval(panel, n => n.outerHTML);
                    stage = `${kind} 404 失败只进入自己的错误区`;
                    await page.$eval(`${selector} [name=id]`, n => { n.value = '00000000-0000-4000-8000-000000000000'; });
                    const failure = page.waitForResponse(r => r.url().endsWith(path) && r.request().method() === 'POST');
                    await click(button);
                    assert.equal((await failure).status(), 404);
                    await page.waitForSelector(`${status}:not([hidden]) [data-ui-error][role=alert]`);
                    await page.waitForFunction(s => !document.querySelector(s).hasAttribute('aria-busy'), {}, selector);
                    assert.equal(await page.$eval(panel, n => n.outerHTML), result, '失败不能替换已有评分结果');
                    assert.ok((await page.$eval(status, n => n.textContent)).trim());
                    await checkPreserved();
                    stage = `${kind} CSRF 拒绝及离线恢复`;
                    const token = await page.$eval(`${selector} [name=csrf_token]`, n => n.value);
                    await page.$eval(`${selector} [name=csrf_token]`, n => { n.value = 'invalid'; });
                    const denied = page.waitForResponse(r => r.url().endsWith(path) && r.request().method() === 'POST');
                    await click(button);
                    assert.equal((await denied).status(), 403);
                    await page.waitForFunction(s => !document.querySelector(s).hasAttribute('aria-busy'), {}, selector);
                    await page.$eval(selector, (form, values) => { form.elements.id.value = values.id; form.elements.csrf_token.value = values.token; }, {id, token});
                    await page.setOfflineMode(true);
                    await click(button);
                    await page.waitForFunction(s => !document.querySelector(s).hasAttribute('aria-busy'), {}, selector);
                    assert.equal(await page.$eval(status, n => n.hidden), false);
                    await page.setOfflineMode(false);
                    await page.waitForSelector('.wb-toast.is-error:not(.is-leaving) .wb-toast-x');
                    assert.equal(await page.$eval('.wb-toast-text', node => getComputedStyle(node).pointerEvents), 'none', '提示文字不能遮挡底下的评分按钮');
                    assert.equal(await page.$eval('.wb-toast-x', node => getComputedStyle(node).pointerEvents), 'auto', '提示关闭按钮仍须可操作');
                    await checkPreserved();
                    stage = `${kind} 交换后键盘提交和再次打开`;
                    await page.focus(button);
                    const again = page.waitForResponse(r => r.url().endsWith(path) && r.request().method() === 'POST');
                    await page.keyboard.press('Enter');
                    assert.equal((await again).status(), 200);
                    await page.waitForFunction(s => !document.querySelector(s).hasAttribute('aria-busy'), {}, selector);
                    assert.equal(await page.$eval(status, n => n.hidden), true);
                    await page.mouse.wheel({deltaY: 180});
                    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), '页面不应横向溢出');
                    await checkPreserved();
                    await page.keyboard.press('Escape');
                    await click(opener);
                    await page.waitForSelector(button + ':not([hidden]):not([disabled])');
                    if (kind === 'product') await page.waitForSelector(panel);
                    else { await click(button); await page.waitForSelector(panel); }
                    await page.waitForFunction(s => !document.querySelector(s).hasAttribute('aria-busy'), {}, selector);
                    await page.keyboard.press('Escape');
                    assert.equal(writes.filter(r => !r.url().endsWith('seo-score')).length, 0, 'SEO 不能触发实体保存');
                    if (kind !== 'product') {
                        stage = `${kind} 仅评分权限可达`;
                        await page.goto(origin + url + '&readonly=1', {waitUntil: 'networkidle0'});
                        await click(opener);
                        await page.waitForSelector(button + ':not([hidden]):not([disabled])');
                        assert.equal(await page.$(editForm), null, '仅评分权限不显示编辑表单');
                        await click(button);
                        await page.waitForSelector(panel);
                        await page.keyboard.press('Escape');
                    }
                }
                assert.deepEqual(errors, []);
            });
        }
    } finally { await browser.close(); }
});
