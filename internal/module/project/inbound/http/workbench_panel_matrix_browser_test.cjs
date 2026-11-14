// 由 workbench_panel_matrix_browser_test.go 调用（设置 GOWP_WORKBENCH_PANEL_BASE_URL）。
//
// 面板输入矩阵：三视口（1440 / 768 / 375）× 四种输入（鼠标 / 滚轮 / 触摸 / 键盘）。
//
// 为什么单独一份：面板契约（保存入口、成功监听、密码回填、状态单源）已由
// workbench_panel_flow_browser_test.cjs 在桌面视口证明；这里只回答**输入方式**
// 那一维 —— 换视口、换输入设备后，同一个动作是否仍然落到同一个请求/同一个结果。
//
// 滚轮路径此前一直没有有效证据：不是「滚轮不能操作」，而是没人证明过
// 「滚到目标再操作」这条路径不会因为面板滚动容器/固定定位而失效。
const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require('/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core');

const BASE = process.env.GOWP_WORKBENCH_PANEL_BASE_URL;

const VIEWPORTS = [
    { name: '1440 桌面', width: 1440, height: 900, touch: false },
    { name: '768 平板', width: 768, height: 1024, touch: true },
    { name: '375 手机', width: 375, height: 812, touch: true },
];

const INPUTS = ['mouse', 'wheel', 'touch', 'keyboard'];

async function open(browser, vp) {
    const page = await browser.newPage();
    await page.setViewport({
        width: vp.width, height: vp.height,
        hasTouch: vp.touch, isMobile: vp.touch,
    });
    const sent = [];
    const errors = [];
    page.on('request', (req) => {
        if (req.method() !== 'POST') return;
        sent.push({ path: new URL(req.url()).pathname, body: req.postData() || '' });
    });
    page.on('console', (msg) => { if (msg.type() === 'error') errors.push(msg.text()); });
    page.on('pageerror', (err) => errors.push(String(err)));
    await page.goto(BASE + '/');
    await page.waitForFunction(() => window.__flowReady === true, { timeout: 10000 });
    page.sent = sent;
    page.errors = errors;
    return page;
}

// scrollIntoViewIfNeeded 用真实滚动把目标带进视口：
// 窄视口下面板在折叠/滚动容器里，点击不可见元素会被 Puppeteer 判为不可点。
async function reveal(page, selector) {
    await page.waitForFunction((s) => {
        const el = document.querySelector(s);
        if (!el) return false;
        el.scrollIntoView({ block: 'center', inline: 'center' });
        return true;
    }, { timeout: 6000 }, selector);
    await new Promise((r) => setTimeout(r, 80));
}

async function scrollBy(page, selector, deltaY) {
    const box = await page.evaluate((s) => {
        const el = document.querySelector(s);
        if (!el) return null;
        const r = el.getBoundingClientRect();
        return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
    }, selector);
    if (!box) throw new Error('找不到滚动落点：' + selector);
    await page.mouse.move(box.x, box.y);
    await page.mouse.wheel({ deltaY: deltaY || 240 });
    await new Promise((r) => setTimeout(r, 120));
}

async function tap(page, selector) {
    // 触摸设备用 touchscreen.tap；坐标取自元素中心。
    const box = await page.evaluate((s) => {
        const el = document.querySelector(s);
        if (!el) return null;
        const r = el.getBoundingClientRect();
        return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
    }, selector);
    if (!box) throw new Error('找不到触摸目标：' + selector);
    await page.touchscreen.tap(box.x, box.y);
}

