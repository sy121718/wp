// 由 workbench_theme_persist_browser_test.go 调用（设置 GOWP_WORKBENCH_PANEL_BASE_URL）。
//
// 只证明一件事：点一次「保存」，真实 SaveThemeSettings 收到请求并落库。
// 断言分两层——这里断请求真的发出且编码/字段正确；Go 侧断数据库回读与
// 页面刷新调用（响应本身不构成落库证据）。
const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require(process.env.GOWP_PUPPETEER_MODULE || '/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core');

const BASE = process.env.GOWP_WORKBENCH_PANEL_BASE_URL;

test('工作台主题保存：真实处理器 + 真实落库', async (t) => {
    const browser = await puppeteer.launch({
        executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome',
        headless: true,
        args: ['--no-sandbox', '--disable-gpu'],
    });
    const page = await browser.newPage();
    page.setDefaultTimeout(8000);
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
    page.on('pageerror', (err) => errors.push(String(err)));
    const notFound = [];
    page.on('request', (req) => { if (!/^https?:/.test(req.url())) notFound.push('SCHEME ' + req.url()); });
    page.on('response', (res) => { if (res.status() === 404) notFound.push(new URL(res.url()).pathname); });
    page.on('requestfailed', (req) => notFound.push('FAILED ' + new URL(req.url()).pathname));
    page.on('console', (msg) => { if (msg.type() === 'error') errors.push(msg.text() + ' @' + JSON.stringify(notFound)); });

    try {
        await page.goto(BASE + '/');
        await page.waitForFunction(() => window.__persistReady === true, { timeout: 10000 });

        await t.test('全局面板可渲染并提交', async () => {
            await page.evaluate(() => window.__wb.renderGlobalPanelHtmx());
            await page.waitForSelector('#wb-global-save input[type=hidden][name="colors.primary"]', { timeout: 8000 });

            await page.evaluate(() => {
                const slot = document.querySelector('#wb-global-save [data-wb-theme-color="colors.primary"]');
                const text = slot.querySelector('.wb-color-text');
                text.value = '#0f766e';
                text.dispatchEvent(new Event('change', { bubbles: true }));
            });
            await page.click('#wb-global-save-btn');

            let saved = null;
            for (let i = 0; i < 80; i++) {
                saved = sent.filter((r) => r.path === '/admin/themes/settings/save').at(-1) || null;
                if (saved) break;
                await new Promise((r) => setTimeout(r, 100));
            }
            if (!saved) throw new Error('保存请求没有发到真实处理器；页面错误：' + errors.join(' | '));

            assert.match(saved.contentType, /^application\/x-www-form-urlencoded/);
            const values = new URLSearchParams(saved.body);
            assert.equal(values.get('colors.primary'), '#0f766e', '提交值必须来自当前控件');
            assert.ok(values.get('id'), '主题 id 必须随表单提交');

            // 真实处理器返回成功回执（失败会走共享失败片段并写进状态行）。
            await page.waitForFunction(
                () => /已保存|主题/.test(document.querySelector('#wb-global-save-result')?.textContent || ''),
                { timeout: 8000 },
            ).catch(() => { throw new Error('保存后没有可见回执；页面错误：' + errors.join(' | ')); });
        });

        assert.deepEqual(errors, [], '生产应用入口不得有脚本异常');
    } finally {
        await page.close();
        await browser.close();
    }
});
