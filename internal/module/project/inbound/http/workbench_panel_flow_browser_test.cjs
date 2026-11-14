// 由 workbench_panel_flow_browser_test.go 调用（设置 GOWP_WORKBENCH_PANEL_BASE_URL）。
//
// 跑的是真实片段（生产处理器 + Jet）与真实模块（editor-app 的 app-panels.js），
// 覆盖三个客户端断点：保存入口、成功监听、密码哈希回填、主题状态单源。
//
// 请求观测在 **Puppeteer 层**（page.on('request')）而不是页面里包 XMLHttpRequest：
// htmx 4 走 fetch，包 XHR 会一条都记不到，测试会以「请求没发出」的方式假红。
const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require('/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core');

const BASE = process.env.GOWP_WORKBENCH_PANEL_BASE_URL;

async function open(browser, width) {
    const page = await browser.newPage();
    await page.setViewport({ width: width || 1440, height: 900 });
    const sent = [];
    const errors = [];
    page.on('request', (req) => {
        if (req.method() !== 'POST') return;
        sent.push({
            path: new URL(req.url()).pathname,
            contentType: req.headers()['content-type'] || '',
            body: req.postData() || '',
        });
    });
    page.on('console', (msg) => { if (msg.type() === 'error') errors.push(msg.text()); });
    page.on('pageerror', (err) => errors.push(String(err)));
    await page.goto(BASE + '/');
    await page.waitForFunction(() => window.__flowReady === true, { timeout: 10000 });
    page.sent = sent;
    page.errors = errors;
    return page;
}