// tapToFocus 在触摸设备上把焦点真正落到目标上。
//
// 触摸事件不保证点中元素本身：窄视口里面板可能被顶栏/固定层盖住，
// 或目标只露出一部分，点中心会命中别的元素。这里按真实触摸坐标点，
// 点完核对 document.activeElement；没命中就换「中心点」重试一次。
// 仍不成立时把命中元素与坐标带进错误，便于判断是布局遮挡还是测试写错。
async function tapToFocus(page, selector) {
    for (let attempt = 0; attempt < 3; attempt++) {
        const probe = await page.evaluate((s, center) => {
            const el = document.querySelector(s);
            if (!el) return null;
            const r = el.getBoundingClientRect();
            const x = r.left + r.width / 2;
            const y = center ? r.top + r.height / 2 : r.top + Math.min(r.height / 2, 12);
            const hit = document.elementFromPoint(x, y);
            return {
                x, y,
                inside: !!hit && (hit === el || el.contains(hit)),
                hit: hit ? (hit.tagName + (hit.id ? '#' + hit.id : '') + (hit.className ? '.' + String(hit.className).split(' ')[0] : '')) : 'none',
            };
        }, selector, attempt > 0);
        if (!probe) throw new Error('找不到触摸目标：' + selector);
        await page.touchscreen.tap(probe.x, probe.y);
        const focused = await page.evaluate((s) => {
            const el = document.querySelector(s);
            return !!el && (document.activeElement === el || el.contains(document.activeElement));
        }, selector);
        if (focused) return true;
        if (!probe.inside) continue;
        // 命中在元素内但没拿到焦点（例如点到了内边距/只读层）：再点一次中心。
    }
    const detail = await page.evaluate((s) => {
        const el = document.querySelector(s);
        const r = el ? el.getBoundingClientRect() : null;
        return { rect: r ? { left: r.left, top: r.top, w: r.width, h: r.height } : null,
                 active: document.activeElement ? document.activeElement.tagName : 'none',
                 scroll: (el && el.closest('.wb-inspector-body')) ? el.closest('.wb-inspector-body').scrollTop : -1 };
    }, selector);
    throw new Error('触摸没有把焦点落到 ' + selector + '：' + JSON.stringify(detail));
}

async function pressEnterOn(page, selector) {
    await page.focus(selector);
    await page.keyboard.press('Enter');
}

async function waitSave(page) {
    for (let i = 0; i < 80; i++) {
        const hit = page.sent.filter((r) => r.path === '/admin/themes/settings/save').at(-1);
        if (hit) return hit;
        await new Promise((r) => setTimeout(r, 100));
    }
    throw new Error('保存请求没有发出；页面错误：' + page.errors.join(' | '));
}

// 驱动一次「打开全局面板并保存」的完整动作，输入方式决定怎么落到按钮上。
async function drive(page, input) {
    await page.evaluate(() => window.__wb.renderGlobalPanelHtmx());
    await reveal(page, '#wb-global-save-btn');

    // 有滚动容器的视口先滚一段再操作：这正是「滚轮路径」要证明的那一步。
    if (input === 'wheel') {
        await scrollBy(page, '#wb-global-content', 240);
        await scrollBy(page, '#wb-global-content', -120);
        await reveal(page, '#wb-global-save-btn');
    }

    const btn = '#wb-global-save-btn';
    switch (input) {
        case 'mouse':
            await page.click(btn);
            break;
        case 'wheel':
            await page.click(btn);
            break;
        case 'touch':
            await tap(page, btn);
            break;
        case 'keyboard':
            await pressEnterOn(page, btn);
            break;
        default:
            throw new Error('未知输入方式：' + input);
    }
    return waitSave(page);
}

// 仅用原生 Tab 移动焦点，不通过 focus() 或 tabindex 注入跳过生产顺序。
async function tabTo(page, selector, backwards = false) {
    for (let count = 1; count <= 180; count++) {
        if (backwards) await page.keyboard.down('Shift');
        await page.keyboard.press('Tab');
        if (backwards) await page.keyboard.up('Shift');
        if (await page.evaluate((s) => document.activeElement?.matches(s), selector)) return count;
    }
    throw new Error('原生 Tab 无法到达：' + selector);
}

async function visibleInPanel(page, selector) {
    return page.evaluate((s) => {
        const el = document.querySelector(s);
        const panel = el?.closest('.wb-inspector-body');
        if (!el || !panel) return false;
        const r = el.getBoundingClientRect(), p = panel.getBoundingClientRect();
        return r.top >= p.top && r.bottom <= p.bottom && r.left >= 0 && r.right <= innerWidth;
    }, selector);
}

