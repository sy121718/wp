// Run with: node --test internal/templates/workbench_panel_browser_test.cjs
//
// 工作台三块业务面板的浏览器契约：取数与保存都走共享写基座（HTMX 4 + write-base），
// 客户端只提供请求对象。这里用真实 Chrome 跑仓内 HTMX 与基座，不替换 fetch，
// 覆盖六种真实结局：成功、业务失败保留输入、非 HTML、整页响应、重定向、网络失败，
// 以及「新请求先返回、旧请求后返回」的迟到覆盖。
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const puppeteer = require('/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core');

const ROOT = path.resolve(__dirname, '../..');
const UI_SRC = path.join(ROOT, 'pkg/view/ui/assets/js/ui');
const APP_SRC = path.join(__dirname, 'static/js/editor-app');

// 与 workbench/layout.html 同形的三块面板外壳（少了它，测的就不是生产形状）。
const PANEL_HTML = `
<meta name="csrf-token" content="panel-token">
<script type="application/json" id="wb-meta">{"themeId":"theme-1","themeSettings":{"colors":{"primary":"#111"},"headerBlockId":"blk-h"},"draftPath":"/about"}</script>
<section data-write-host id="wb-settings-body">
  <p class="form-hint" data-write-status role="status" hidden></p>
  <form id="wb-settings-form" data-write hx-post="/workbench/settings"
        hx-trigger="wb:settings-panel from:body" hx-target="#wb-settings-content"
        hx-swap="innerMorph" hx-sync="this:replace" data-write-builder="workbench-settings">
    <div id="wb-settings-content"><p data-panel>original settings</p></div>
  </form>
  <form id="wb-seo-form" data-write hx-post="/workbench/seo-score-panel"
        hx-trigger="wb:seo-panel from:body" hx-target="#wb-seo-score"
        hx-swap="innerMorph" hx-sync="this:replace" data-write-builder="workbench-seo"></form>
</section>
<section data-write-host id="wb-global-body">
  <p class="form-hint" data-write-status role="status" hidden></p>
  <form id="wb-global-load" data-write hx-post="/workbench/global"
        hx-trigger="wb:global-panel from:body" hx-target="#wb-global-content"
        hx-swap="innerMorph" hx-sync="this:replace" data-write-builder="workbench-global"></form>
  <div id="wb-global-content">
    <form id="wb-global-save" data-write hx-post="/admin/themes/settings/save"
          hx-target="#wb-global-save-result" hx-swap="innerHTML"
          hx-sync="this:drop" hx-disable="findAll button:not(:disabled)" data-msg-failed="save failed">
      <input type="hidden" name="id" value="theme-1">
      <input type="hidden" name="headerBlockId" value="blk-h">
      <input type="hidden" name="colors.primary" value="#111">
      <div role="status" aria-live="polite" id="wb-global-save-result"></div>
      <button type="submit" id="wb-global-save-btn" data-wb-global-save>save</button>
    </form>
  </div>
</section>
<div id="wb-seo-score"><p data-seo>original score</p></div>`;

function startServer() {
    const received = [];
    const server = http.createServer(async (req, res) => {
        let body = '';
        for await (const chunk of req) body += chunk;
        const url = new URL(req.url, 'http://localhost');
        received.push({ path: url.pathname, body, headers: req.headers });
        if (url.pathname === '/') {
            res.setHeader('Content-Type', 'text/html');
            res.end('<html><body></body></html>');
            return;
        }
        if (url.pathname === '/network') { req.socket.destroy(); return; }
        if (url.pathname === '/redirect') { res.writeHead(302, { Location: '/forbidden' }); res.end(); return; }
        const delay = Number(url.searchParams.get('delay')) || 0;
        if (delay) await new Promise((r) => setTimeout(r, delay));
        const mode = url.searchParams.get('mode') || '';
        if (mode === 'json') {
            res.setHeader('Content-Type', 'application/json');
            res.end('{"code":200}');
            return;
        }
        if (mode === 'fullpage') {
            res.setHeader('Content-Type', 'text/html');
            res.end('<!DOCTYPE html><html><body><p>full</p></body></html>');
            return;
        }
        if (mode === 'fail') {
            res.statusCode = 422;
            res.setHeader('Content-Type', 'text/html; charset=utf-8');
            res.end('<div role="alert" data-ui-error><p>字段不合法</p></div>');
            return;
        }
        res.setHeader('Content-Type', 'text/html; charset=utf-8');
        res.setHeader('X-CSRF-Token', 'rotated');
        // 成功片段：设置面板带回一个可观测标记；全局保存回共享成功提示。
        if (url.pathname === '/admin/themes/settings/save') {
            res.end('<p role="status" data-saved>主题已保存</p>');
            return;
        }
        const tag = url.searchParams.get('tag') || 'fresh';
        res.end('<p data-panel data-tag="' + tag + '">' + tag + '</p>');
    });
    return new Promise((resolve) => {
        server.listen(0, '127.0.0.1', () => resolve({ server, received, origin: `http://127.0.0.1:${server.address().port}` }));
    });
}