test('工作台面板：生产片段 + 生产模块的真实闭环', async (t) => {
    const browser = await puppeteer.launch({
        executablePath: '/usr/bin/google-chrome', headless: true,
        args: ['--no-sandbox', '--disable-gpu'],
    });
    try {
        await t.test('全局面板自带可提交控件', async () => {
            const page = await open(browser);
            await page.evaluate(() => window.__wb.renderGlobalPanelHtmx());
            await page.waitForFunction(() => document.querySelector('#wb-global-save'), { timeout: 6000 });
            const count = await page.evaluate(() => {
                const form = document.querySelector('#wb-global-save');
                return Array.from(form.querySelectorAll('button'))
                    .filter((b) => !b.disabled && (b.type === 'submit' || b.type === 'button')).length;
            });
            assert.ok(count > 0, '面板片段必须提供可提交控件；0 个等于用户没有保存入口');
            await page.close();
        });

        await t.test('全局保存：编码、绑定事实、成功合回、再打开提交新值', async () => {
            const page = await open(browser);
            await page.evaluate(() => window.__wb.renderGlobalPanelHtmx());
            await page.waitForFunction(() => document.querySelector('#wb-global-save input[type=hidden][name="colors.primary"]'), { timeout: 6000 });
            // 走真实取色器路径：改文本框并派发 change —— 控件会同时更新隐藏域。
            // 只改隐藏域是测试造出来的状态，commitGlobalSave 读的是控件当前值。
            await page.evaluate(() => {
                const slot = document.querySelector('#wb-global-save [data-wb-theme-color="colors.primary"]');
                const text = slot.querySelector('.wb-color-text');
                text.value = '#222222';
                text.dispatchEvent(new Event('change', { bubbles: true }));
            });
            await page.click('#wb-global-save-btn');
            const saved = await (async () => {
                for (let i = 0; i < 60; i++) {
                    const hit = page.sent.filter((r) => r.path === '/admin/themes/settings/save').at(-1);
                    if (hit) return hit;
                    await new Promise((r) => setTimeout(r, 100));
                }
                throw new Error('保存请求没有发出；页面错误：' + page.errors.join(' | '));
            })();
            assert.match(saved.contentType, /^application\/x-www-form-urlencoded/);
            const values = new URLSearchParams(saved.body);
            assert.equal(values.get('id'), 'theme-flow-1');
            assert.equal(values.get('headerBlockId'), 'blk-header', '面板不渲染的绑定事实必须随表单带回');
            assert.equal(values.get('colors.primary'), '#222222');

            await page.waitForFunction(() => (window.__wb.meta.themeSettings?.colors?.primary || '') === '#222222', { timeout: 6000 })
                .catch(() => { throw new Error('成功事件没有把提交值合回内存 meta；页面错误：' + page.errors.join(' | ')); });
            const after = await page.evaluate(() => ({
                docPrimary: window.__wb.doc.settings?.theme?.colors?.primary || '',
                notice: window.__wb.notice || '',
            }));
            assert.equal(after.docPrimary, '#222222', '文档也要合入主题');
            assert.match(after.notice, /主题已保存/);

            // 再打开面板：请求对象必须来自更新后的 meta，而不是页面加载时的数据岛。
            const before = page.sent.filter((r) => r.path === '/workbench/global').length;
            await page.evaluate(() => window.__wb.renderGlobalPanelHtmx());
            let load = null;
            for (let i = 0; i < 60; i++) {
                const hits = page.sent.filter((r) => r.path === '/workbench/global');
                if (hits.length > before) { load = JSON.parse(hits.at(-1).body); break; }
                await new Promise((r) => setTimeout(r, 100));
            }
            if (!load) throw new Error('第二次打开面板没有发出请求；页面错误：' + page.errors.join(' | '));
            assert.equal(load.settings?.colors?.primary, '#222222', '第二次打开提交的仍是旧值 = 两个状态源并存');
            await page.close();
        });

        await t.test('延迟保存只确认本次提交，保留并提示后续编辑', async () => {
            const page = await open(browser);
            try {
                await page.evaluate(() => window.__wb.renderGlobalPanelHtmx());
                await page.waitForSelector('#wb-global-save .wb-color-text');
                await page.setRequestInterception(true);
                let release;
                const held = new Promise((resolve) => { release = resolve; });
                let blocked = false;
                page.on('request', (req) => {
                    if (!blocked && new URL(req.url()).pathname === '/admin/themes/settings/save') {
                        blocked = true;
                        release(req);
                    } else req.continue();
                });
                const setColor = (value) => page.evaluate((color) => {
                    const input = document.querySelector('[data-wb-theme-color="colors.primary"] .wb-color-text');
                    input.value = color;
                    input.dispatchEvent(new Event('change', { bubbles: true }));
                }, value);
                await setColor('#222222');
                await page.click('#wb-global-save-btn');
                const req = await held;
                assert.equal(new URLSearchParams(req.postData()).get('colors.primary'), '#222222');
                await setColor('#333333');
                assert.equal(await page.$eval('#wb-global-save-btn', (el) => el.disabled), true);
                await req.continue();
                await page.waitForFunction(() => window.__wb.meta.themeSettings.colors.primary === '#222222');
                const state = await page.evaluate(() => ({
                    meta: window.__wb.meta.themeSettings.colors.primary,
                    doc: window.__wb.doc.settings.theme.colors.primary,
                    input: document.querySelector('[data-wb-theme-color="colors.primary"] .wb-color-text').value,
                    notice: window.__wb.notice,
                }));
                assert.equal(state.meta, '#222222');
                assert.equal(state.doc, '#222222');
                assert.equal(state.input, '#333333', '不能覆盖等待期间的新输入');
                assert.match(state.notice, /仍有未保存修改/, '不能把后续编辑冒认为已保存');
                await page.waitForFunction(() => !document.querySelector('#wb-global-save-btn').disabled);
                await page.click('#wb-global-save-btn');
                await page.waitForFunction(() => window.__wb.meta.themeSettings.colors.primary === '#333333');
                assert.equal(await page.evaluate(() => window.__wb.doc.settings.theme.colors.primary), '#333333');
                assert.match(await page.evaluate(() => window.__wb.notice), /^主题已保存/);
                assert.equal(new URLSearchParams(page.sent.filter((r) => r.path === '/admin/themes/settings/save').at(-1).body).get('colors.primary'), '#333333');
            } finally {
                await page.close();
            }
        });

        await t.test('设置访问密码：哈希回填进文档并产生快照', async () => {
            const page = await open(browser);
            await page.evaluate(() => window.__wb.renderSettingsPanelHtmx());
            await page.waitForFunction(() => document.querySelector('#wb-settings-content [name="wb-access-password"]'), { timeout: 6000 });
            await page.type('#wb-settings-content [name="wb-access-password"]', 'p@ssw0rd');
            await page.click('#wb-settings-content [data-wb-access-set]');
            await page.waitForFunction(() => (window.__wb.doc.settings?.access?.passwordHash || '').startsWith('$2'), { timeout: 6000 })
                .catch(() => { throw new Error('密码哈希没有回填进文档；页面错误：' + page.errors.join(' | ')); });

            const state = await page.evaluate(() => ({
                hash: window.__wb.doc.settings?.access?.passwordHash || '',
                type: window.__wb.doc.settings?.access?.type || '',
                snapshots: window.__snapshots || 0,
                saveState: window.__wb.saveState,
            }));
            assert.match(state.hash, /^\$2[aby]\$/);
            assert.equal(state.type, 'password');
            assert.ok(state.snapshots > 0, '回填哈希时必须产生快照（可撤销）');
            assert.equal(state.saveState, 'dirty', '哈希进文档后应标记为未保存');

            // 明文绝不进文档（会随 page_revisions 落库）。
            assert.ok(!JSON.stringify(await page.evaluate(() => window.__wb.doc)).includes('p@ssw0rd'));
            await page.close();
        });

        await t.test('name="id" 会遮蔽 form.id：成功监听必须读 id 属性', async () => {
            const page = await open(browser);
            await page.evaluate(() => window.__wb.renderGlobalPanelHtmx());
            await page.waitForFunction(() => document.querySelector('#wb-global-save'), { timeout: 6000 });
            const shadowed = await page.evaluate(() => {
                const form = document.querySelector('#wb-global-save');
                return {
                    propType: form.id && form.id.nodeName ? form.id.nodeName : typeof form.id,
                    attrID: form.getAttribute('id'),
                };
            });
            // 前提成立才说明「读 .id」这条路径是错的；前提变了本用例需重写。
            assert.equal(shadowed.propType, 'INPUT', '前提变了：name="id" 不再遮蔽 .id');
            assert.equal(shadowed.attrID, 'wb-global-save');
            await page.close();
        });
    } finally {
        await browser.close();
    }
});