async function scrollPanel(page, panelSelector, selector, input) {
    const before = await page.$eval(panelSelector, (el) => ({ top: el.scrollTop, max: el.scrollHeight - el.clientHeight }));
    assert.ok(before.max > 0, '生产面板必须实际可滚动，不能用空壳代替');
    const client = input === 'touch' ? await page.createCDPSession() : null;
    try {
        for (let i = 0; i < 20 && !(await visibleInPanel(page, selector)); i++) {
            const point = await page.$eval(panelSelector, (el) => {
                const r = el.getBoundingClientRect();
                return { x: r.left + r.width / 2, y: r.top + r.height * 0.75, dy: Math.min(240, r.height / 2) };
            });
            if (client) {
                await client.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: point.x, y: point.y }] });
                for (let j = 1; j <= 6; j++) {
                    await client.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: point.x, y: point.y - point.dy * j / 6 }] });
                    await new Promise((r) => setTimeout(r, 25));
                }
                await client.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
            } else {
                await page.mouse.move(point.x, point.y);
                await page.mouse.wheel({ deltaY: point.dy });
            }
            await new Promise((r) => setTimeout(r, 180));
        }
    } finally {
        if (client) await client.detach();
    }
    const after = await page.$eval(panelSelector, (el) => el.scrollTop);
    assert.ok(after > before.top, `${input} 必须改变真实面板 scrollTop：${before.top} -> ${after}`);
    assert.ok(await visibleInPanel(page, selector), '必须靠实际滚动找到保存按钮');
    return { before: before.top, after };
}

test('生产工作台布局：三视口 × 四输入，实际滚动与 Tab 可达', async (t) => {
    const browser = await puppeteer.launch({ executablePath: '/usr/bin/google-chrome', headless: true,
        args: ['--no-sandbox', '--disable-gpu'] });
    try {
        for (const vp of VIEWPORTS) for (const input of INPUTS) {
            await t.test(`${vp.name} · ${input}`, async () => {
                const page = await browser.newPage();
                page.setDefaultTimeout(6000);
                const errors = [];
                page.on('pageerror', (err) => errors.push(String(err)));
                try {
                    await page.setViewport({ width: vp.width, height: vp.height, hasTouch: true, isMobile: vp.touch });
                    await page.goto(BASE + '/production');
                    await page.waitForFunction(() => !!window.__wb?.meta);
                    assert.equal(await page.$eval('body', (el) => el.classList.contains('wb-body')), true);
                    assert.equal(await page.evaluate(() => Array.from(document.styleSheets).some((s) => /\/css\/editor\.css/.test(s.href || ''))), true);
                    const menu = '.wb-document-menu summary';
                    const opener = '[data-wb-view="global"]';
                    if (input === 'keyboard') {
                        await tabTo(page, menu, true);
                        await page.keyboard.press('Enter');
                        await tabTo(page, opener);
                        await page.keyboard.press('Enter');
                    } else if (input === 'touch') {
                        await tap(page, menu);
                        await tap(page, opener);
                    } else {
                        await page.click(menu);
                        await page.click(opener);
                    }
                    await page.waitForSelector('#wb-global-save .wb-color-text');
                    const btn = '#wb-global-save-btn';
                    if (input === 'keyboard') {
                        const tabs = await tabTo(page, btn);
                        assert.ok(tabs > 1, '必须穿过真实面板控件顺序');
                        assert.ok(await visibleInPanel(page, btn), 'Tab 到达时保存按钮应自动滚入真实面板');
                        t.diagnostic(`${vp.width} keyboard: ${tabs} 次 Tab 到达保存按钮`);
                    }
                    if (input === 'wheel' || input === 'touch') {
                        const scroll = await scrollPanel(page, '#wb-global-body', btn, input);
                        t.diagnostic(`${vp.width} ${input}: scrollTop ${scroll.before} -> ${scroll.after}`);
                    }
                    const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/admin/themes/settings/save');
                    if (input === 'keyboard') await page.keyboard.press('Enter');
                    else if (input === 'touch') await tap(page, btn);
                    else await page.click(btn);
                    const saved = await response;
                    assert.equal(saved.status(), 200);
                    assert.equal(new URLSearchParams(saved.request().postData()).get('colors.primary'), '#111111');
                    await page.waitForFunction(() => window.__wb.doc.settings?.theme?.colors?.primary === '#111111');
                    assert.match(await page.$eval('#wb-global-save-result', (el) => el.textContent), /主题已保存/);
                    assert.deepEqual(errors, [], '生产应用入口不得有脚本异常');
                } finally { await page.close(); }
            });
        }
    } finally { await browser.close(); }
});