async function setup(browser, origin, opts = {}) {
    const page = await browser.newPage();
    await page.setViewport({ width: opts.width || 1440, height: 900 });
    await page.goto(origin);
    await page.setContent(PANEL_HTML);
    await page.evaluate(() => {
        window.toasts = [];
        window.successes = 0;
        window.WBUI = {
            toast: (text) => window.toasts.push(text),
            // 面板里的取色器增调用不到真实 DOM 控件，这里给一个占位实现，
            // 避免视觉增强失败影响执行链断言。
            markOnce: () => true,
            controls: [],
        };
        document.addEventListener('write:success', () => window.successes++);
        // 编辑器实例：面板的请求对象从这里取当前文档。
        window.__wb = { doc: { settings: { seo: { title: 'T' } }, root: [] } };
    });
    await page.addScriptTag({ content: fs.readFileSync(path.join(UI_SRC, 'htmx.min.js'), 'utf8') });
    await page.addScriptTag({ content: fs.readFileSync(path.join(UI_SRC, 'write-base.js'), 'utf8') });
    await page.addScriptTag({ content: fs.readFileSync(path.join(APP_SRC, 'panel-requests.js'), 'utf8') });
    await page.evaluate(() => htmx.process(document.body));
    return page;
}

// trigger 派发面板取数事件（生产代码用同一事件名，从 body 冒泡）。
async function trigger(page, name) {
    await page.evaluate((n) => document.body.dispatchEvent(new CustomEvent(n, { bubbles: true })), name);
}

async function settle(page, count = 1) {
    await page.waitForFunction((n) => window.successes >= n || window.__settled >= n, { timeout: 4000 }, count).catch(() => {});
    await new Promise((r) => setTimeout(r, 120));
}

function state(page) {
    return page.evaluate(() => {
        const status = document.querySelector('#wb-settings-body [data-write-status]');
        const content = document.querySelector('#wb-settings-content');
        return {
            tag: content?.querySelector('[data-panel]')?.getAttribute('data-tag') || '',
            text: content?.textContent?.trim() || '',
            statusText: status?.textContent || '',
            statusHidden: status ? status.hidden : null,
            successes: window.successes,
            toasts: window.toasts,
        };
    });
}

