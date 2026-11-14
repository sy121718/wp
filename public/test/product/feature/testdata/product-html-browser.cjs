const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require(process.env.GOWP_PUPPETEER_MODULE || 'puppeteer-core');
const { tabTo, activate } = require('../../../../../internal/templates/testdata/html-ui/input-matrix.cjs');

// 使用真实页面、嵌入资产、HTTP 与数据库，不拦截或伪造业务响应。
test('商品创建、主体编辑与列表删除的真实 HTML 链路', async t => {
    const origin = process.env.GOWP_PRODUCT_BASE_URL;
    const project = process.env.GOWP_PRODUCT_PROJECT_ID;
    assert.ok(origin && project);
    const browser = await puppeteer.launch({ executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome',
        headless: true, args: ['--no-sandbox', '--disable-gpu'] });
    try {
        for (const [width, price] of [[1440, null], [768, '0'], [375, '12.50']]) {
            await t.test(`${width}px：可选输入、失败保留、导航、确认与原子批量`, async t => {
                let stage = '加载新建页';
                const page = await browser.newPage();
                page.setDefaultTimeout(6000);
                const errors = [];
                const writes = [];
                page.on('pageerror', error => errors.push(error.message));
                page.on('request', request => {
                    if (request.method() === 'POST') writes.push({ url: request.url(), body: request.postData(), headers: request.headers() });
                });
                t.after(async () => {
                    t.diagnostic(JSON.stringify({ stage, url: page.url(), errors, writes: writes.map(write => ({ url: write.url, body: write.body, contentType: write.headers['content-type'] })) }));
                    await page.close();
                });
                await page.setViewport({ width, height: 900, hasTouch: true });
                await page.setExtraHTTPHeaders({ 'Accept-Language': 'zh-CN' });
                await page.goto(`${origin}/admin/products/new?project=${project}`, { waitUntil: 'networkidle0' });
                const form = '#product-create-form';
                const submit = `${form} button[type=submit]`;
                await page.waitForSelector(`${form}[data-sku-enhanced="1"]`);
                await page.waitForSelector(`${submit}:not([disabled]):not([hidden])`);
                assert.equal(await page.$eval(`${form} [name=defaultPrice]`, node => node.disabled), true);
                assert.equal(await page.$eval(`${form} [name=quantity]`, node => node.disabled), true);
                const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
                assert.ok(overflow <= 1, `新建页不应横向溢出：${overflow}px`);
                await page.mouse.wheel({ deltaY: 280 });
                await page.keyboard.press('Tab');
                stage = '动态替换表单并重新接线';
                await page.evaluate(() => {
                    // 模拟真实片段替换后的统一扫描，不手动调用页面增强器。
                    const old = document.querySelector('#product-create-form');
                    const next = old.cloneNode(true);
                    next.removeAttribute('data-sku-enhanced');
                    next.querySelector('[data-sku-input]').removeAttribute('data-sku-enhanced');
                    old.replaceWith(next);
                    document.dispatchEvent(new CustomEvent('wb:rescan', { detail: { el: next } }));
                });
                await page.waitForSelector(`${form}[data-sku-enhanced="1"]`);
                await page.type(`${form} [name=name]`, `browser-created-${width}`);
                await page.type(`${form} [name=slug]`, 'browser-conflict');
                await page.$$eval(`${form} [name=attributeIds]`, nodes => nodes.forEach(node => { node.checked = true; }));
                if (width === 375) await page.tap(`${form} [data-default-price-switch]`);
                else await page.click(`${form} [data-default-price-switch]`);
                assert.equal(await page.$eval(`${form} [name=defaultPrice]`, node => node.required && !node.disabled), true);
                const beforeInvalid = writes.length;
                await page.click(submit);
                assert.equal(await page.$eval(`${form} [name=defaultPrice]`, node => node.validity.valueMissing), true);
                assert.equal(writes.length, beforeInvalid, '空的必填价格不能发送请求');
                if (price === null) await page.click(`${form} [data-default-price-switch]`);
                else await page.type(`${form} [name=defaultPrice]`, price);
                await page.click(`${form} [name=trackQuantity]`);
                await page.type(`${form} [name=quantity]`, '0');
                stage = '创建冲突返回 409';
                const failed = page.waitForResponse(response => response.url() === `${origin}/api/product/create`);
                await page.click(submit);
                assert.equal((await failed).status(), 409);
                await page.waitForSelector(`${form} [data-write-status]:not([hidden]) [data-ui-error]`);
                assert.equal(new URL(page.url()).pathname, '/admin/products/new');
                assert.equal(await page.$eval(`${form} [name=name]`, node => node.value), `browser-created-${width}`);
                assert.equal(await page.$eval(`${form} [name=quantity]`, node => node.value), '0');
                assert.equal(await page.$$eval(`${form} [name=attributeIds]:checked`, nodes => nodes.length), 2);
                await page.waitForFunction(selector => !document.querySelector(selector).disabled && !document.querySelector('#product-create-form').hasAttribute('aria-busy'), {}, submit);
                const firstBody = new URLSearchParams(writes.at(-1).body);
                assert.equal(firstBody.get('defaultPrice'), price);
                assert.equal(firstBody.get('quantity'), '0');
                assert.equal(firstBody.getAll('attributeIds').length, 2);
                assert.match(writes.at(-1).headers['content-type'], /^application\/x-www-form-urlencoded/);
                assert.ok(writes.at(-1).headers['x-csrf-token']);
                await page.$eval(`${form} [name=slug]`, (node, value) => { node.value = value; }, `browser-created-${width}`);
                stage = '创建成功并按 Jet 链接导航';
                const createdResponse = page.waitForResponse(response => response.url() === `${origin}/api/product/create`);
                const navigation = page.waitForNavigation({ waitUntil: 'networkidle0' });
                await page.click(submit);
                const created = await createdResponse;
                assert.equal(created.status(), 200);
                const createdHTML = await created.text();
                assert.match(createdHTML, /data-product-created-link/);
                assert.doesNotMatch(createdHTML, /<!doctype|"code":/i);
                await navigation;
                assert.equal(new URL(page.url()).pathname, '/admin/products/edit');
                assert.equal(new URL(page.url()).searchParams.get('project'), project);
                assert.ok(new URL(page.url()).searchParams.get('product'));
                assert.match(await page.$eval('h1', node => node.textContent), new RegExp(`browser-created-${width}`));

                stage = '主体编辑：富文本、图库与未保存变体';
                const editIDs = JSON.parse(process.env.GOWP_PRODUCT_EDIT_IDS);
                await page.goto(`${origin}/admin/products/edit?project=${project}&product=${editIDs[width]}`, { waitUntil: 'networkidle0' });
                const editForm = '#product-edit-main';
                const editSubmit = 'button[form="product-edit-main"]';
                await page.waitForSelector(`${editSubmit}:not([disabled]):not([hidden])`);
                await page.waitForFunction(() => !!document.querySelector('trix-editor')?.editor);
                assert.deepEqual(await page.$$eval(`${editForm} [data-gallery-alt]`, nodes => nodes.map(node => node.value)), ['', '第二张']);
                await page.evaluate(width => {
                    const form = document.querySelector('#product-edit-main');
                    form.elements.name.value = `browser-edited-${width}`;
                    form.elements.slug.value = 'browser-conflict';
                    form.querySelectorAll('[name=attributeIds]').forEach(node => { node.checked = false; });
                    const editor = document.querySelector('trix-editor');
                    editor.editor.loadHTML(`<p>浏览器富文本-${width}</p>`);
                    const alts = form.querySelectorAll('[data-gallery-alt]');
                    alts[1].value = `浏览器说明-${width}`;
                    alts[1].dispatchEvent(new Event('input', { bubbles: true }));
                    form.elements.defaultPrice.value = width === 1440 ? '' : width === 768 ? '0' : '23.50';
                    form.elements.weight.value = width === 1440 ? '' : width === 768 ? '0' : '1.25';
                    const variant = document.querySelector('[data-variant-sku]');
                    variant.value = `未保存变体-${width}`;
                    window.editNodes = { form, editor, variant, gallery: form.querySelector('[data-media-gallery]') };
                }, width);
                await page.setExtraHTTPHeaders({ 'Accept-Language': 'zh-CN', 'X-Product-Test-Delay': '1' });
                const beforeEdit = writes.length;
                const updateURL = `${origin}/api/product/update`;
                const rejectedUpdate = page.waitForResponse(response => response.url() === updateURL);
                if (width === 375) await page.tap(editSubmit);
                else await page.click(editSubmit);
                await page.waitForSelector(`${editSubmit}[disabled]`);
                await page.evaluate(() => {
                    // 在途再次提交应被 hx-sync 丢弃；响应不能改变用户随后移动到的焦点。
                    const form = document.querySelector('#product-edit-main');
                    form.requestSubmit(document.querySelector('button[form="product-edit-main"]'));
                    form.elements.subtitle.focus();
                    form.elements.subtitle.value = '保留输入与光标';
                    form.elements.subtitle.setSelectionRange(2, 4);
                });
                assert.equal((await rejectedUpdate).status(), 409);
                await page.waitForSelector(`${editForm} [data-write-status] [data-ui-error]`);
                await page.waitForFunction(selector => !document.querySelector(selector).disabled && !document.querySelector('#product-edit-main').hasAttribute('aria-busy'), {}, editSubmit);
                assert.equal(writes.length - beforeEdit, 1, '主体编辑重复提交必须丢弃而不是排队');
                assert.deepEqual(await page.evaluate(() => {
                    const input = document.querySelector('#product-edit-main').elements.subtitle;
                    return [document.activeElement === input, input.value, input.selectionStart, input.selectionEnd];
                }), [true, '保留输入与光标', 2, 4]);
                assert.equal(await page.$eval('#product-edit-result', node => node.textContent), '');
                assert.equal(await page.$eval('[data-variant-form] [data-write-status]', node => node.hidden), true);
                const attempted = writes.at(-1);
                assert.match(attempted.headers['content-type'], /^application\/json/);
                assert.ok(attempted.headers['x-csrf-token']);
                const object = JSON.parse(attempted.body);
                assert.match(object.description.html, new RegExp(`浏览器富文本-${width}`));
                assert.deepEqual(object.imageAlts, ['', `浏览器说明-${width}`]);
                assert.equal(object.images.length, 2);
                for (const field of ['attributeIds', 'categoryIds', 'tagIds']) assert.deepEqual(object[field], []);
                assert.equal(object.brandId, '');
                assert.equal(object.primaryCategoryId, '');
                if (width === 1440) {
                    assert.equal(object.clearDefaultPrice, true);
                    assert.equal(object.clearWeight, true);
                    assert.equal('defaultPrice' in object || 'weight' in object, false);
                } else {
                    assert.equal(object.defaultPrice, width === 768 ? 0 : 23.5);
                    assert.equal(object.weight, width === 768 ? 0 : 1.25);
                }
                stage = '主体保存成功仅交换结果区';
                await page.$eval(`${editForm} [name=slug]`, (node, width) => { node.value = `browser-edit-${width}`; }, width);
                const editLocation = page.url();
                const updatedResponse = page.waitForResponse(response => response.url() === updateURL);
                if (width === 375) await page.tap(editSubmit);
                else await page.click(editSubmit);
                await page.waitForSelector(`${editSubmit}[disabled]`);
                assert.equal((await updatedResponse).status(), 200);
                await page.waitForSelector('#product-edit-result p[role=status]');
                await page.waitForFunction(selector => !document.querySelector(selector).disabled && !document.querySelector('#product-edit-main').hasAttribute('aria-busy'), {}, editSubmit);
                assert.equal(page.url(), editLocation);
                assert.equal(await page.evaluate(() => {
                    const old = window.editNodes;
                    return old.form === document.querySelector('#product-edit-main') && old.editor === document.querySelector('trix-editor') &&
                        old.variant === document.querySelector('[data-variant-sku]') && old.gallery === document.querySelector('[data-media-gallery]');
                }), true, '成功与失败都不得替换编辑器、图库或未保存变体');
                assert.equal(await page.$eval('[data-variant-sku]', node => node.value), `未保存变体-${width}`);
                assert.match(await page.$eval(`${editForm} [name=descriptionHtml]`, node => node.value), new RegExp(`浏览器富文本-${width}`));
                assert.equal(await page.$eval(`${editForm} [data-write-status]`, node => node.hidden), true);
                assert.ok(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth <= 1), '编辑页不应横向溢出');

                stage = '评分失败只更新抽屉错误区';
                const ratingPanel = `#product-ratings-${editIDs[width]}`;
                const ratingOpen = `${ratingPanel} [data-drawer-open]`;
                const ratingForm = '[data-drawer-body] form[hx-post="/api/product/rating/add"]';
                const ratingSubmit = `${ratingForm} button[type=submit]`;
                const ratingURL = `${origin}/api/product/rating/add`;
                await page.$eval(`${editForm} [name=name]`, node => { node.value = '评分操作期间未保存的主体'; });
                if (width === 375) await page.tap(ratingOpen);
                else await page.click(ratingOpen);
                await page.waitForSelector(ratingSubmit, { visible: true });
                await page.$eval(ratingForm, form => {
                    form.noValidate = true;
                    form.elements.score.value = '9';
                });
                const invalidRating = page.waitForResponse(response => response.url() === ratingURL);
                await page.click(ratingSubmit);
                await page.waitForSelector(`${ratingSubmit}[disabled]`);
                assert.equal((await invalidRating).status(), 400);
                await page.waitForSelector(`${ratingForm} [data-write-status] [data-ui-error]`);
                await page.waitForSelector(`${ratingSubmit}:not([disabled])`);
                assert.equal(await page.$eval(`${ratingForm} [name=score]`, node => node.value), '9');
                assert.equal(await page.$eval('[data-drawer]', node => node.hidden), false);
                assert.match(await page.$eval(ratingPanel, node => node.textContent), /这个商品还没有评分/);
                assert.equal(await page.$eval(`${editForm} [data-write-status]`, node => node.hidden), true);

                stage = '评分零分新增、重复提交与局部交换';
                const beforeRating = writes.length;
                await page.$eval(ratingForm, form => { form.noValidate = false; form.elements.score.value = '0'; });
                const zeroRating = page.waitForResponse(response => response.url() === ratingURL);
                await page.click(ratingSubmit);
                await page.waitForSelector(`${ratingSubmit}[disabled]`);
                await page.$eval(ratingForm, form => form.requestSubmit(form.querySelector('button[type=submit]')));
                assert.equal((await zeroRating).status(), 200);
                await page.waitForSelector('[data-drawer][hidden]');
                await page.waitForSelector(`${ratingPanel} [data-product-rating="0"]`);
                assert.equal(writes.length - beforeRating, 1, '评分重复提交必须丢弃');
                const ratingRequest = writes.at(-1);
                assert.match(ratingRequest.headers['content-type'], /^application\/x-www-form-urlencoded/);
                assert.equal(new URLSearchParams(ratingRequest.body).get('score'), '0');
                assert.ok(ratingRequest.headers['x-csrf-token']);
                assert.equal(page.url(), editLocation);

                stage = '重新打开交换后的评分抽屉';
                await page.click(ratingOpen);
                await page.waitForSelector(ratingSubmit, { visible: true });
                await page.type(`${ratingForm} [name=score]`, '4.5');
                const secondRating = page.waitForResponse(response => response.url() === ratingURL);
                // 键盘提交与鼠标、触摸共用同一原生表单链。
                await page.keyboard.press('Enter');
                assert.equal((await secondRating).status(), 200);
                await page.waitForSelector('[data-drawer][hidden]');
                await page.waitForSelector(`${ratingPanel} [data-product-rating="2.25"]`);
                assert.equal(await page.$$eval(`${ratingPanel} tbody tr`, rows => rows.length), 2);

                stage = '评分删除取消、失败恢复与删除后空态';
                const ratingDelete = `${ratingPanel} form[hx-post="/api/product/rating/delete"]`;
                const ratingDeleteButton = `${ratingDelete} button[type=submit]`;
                const clickRatingDelete = async () => {
                    // 交换后的控件先完成共享扫描，再滚动并以真实指针命中提交。
                    await page.waitForSelector(`${ratingDeleteButton}:not([hidden]):not([disabled])`, { visible: true });
                    await page.$eval(ratingDeleteButton, node => node.scrollIntoView({ block: 'center', inline: 'nearest', behavior: 'instant' }));
                    const hit = await page.$eval(ratingDeleteButton, node => {
                        const box = node.getBoundingClientRect();
                        const target = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
                        return { matched: node.contains(target), button: { x: box.x, y: box.y, width: box.width, height: box.height }, target: target && target.outerHTML.slice(0, 500) };
                    });
                    assert.equal(hit.matched, true, `评分删除按钮滚动后必须可真实点击：${JSON.stringify(hit)}`);
                    await page.click(ratingDeleteButton);
                };
                const beforeRatingCancel = writes.length;
                page.once('dialog', dialog => dialog.dismiss());
                await clickRatingDelete();
                assert.equal(writes.length, beforeRatingCancel);
                const firstRatingID = await page.$eval(`${ratingDelete} [name=id]`, node => node.value);
                await page.$eval(`${ratingDelete} [name=id]`, node => { node.value = '11111111-1111-4111-8111-111111111111'; });
                page.once('dialog', dialog => dialog.accept());
                const badRatingDelete = page.waitForResponse(response => response.url() === `${origin}/api/product/rating/delete`);
                await clickRatingDelete();
                assert.equal((await badRatingDelete).status(), 404);
                await page.waitForSelector(`${ratingPanel} > [data-write-status] [data-ui-error]`);
                await page.waitForSelector(`${ratingDeleteButton}:not([disabled])`);
                assert.equal(await page.$$eval(`${ratingPanel} tbody tr`, rows => rows.length), 2);
                await page.$eval(`${ratingDelete} [name=id]`, (node, id) => { node.value = id; }, firstRatingID);
                for (const remaining of [1, 0]) {
                    page.once('dialog', dialog => dialog.accept());
                    const ratingDeleted = page.waitForResponse(response => response.url() === `${origin}/api/product/rating/delete`);
                    await clickRatingDelete();
                    await page.waitForSelector(`${ratingDeleteButton}[disabled]`);
                    assert.equal((await ratingDeleted).status(), 200);
                    await page.waitForFunction((selector, count) => document.querySelectorAll(`${selector} form[hx-post="/api/product/rating/delete"]`).length === count, {}, ratingPanel, remaining);
                }
                assert.match(await page.$eval(ratingPanel, node => node.textContent), /这个商品还没有评分/);
                assert.equal(page.url(), editLocation);
                assert.equal(await page.evaluate(() => {
                    const old = window.editNodes;
                    return old.form === document.querySelector('#product-edit-main') && old.editor === document.querySelector('trix-editor') &&
                        old.variant === document.querySelector('[data-variant-sku]') && old.gallery === document.querySelector('[data-media-gallery]');
                }), true, '评分增删不得重载或替换主体、富文本、图库与变体');
                assert.equal(await page.$eval(`${editForm} [name=name]`, node => node.value), '评分操作期间未保存的主体');
                assert.equal(await page.$eval('[data-variant-sku]', node => node.value), `未保存变体-${width}`);
                assert.match(await page.$eval(`${editForm} [name=descriptionHtml]`, node => node.value), new RegExp(`浏览器富文本-${width}`));

                stage = '列表单删确认与失败恢复';
                await page.goto(`${origin}/admin/products?project=${project}&keyword=browser-single-${width}`, { waitUntil: 'networkidle0' });
                const button = 'button[form^="product-del-"]';
                await page.waitForSelector(`${button}:not([hidden])`);
                const deleteFormID = await page.$eval(button, node => node.getAttribute('form'));
                const deleteForm = `#${deleteFormID}`;
                const realID = await page.$eval(`${deleteForm} [name=id]`, node => node.value);
                // 取消确认不发请求；外部关联按钮不得误触发表格里的批量 form。
                const beforeCancel = writes.length;
                page.once('dialog', dialog => dialog.dismiss());
                await page.click(button);
                assert.equal(writes.length, beforeCancel);
                await page.$eval(`${deleteForm} [name=id]`, node => { node.value = '11111111-1111-4111-8111-111111111111'; });
                page.once('dialog', dialog => dialog.accept());
                const rejectedDelete = page.waitForResponse(response => response.url() === `${origin}/api/product/delete`);
                await page.click(button);
                assert.equal((await rejectedDelete).status(), 404);
                await page.waitForFunction(id => !document.querySelector(`#${id}`).hasAttribute('aria-busy'), {}, deleteFormID);
                assert.equal(await page.$eval(button, node => node.disabled), false);
                assert.equal(await page.$eval(`${deleteForm} [name=id]`, node => node.value), '11111111-1111-4111-8111-111111111111');
                assert.equal(await page.$$eval('[data-write-host] > [data-write-status]:not([hidden]) [data-ui-error]', nodes => nodes.length), 1);
                assert.equal(await page.$eval('[data-list-bulk] [data-write-status]', node => node.hidden), true);
                await page.$eval(`${deleteForm} [name=id]`, (node, id) => { node.value = id; }, realID);
                // 服务端延迟通过真实中间件施加，浏览器观察在途禁用状态。
                await page.setExtraHTTPHeaders({ 'Accept-Language': 'zh-CN', 'X-Product-Test-Delay': '1' });
                page.once('dialog', dialog => dialog.accept());
                const deletedResponse = page.waitForResponse(response => response.url() === `${origin}/api/product/delete`);
                const reloaded = page.waitForNavigation({ waitUntil: 'networkidle0' });
                await page.click(button);
                await page.waitForSelector(`${button}[disabled]`);
                assert.equal((await deletedResponse).status(), 200);
                await reloaded;
                assert.equal(await page.$(button), null);

                stage = '批量拒绝保留选择与成功重载';
                await page.goto(`${origin}/admin/products?project=${project}&keyword=browser-bulk-`, { waitUntil: 'networkidle0' });
                const checks = await page.$$eval('[data-check-item]', (nodes, width) => nodes.filter(node => node.closest('tr').textContent.includes(`-${width}`)).map(node => node.value), width);
                assert.equal(checks.length, 2);
                for (const id of checks) await page.click(`[data-check-item][value="${id}"]`);
                await page.$eval(`[data-check-item][value="${checks[1]}"]`, node => { node.value = '11111111-1111-4111-8111-111111111111'; });
                const bulkButton = '[data-list-bulk] [data-bulk-bar] button[type=submit]';
                page.once('dialog', dialog => dialog.accept());
                const rejectedBulk = page.waitForResponse(response => response.url() === `${origin}/api/product/bulk-delete`);
                await page.click(bulkButton);
                assert.equal((await rejectedBulk).status(), 409);
                // 共享失败模板必须显示本次拒绝的逐条事实，HTML 不存在另一份未发送的 JSON 明细。
                await page.waitForSelector('[data-list-bulk] [data-write-status] [data-ui-error]');
                assert.match(await page.$eval('[data-list-bulk] [data-write-status]', node => node.textContent), /批量操作被拒绝/);
                assert.equal(await page.$$eval('[data-list-bulk] [data-ui-error] li', nodes => nodes.length), 1);
                assert.match(await page.$eval('[data-list-bulk] [data-ui-error] li', node => node.textContent), /11111111-1111-4111-8111-111111111111/);
                assert.equal(await page.$$eval('[data-check-item]:checked', nodes => nodes.length), 2);
                assert.equal(await page.$$eval('[data-write-host] > [data-write-status]:not([hidden])', nodes => nodes.length), 0);
                await page.$eval('[data-check-item][value="11111111-1111-4111-8111-111111111111"]', (node, id) => { node.value = id; }, checks[1]);
                page.once('dialog', dialog => dialog.accept());
                const bulkResponse = page.waitForResponse(response => response.url() === `${origin}/api/product/bulk-delete`);
                const bulkReload = page.waitForNavigation({ waitUntil: 'networkidle0' });
                await page.click(bulkButton);
                assert.equal((await bulkResponse).status(), 200);
                await bulkReload;
                for (const id of checks) assert.equal(await page.$(`[data-check-item][value="${id}"]`), null);
                assert.deepEqual(errors, []);
                stage = '全部完成';
            });
        }
        for (const [width, height] of [[1440, 900], [768, 1024], [375, 812]]) {
            for (const input of ['mouse', 'wheel', 'touch', 'keyboard']) {
                await t.test(`生产商品 ${width}px · ${input}：原生表单提交与冲突保留`, async t => {
                    const page = await browser.newPage();
                    t.after(() => page.close());
                    page.setDefaultTimeout(6000);
                    await page.setViewport({ width, height, hasTouch: true });
                    await page.setExtraHTTPHeaders({ 'Accept-Language': 'zh-CN' });
                    const errors = [];
                    page.on('pageerror', error => errors.push(error.message));
                    await page.goto(`${origin}/admin/products/new?project=${project}`, { waitUntil: 'networkidle0' });
                    const form = '#product-create-form';
                    const name = form + ' [name=name]';
                    const slug = form + ' [name=slug]';
                    const submit = form + ' button[type=submit]';
                    await page.waitForSelector(form + '[data-sku-enhanced="1"]');
                    assert.ok(await page.$('.admin-layout'));
                    assert.ok(await page.$('link[href="/static/css/theme.css"]'));
                    if (input === 'keyboard') await tabTo(page, name);
                    else await page.click(name);
                    const value = `matrix-${width}-${input}`;
                    await page.keyboard.type(value);
                    if (input === 'keyboard') await tabTo(page, slug);
                    else await page.click(slug);
                    await page.keyboard.press('Home');
                    await page.keyboard.down('Shift');
                    await page.keyboard.press('End');
                    await page.keyboard.up('Shift');
                    await page.keyboard.type('browser-conflict');
                    const rejected = page.waitForResponse(response => response.url() === origin + '/api/product/create');
                    await activate(page, submit, input, t);
                    const response = await rejected;
                    assert.equal(response.status(), 409);
                    const sent = new URLSearchParams(response.request().postData());
                    assert.equal(sent.get('name'), value);
                    assert.equal(sent.get('slug'), 'browser-conflict');
                    assert.ok(response.request().headers()['x-csrf-token']);
                    await page.waitForSelector(form + ' [data-write-status] [data-ui-error]');
                    assert.equal(await page.$eval(name, node => node.value), value);
                    assert.equal(new URL(page.url()).pathname, '/admin/products/new');
                    await page.waitForSelector(submit + ':not([disabled])');
                    assert.deepEqual(errors, []);
                });
            }
        }
    } finally { await browser.close(); }
});