// 生产布局里的密码业务链：三视口 × 四输入。
//
// 与下面简化外壳的密码矩阵分工：
//   · 简化外壳证明「输入方式落到同一请求」这一维，不含生产布局；
//   · 这一条必须走真实 workbench/layout、生产 CSS 与生产应用模块，
//     滚动读的是 #wb-settings-body 的真实 scrollTop，键盘只用原生 Tab，
//     触摸用 CDP 触摸事件 —— 三者的可达性都要在生产布局里成立才算数。
test('生产工作台布局：三视口 × 四输入密码业务链', async (t) => {
    const PWD = 'p@ssw0rd-prod';
    const browser = await puppeteer.launch({ executablePath: '/usr/bin/google-chrome', headless: true,
        args: ['--no-sandbox', '--disable-gpu'] });
    try {
        for (const vp of VIEWPORTS) for (const input of INPUTS) {
            await t.test(`${vp.name} · 密码 · ${input}`, async () => {
                const page = await browser.newPage();
                page.setDefaultTimeout(8000);
                const errors = [];
                page.on('pageerror', (err) => errors.push(String(err)));
                const writes = [];
                page.on('request', (req) => {
                    if (req.method() !== 'POST') return;
                    if (new URL(req.url()).pathname === '/workbench/settings') writes.push(req.postData() || '');
                });
                try {
                    await page.setViewport({ width: vp.width, height: vp.height, hasTouch: true, isMobile: vp.touch });
                    await page.goto(BASE + '/production');
                    await page.waitForFunction(() => !!window.__wb?.meta);

                    // 打开「页面设置」（与主题保存同一条生产菜单路径）。
                    const menu = '.wb-document-menu summary';
                    const opener = '[data-wb-view="settings"]';
                    if (input === 'keyboard') {
                        await tabTo(page, menu, true);
                        await page.keyboard.press('Enter');
                        await tabTo(page, opener);
                        await page.keyboard.press('Enter');
                    } else if (input === 'touch') {
                        await tap(page, menu);
                        await tap(page, opener);
                    } else {
                        await page.click(menu);
                        await page.click(opener);
                    }
                    const field = '#wb-settings-content [name="wb-access-password"]';
                    const apply = '#wb-settings-content [data-wb-access-set]';
                    await page.waitForSelector(field, { timeout: 10000 });

                    // 输入明文：每一种输入方式都必须先真正到达字段。
                    if (input === 'keyboard') {
                        const tabs = await tabTo(page, field);
                        assert.ok(tabs >= 1, '必须用原生 Tab 到达密码输入框');
                        t.diagnostic(`${vp.width} 密码 keyboard: ${tabs} 次 Tab 到达输入框`);
                    } else if (input === 'touch') {
                        if (!(await visibleInPanel(page, field))) {
                            const scroll = await scrollPanel(page, '#wb-settings-body', field, 'touch');
                            t.diagnostic(`${vp.width} 密码 touch: scrollTop ${scroll.before} -> ${scroll.after}`);
                        }
                        await tapToFocus(page, field);
                    } else {
                        if (input === 'wheel' && !(await visibleInPanel(page, field))) {
                            const scroll = await scrollPanel(page, '#wb-settings-body', field, 'wheel');
                            t.diagnostic(`${vp.width} 密码 wheel: scrollTop ${scroll.before} -> ${scroll.after}`);
                        }
                        await page.click(field);
                    }
                    await page.keyboard.type(PWD);
                    assert.equal(await page.$eval(field, (el) => el.value), PWD);

                    // 提交：键盘必须穿过真实控件顺序，触摸/滚轮先确保按钮在面板内可见。
                    const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/workbench/settings');
                    if (input === 'keyboard') {
                        const tabs = await tabTo(page, apply);
                        assert.ok(tabs >= 1, '必须用原生 Tab 到达「设置」按钮');
                        assert.ok(await visibleInPanel(page, apply), 'Tab 到达时按钮应自动滚入真实面板');
                        await page.keyboard.press('Enter');
                    } else if (input === 'touch') {
                        if (!(await visibleInPanel(page, apply))) await scrollPanel(page, '#wb-settings-body', apply, 'touch');
                        await tap(page, apply);
                    } else {
                        await page.click(apply);
                    }
                    const res = await response;
                    assert.equal(res.status(), 200, `密码提交应 200，实际 ${res.status()}`);

                    const body = JSON.parse(res.request().postData() || '{}');
                    assert.equal(body.accessPassword, PWD, '提交的明文必须来自当前输入');
                    assert.ok(body.document && typeof body.document === 'object', '同一跳要带整份文档对象');
                    assert.ok(writes.length >= 1, '必须观察到真实的 /workbench/settings 请求');

                    // 服务端 bcrypt 回填：哈希进文档、文档变脏、明文不落文档。
                    await page.waitForFunction(
                        () => (window.__wb.doc.settings?.access?.passwordHash || '').startsWith('$2'),
                        { timeout: 10000 },
                    ).catch((e) => { throw new Error('哈希没有回填进文档：' + e.message); });
                    const state = await page.evaluate(() => ({
                        hash: window.__wb.doc.settings?.access?.passwordHash || '',
                        type: window.__wb.doc.settings?.access?.type || '',
                        saveState: window.__wb.saveState,
                        doc: JSON.stringify(window.__wb.doc),
                    }));
                    assert.match(state.hash, /^\$2[aby]\$/, '文档里的必须是 bcrypt 哈希');
                    assert.equal(state.type, 'password');
                    assert.equal(state.saveState, 'dirty', '哈希进文档后应标记为未保存');
                    assert.ok(!state.doc.includes(PWD), '明文不得进入文档');
                    assert.deepEqual(errors, [], '生产应用入口不得有脚本异常');
                } finally { await page.close(); }
            });
        }
    } finally { await browser.close(); }
});