test('工作台面板走共享写基座的六种真实结局', async (t) => {
    const { server, received, origin } = await startServer();
    const browser = await puppeteer.launch({
        executablePath: '/usr/bin/google-chrome', headless: true,
        args: ['--no-sandbox', '--disable-gpu'],
    });
    try {
        await t.test('成功：JSON 请求对象 + CSRF 头，HTML 片段换进目标', async () => {
            for (const width of [1440, 768, 375]) {
                const page = await setup(browser, origin, { width });
                await trigger(page, 'wb:settings-panel');
                await settle(page);
                const sent = received.filter((r) => r.path === '/workbench/settings').at(-1);
                assert.equal(sent.headers['x-csrf-token'], 'panel-token', `宽度 ${width}`);
                assert.match(sent.headers['content-type'], /^application\/json/);
                const body = JSON.parse(sent.body);
                assert.deepEqual(Object.keys(body).sort(), ['accessPassword', 'document']);
                assert.equal(body.accessPassword, '', '普通加载不应提交密码');
                assert.equal(body.document.settings.seo.title, 'T');
                const s = await state(page);
                assert.equal(s.tag, 'fresh');
                assert.equal(s.successes, 1);
                assert.equal(s.statusHidden, true);
                await page.close();
            }
        });

        await t.test('业务失败：错误进声明区，正在编辑的面板不被替换', async () => {
            const page = await setup(browser, origin);
            await trigger(page, 'wb:settings-panel');
            await settle(page);
            const s = await state(page);
            assert.equal(s.successes, 1);
            await page.evaluate(() => {
                document.querySelector('#wb-settings-form').setAttribute('hx-post', '/workbench/settings?mode=fail');
                htmx.process(document.querySelector('#wb-settings-form'));
            });
            await trigger(page, 'wb:settings-panel');
            await new Promise((r) => setTimeout(r, 400));
            const after = await state(page);
            assert.equal(after.text, 'fresh', '失败不能替换已渲染的面板内容');
            assert.equal(after.statusHidden, false, '失败必须写进声明的状态行');
            assert.equal(after.successes, 1, '失败不得触发成功动作');
            await page.close();
        });

        await t.test('非 HTML 与整页响应都不算成功片段', async () => {
            for (const mode of ['json', 'fullpage']) {
                const page = await setup(browser, origin);
                await page.evaluate((m) => {
                    document.querySelector('#wb-settings-form').setAttribute('hx-post', '/workbench/settings?mode=' + m);
                    htmx.process(document.querySelector('#wb-settings-form'));
                }, mode);
                await trigger(page, 'wb:settings-panel');
                await new Promise((r) => setTimeout(r, 400));
                const s = await state(page);
                assert.equal(s.text, 'original settings', `${mode} 不得替换目标`);
                assert.equal(s.successes, 0, `${mode} 不得触发成功动作`);
                assert.equal(s.statusHidden, false, `${mode} 必须写失败状态`);
                await page.close();
            }
        });

        await t.test('重定向与网络失败：失败受控，不导航', async () => {
            for (const target of ['/redirect', '/network']) {
                const page = await setup(browser, origin);
                await page.evaluate((t) => {
                    document.querySelector('#wb-settings-form').setAttribute('hx-post', t);
                    htmx.process(document.querySelector('#wb-settings-form'));
                }, target);
                await trigger(page, 'wb:settings-panel');
                await new Promise((r) => setTimeout(r, 500));
                const s = await state(page);
                assert.equal(s.successes, 0, `${target} 不得成功`);
                assert.equal(s.text, 'original settings', `${target} 不得替换目标`);
                assert.equal(await page.evaluate(() => location.pathname), '/', `${target} 不得导航`);
                await page.close();
            }
        });

        await t.test('迟到响应：新请求先返回，旧请求后返回不覆盖', async () => {
            const page = await setup(browser, origin);
            // 先用慢请求（2s）触发一次，再用快请求覆盖同一表单。
            await page.evaluate(() => {
                const form = document.querySelector('#wb-settings-form');
                form.setAttribute('hx-post', '/workbench/settings?tag=old&delay=1200');
                htmx.process(form);
            });
            await trigger(page, 'wb:settings-panel');
            await new Promise((r) => setTimeout(r, 60));
            await page.evaluate(() => {
                const form = document.querySelector('#wb-settings-form');
                form.setAttribute('hx-post', '/workbench/settings?tag=new');
                htmx.process(form);
            });
            await trigger(page, 'wb:settings-panel');
            await new Promise((r) => setTimeout(r, 1800));
            const s = await state(page);
            assert.equal(s.tag, 'new', '旧响应不得覆盖新面板');
            await page.close();
        });

        await t.test('全局主题保存：标准表单编码 + 隐藏绑定事实，成功才合回状态', async () => {
            const page = await setup(browser, origin);
            await page.evaluate(() => document.querySelector('#wb-global-save-btn').click());
            await new Promise((r) => setTimeout(r, 500));
            const sent = received.filter((r) => r.path === '/admin/themes/settings/save').at(-1);
            assert.match(sent.headers['content-type'], /^application\/x-www-form-urlencoded/);
            const values = new URLSearchParams(sent.body);
            assert.equal(values.get('id'), 'theme-1');
            assert.equal(values.get('headerBlockId'), 'blk-h', '面板不渲染的绑定事实必须原样带回');
            assert.equal(values.get('colors.primary'), '#111');
            const s = await page.evaluate(() => ({
                result: document.querySelector('#wb-global-save-result')?.textContent || '',
                successes: window.successes,
                btnDisabled: document.querySelector('#wb-global-save-btn')?.disabled,
            }));
            assert.match(s.result, /主题已保存/, '成功回执必须换进结果区');
            assert.equal(s.successes, 1);
            assert.equal(s.btnDisabled, false, '成功收尾必须恢复按钮');
            await page.close();
        });
    } finally {
        await browser.close();
        await new Promise((r) => server.close(r));
    }
});
