// 由 admin_drawer_session_browser_test.go 调用（设置 GOWP_HTML_UI_BASE_URL）。
//
// 只证明凭证链路：页面里的 csrf meta 来自真实 EnsureCSRFToken（写进会话），
// 会话 cookie 由真实 SaveCookieSession 写入；写请求带着它们到达 Controller。
// 抽屉的滚动/焦点/失败保留交互由 admin-drawers.cjs 覆盖，这里不重复。
const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require(process.env.GOWP_PUPPETEER_MODULE || 'puppeteer-core');

test('抽屉：真会话 cookie + 真 CSRF token', async (t) => {
    const origin = process.env.GOWP_HTML_UI_BASE_URL;
    assert.ok(origin, '必须由 Go 测试提供服务地址');

    const browser = await puppeteer.launch({
        executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome',
        headless: true,
        args: ['--no-sandbox', '--disable-gpu'],
    });
    const page = await browser.newPage();
    page.setDefaultTimeout(8000);
    const writes = [];
    const errors = [];
    page.on('console', (m) => { if (m.type() === 'error') errors.push('console: ' + m.text()); });
    page.on('requestfailed', (r) => errors.push('reqfail: ' + r.url() + ' ' + (r.failure()?.errorText || '')));
    page.on('pageerror', (err) => errors.push(String(err)));
    page.on('request', (req) => {
        if (req.method() !== 'POST') return;
        writes.push({
            path: new URL(req.url()).pathname,
            csrf: req.headers()['x-csrf-token'] || '',
            body: req.postData() || '',
        });
    });

    try {
        // 1. 先登录：真实 SaveCookieSession 写入会话 cookie。
        await page.goto(origin + '/login', { waitUntil: 'domcontentloaded' });
        const cookies = await page.cookies();
        assert.ok(cookies.some((c) => c.name === 'gowp_session' && c.value),
            '登录后必须有真实会话 cookie，实际：' + JSON.stringify(cookies.map((c) => c.name)));

        // 2. 打开页面：csrf meta 来自真实 EnsureCSRFToken。
        await page.goto(origin + '/', { waitUntil: 'networkidle0' });
        const token = await page.$eval('meta[name="csrf-token"]', (el) => el.content);
        assert.ok(token && token.length >= 16, '页面必须拿到真实 CSRF token，实际：' + JSON.stringify(token));
        assert.notEqual(token, 'browser-csrf', '不能是桩 token');

        // 3. 打开抽屉并发起真实写入。
        const hosts = await page.evaluate(() => ({
            htmx: typeof window.htmx,
            wbui: typeof window.WBUI,
            drawerHidden: document.querySelector('[data-drawer]')?.hidden,
            target: !!document.getElementById('admin-drawer-body'),
        }));
        let drawerStatus = 0;
        page.on('response', (r) => {
            if (new URL(r.url()).pathname === '/admin/menus/edit') drawerStatus = r.status();
        });
        await page.click('#menu');
        for (let i = 0; i < 80 && drawerStatus === 0; i++) await new Promise((r) => setTimeout(r, 100));
        assert.equal(drawerStatus, 200,
            '抽屉片段应由真实 Controller 返回 200，实际 ' + drawerStatus +
            ' | host=' + JSON.stringify(hosts) + ' | errors=' + JSON.stringify(errors));
        await page.waitForSelector('#admin-drawer-body [name=title]', { timeout: 8000 }).catch((e) => {
            throw new Error(e.message + ' | errors=' + JSON.stringify(errors) + ' | writes=' + JSON.stringify(writes));
        });
        await page.$eval('#admin-drawer-body [name=title]', (el) => { el.value = '菜单 375'; });

        const response = page.waitForResponse(
            (r) => new URL(r.url()).pathname === '/admin/menus/update');
        await page.click('#admin-drawer-body [type=submit]');
        const res = await response;
        assert.equal(res.status(), 200, '真实会话 + 真实 CSRF 下写入应成功');

        const write = writes.find((w) => w.path === '/admin/menus/update');
        assert.ok(write, '没有观察到写入请求');
        assert.equal(write.csrf, token, '请求头必须带页面上的同一个 token');
        assert.equal(new URLSearchParams(write.body).get('title'), '菜单 375');

        assert.deepEqual(errors, [], '页面不得有脚本异常');
    } finally {
        await page.close();
        await browser.close();
    }
});