test('面板输入矩阵：三视口 × 四种输入', async (t) => {
    const browser = await puppeteer.launch({
        executablePath: '/usr/bin/google-chrome', headless: true,
        args: ['--no-sandbox', '--disable-gpu'],
    });
    try {
        for (const vp of VIEWPORTS) {
            for (const input of INPUTS) {
                await t.test(`${vp.name} · ${input}`, async () => {
                    const page = await open(browser, vp);
                    try {
                        const saved = await drive(page, input);
                        const values = new URLSearchParams(saved.body);
                        assert.equal(values.get('id'), 'theme-flow-1', '保存入口必须在所有视口/输入下都可用');
                        assert.equal(values.get('colors.primary'), '#111111', '提交值不能被输入方式改变');
                        // 成功回执落进结果区：这是「保存真的完成」的可见证据。
                        await page.waitForFunction(
                            () => /主题已保存/.test(document.querySelector('#wb-global-save-result')?.textContent || ''),
                            { timeout: 6000 },
                        );
                    } finally {
                        await page.close();
                    }
                });
            }
        }

        // 密码按钮生产矩阵：三视口 × 四输入，完整走「输入明文 → 提交 → 服务端 bcrypt
        // 回填哈希 → 文档变脏 → 明文不落文档」这一条链。
        //
        // 此前的窄视口专项只断言按钮有可点面积，不证明真的能设密码；这里每一种
        // 输入方式都必须走完同一条链，判据是文档里的 passwordHash 以 $2 开头。
        for (const vp of VIEWPORTS) {
            for (const input of INPUTS) {
                await t.test(`${vp.name} · 密码按钮 · ${input}`, async () => {
                    const page = await open(browser, vp);
                    try {
                        await page.evaluate(() => window.__wb.renderSettingsPanelHtmx());
                        await page.waitForSelector('#wb-settings-content [name="wb-access-password"]', { timeout: 8000 });
                        await reveal(page, '#wb-settings-content [name="wb-access-password"]');

                        const field = '#wb-settings-content [name="wb-access-password"]';
                        const apply = '#wb-settings-content [data-wb-access-set]';

                        // 输入明文：按输入方式落到字段上。
                        if (input === 'keyboard') {
                            await page.focus(field);
                            await page.keyboard.type('p@ssw0rd-matrix');
                        } else if (input === 'touch') {
                            await tap(page, field);
                            await page.keyboard.type('p@ssw0rd-matrix');
                        } else {
                            if (input === 'wheel') {
                                await scrollBy(page, '#wb-settings-content', 240);
                                await reveal(page, field);
                            }
                            await page.click(field);
                            await page.keyboard.type('p@ssw0rd-matrix');
                        }
                        assert.equal(await page.$eval(field, (el) => el.value), 'p@ssw0rd-matrix');

                        // 提交：按输入方式落到按钮上。
                        const saved = page.waitForResponse(
                            (r) => new URL(r.url()).pathname === '/workbench/settings');
                        switch (input) {
                            case 'keyboard':
                                await page.focus(apply);
                                await page.keyboard.press('Enter');
                                break;
                            case 'touch':
                                await tap(page, apply);
                                break;
                            default:
                                await page.click(apply);
                                break;
                        }
                        const res = await saved;
                        assert.equal(res.status(), 200, `密码提交应 200，实际 ${res.status()}`);
                        // 密码表单挂 workbench-settings 对象提供器：请求体是 JSON
                        // {document, accessPassword}，不是表单编码。
                        const body = JSON.parse(res.request().postData() || '{}');
                        assert.equal(body.accessPassword, 'p@ssw0rd-matrix',
                            '提交的明文必须来自当前输入（对象提供器从密码表单隐藏域读取）');
                        assert.ok(body.document && typeof body.document === 'object',
                            '同一跳要带整份文档对象');

                        // 服务端 bcrypt 回填：文档里的哈希以 $2 开头，且文档变脏。
                        await page.waitForFunction(
                            () => (window.__wb.doc.settings?.access?.passwordHash || '').startsWith('$2'),
                            { timeout: 8000 },
                        ).catch((e) => { throw new Error('哈希没有回填进文档：' + e.message); });
                        const state = await page.evaluate(() => ({
                            hash: window.__wb.doc.settings?.access?.passwordHash || '',
                            type: window.__wb.doc.settings?.access?.type || '',
                            saveState: window.__wb.saveState,
                            doc: JSON.stringify(window.__wb.doc),
                        }));
                        assert.match(state.hash, /^\$2[aby]\$/, '文档里的必须是 bcrypt 哈希');
                        assert.equal(state.type, 'password');
                        assert.equal(state.saveState, 'dirty', '哈希进文档后应标记为未保存');
                        // 明文绝不进文档（会随 page_revisions 落库）。
                        assert.ok(!state.doc.includes('p@ssw0rd-matrix'), '明文不得进入文档');
                        assert.ok(page.errors.length === 0, '页面错误：' + page.errors.join(' | '));
                    } finally {
                        await page.close();
                    }
                });
            }
        }

        // 窄视口专项：设置面板的「设置访问密码」按钮在 375 宽下也必须可点。
        for (const vp of VIEWPORTS) {
            await t.test(`${vp.name} · 设置面板密码按钮可操作`, async () => {
                const page = await open(browser, vp);
                try {
                    await page.evaluate(() => window.__wb.renderSettingsPanelHtmx());
                    await reveal(page, '#wb-settings-content [data-wb-access-set]');
                    const box = await page.evaluate(() => {
                        const el = document.querySelector('#wb-settings-content [data-wb-access-set]');
                        const r = el.getBoundingClientRect();
                        return { w: r.width, h: r.height, left: r.left, right: r.right, vw: window.innerWidth };
                    });
                    assert.ok(box.w > 0 && box.h > 0, '按钮必须有可点面积');
                    assert.ok(box.left >= 0 && box.right <= box.vw + 0.5,
                        `按钮不能越出视口：left=${box.left} right=${box.right} vw=${box.vw}`);
                } finally {
                    await page.close();
                }
            });
        }
    } finally {
        await browser.close();
    }
});
