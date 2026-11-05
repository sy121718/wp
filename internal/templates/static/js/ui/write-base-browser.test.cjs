const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const puppeteer = require('/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core');

// 不替换 fetch：编码、网络、交换、取消与 finally 均由仓内 HTMX 执行。
test('HTML 写基座的真实浏览器契约', async t => {
    const received = [];
    const server = http.createServer(async (req, res) => {
        let body = '';
        for await (const chunk of req) body += chunk;
        const url = new URL(req.url, 'http://localhost');
        received.push({ path: url.pathname, body, headers: req.headers });
        if (url.pathname === '/') { res.setHeader('Content-Type', 'text/html'); res.end('<html><body></body></html>'); return; }
        if (url.pathname === '/network') { req.socket.destroy(); return; }
        if (url.pathname === '/redirect') { res.writeHead(302, { Location: '/forbidden' }); res.end(); return; }
        await new Promise(resolve => setTimeout(resolve, Number(url.searchParams.get('delay')) || 0));
        res.setHeader('Content-Type', url.pathname === '/json' ? 'application/json' : 'text/html; charset=utf-8');
        res.setHeader('X-CSRF-Token', url.pathname.endsWith('/visitor-echo') ? req.headers['x-csrf-token'] || 'visitor-token' : 'rotated');
        if (url.pathname === '/api/site/visitor-echo') {
            res.setHeader('Content-Type', 'application/json');
            res.end('{"code":200,"data":{}}'); return;
        }
        if (url.pathname === '/hx') {
            res.setHeader('HX-Redirect', '/forbidden');
            res.setHeader('HX-Location', '/forbidden');
            res.setHeader('HX-Refresh', 'true');
            res.setHeader('HX-Trigger', 'forbidden-event');
        }
        const status = url.pathname.match(/^\/status\/(\d+)$/);
        res.statusCode = status ? Number(status[1]) : 200;
        if (url.pathname === '/json') res.end('{"code":200,"message":"wrong"}');
        else if (url.pathname === '/login') res.end('<!DOCTYPE html><html><body><form id="login">login</form></body></html>');
        else if (url.pathname === '/empty') { res.statusCode = 500; res.end(); }
        else if (url.pathname === '/unsafe-error') { res.statusCode = 409; res.end('<div role="alert" data-ui-error>rejected<hx-partial hx-target="#result">wrong</hx-partial></div>'); }
        else if (url.pathname === '/drawer') res.end('<form data-drawer-fragment data-write hx-post="/status/422" hx-target="#result" hx-swap="innerHTML"><input name="title" value="' + (url.searchParams.get('message') || 'remote') + '"><select name="choice"><option value="a">A</option><option value="b">B</option></select><div data-write-status hidden></div><button id="save" type="submit">save</button></form>');
        else if (url.pathname === '/drawer-active') res.end('<form data-drawer-fragment onclick="window.forbidden++"><script>window.forbidden++</script></form>');
        else if (url.pathname === '/drawer-external') res.end('<form data-drawer-fragment hx-post="https://example.invalid/write"></form>');
        else if (status) res.end('<div role="alert" data-ui-error><p>rejected ' + res.statusCode + '</p></div>');
        else res.end('<p data-saved>' + (url.searchParams.get('message') || 'saved') + '</p><a data-write-next href="/saved/a%2Fb">edit</a><button type="button" id="dynamic" hx-post="/dynamic" hx-target="#result" data-write>again</button>');
    });
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    const origin = `http://127.0.0.1:${server.address().port}`;
    const browser = await puppeteer.launch({ executablePath: '/usr/bin/google-chrome', headless: true,
        args: ['--no-sandbox', '--disable-gpu'] });
    try {
        async function setup(path = '/save', attrs = '', fields = '', width = 1440, hasTouch = false) {
            const p = await browser.newPage();
            await p.setViewport({ width, height: 900, hasTouch });
            await p.goto(origin);
            await p.setContent(`<meta name="csrf-token" content="meta-token"><div data-hx-progress hidden></div><section data-write-host><form id="f" hx-post="${path}" hx-target="#result" hx-swap="innerHTML" hx-sync="this:drop" hx-disable="findAll button:not(:disabled)" data-write ${attrs}><input name="title" value="keep"><input name="csrf_token" value="form-token"><input name="tags" value="a"><input name="tags" value="b"><input type="file" name="file"><textarea name="editor">unsaved document</textarea>${fields}<div data-write-status hidden></div><button type="submit" id="save" name="intent" value="save">save</button><button id="disabled" disabled>disabled</button></form><div id="result">original</div></section>`);
            await p.addScriptTag({ content: fs.readFileSync(__dirname + '/htmx.min.js', 'utf8') });
            await p.evaluate(() => {
                window.toasts = []; window.successes = 0; window.finals = 0; window.forbidden = 0;
                window.WBUI = { toast: (text, options) => window.toasts.push({ text, type: options.type }) };
                document.addEventListener('write:success', () => window.successes++);
                document.addEventListener('htmx:finally:request', () => window.finals++);
                document.addEventListener('forbidden-event', () => window.forbidden++);
            });
            await p.addScriptTag({ content: fs.readFileSync(__dirname + '/write-base.js', 'utf8') });
            await p.addScriptTag({ content: fs.readFileSync(__dirname + '/htmx-bridge.js', 'utf8') });
            await p.addScriptTag({ content: fs.readFileSync(__dirname + '/../admin.js', 'utf8') });
            await p.evaluate(() => htmx.process(document.body));
            return p;
        }
        async function settle(p, count = 1) {
            await p.waitForFunction(n => window.finals >= n, { timeout: 4000 }, count);
            await p.waitForFunction(() => !document.querySelector('#save')?.disabled, { timeout: 4000 });
        }
        async function state(p) {
            return p.evaluate(() => ({ title: document.querySelector('[name=title]')?.value,
                text: document.querySelector('[data-write-status]')?.textContent,
                tone: document.querySelector('[data-write-status]')?.getAttribute('data-tone'),
                result: document.querySelector('#result')?.textContent,
                toasts: window.toasts, successes: window.successes, forbidden: window.forbidden,
                progress: !document.querySelector('[data-hx-progress]').hidden,
                disabled: document.querySelector('#disabled')?.disabled, path: location.pathname }));
        }
        await t.test('三个视口：标准表单、重复字段、具名按钮、CSRF 与 HTML 原生交换', async () => {
            for (const width of [1440, 768, 375]) {
                const p = await setup('/save', '', '', width);
                await p.click('#save'); await settle(p);
                const r = received.filter(r => r.path === '/save').at(-1);
                assert.equal(r.headers['x-csrf-token'], 'form-token');
                assert.equal(r.headers.accept, 'text/html');
                assert.match(r.headers['content-type'], /^application\/x-www-form-urlencoded/);
                const values = new URLSearchParams(r.body);
                assert.equal(values.get('title'), 'keep'); assert.deepEqual(values.getAll('tags'), ['a', 'b']);
                assert.equal(values.get('intent'), 'save');
                const s = await state(p);
                assert.match(s.result, /saved/); assert.equal(s.successes, 1);
                assert.equal(s.progress, false); assert.equal(s.disabled, true);
                await p.click('#dynamic'); await settle(p, 2);
                assert.equal((await state(p)).successes, 2);
                await p.close();
            }
        });
        await t.test('所有失败仅入本表单错误区；文件、富文本、焦点和光标不变', async () => {
            for (const path of ['/status/400', '/status/401', '/status/403', '/status/404', '/status/409', '/status/413', '/status/422', '/status/429', '/status/500', '/json', '/login', '/empty', '/network', '/redirect', '/hx', '/unsafe-error']) {
                const p = await setup(path, 'data-write-nav="[data-write-next]"');
                await p.evaluate(() => {
                    const transfer = new DataTransfer(); transfer.items.add(new File(['keep-file'], 'kept.txt'));
                    document.querySelector('[type=file]').files = transfer.files;
                    const input = document.querySelector('[name=title]'); input.focus(); input.setSelectionRange(1, 3);
                    document.querySelector('form').requestSubmit(document.querySelector('#save'));
                });
                await settle(p);
                const s = await state(p);
                assert.equal(s.tone, 'error', path); assert.equal(s.successes, 0, path);
                assert.equal(s.title, 'keep', path); assert.equal(s.result, 'original', path);
                assert.equal(s.progress, false, path); assert.equal(s.path, '/', path); assert.equal(s.forbidden, 0, path);
                assert.equal(s.toasts.length, path.startsWith('/status/') ? 0 : 1, path);
                assert.deepEqual(await p.evaluate(() => ({ file: document.querySelector('[type=file]').files[0].name,
                    editor: document.querySelector('textarea').value, focus: document.activeElement.name,
                    selection: [document.activeElement.selectionStart, document.activeElement.selectionEnd] })),
                { file: 'kept.txt', editor: 'unsaved document', focus: 'title', selection: [1, 3] }, path);
                await p.close();
            }
            assert.equal(received.filter(r => r.path === '/forbidden').length, 0);
        });
        await t.test('GET 局部界面同样拒绝失败交换、JSON、整页与后端导航，不发送写令牌', async () => {
            for (const path of ['/read', '/status/401', '/status/422', '/json', '/login', '/network', '/redirect', '/hx']) {
                const p = await setup(path);
                await p.evaluate(path => {
                    const form = document.querySelector('form');
                    form.removeAttribute('hx-post'); form.removeAttribute('data-write');
                    form.setAttribute('hx-get', path);
                    form.querySelector('[name=csrf_token]').remove();
                    document.querySelector('meta').remove();
                    htmx.process(form);
                }, path);
                await p.click('#save'); await settle(p);
                const s = await state(p);
                assert.equal(s.result, path === '/read' ? 'savededitagain' : 'original', path);
                assert.equal(s.title, 'keep', path); assert.equal(s.successes, 0, path);
                assert.equal(s.progress, false, path); assert.equal(s.forbidden, 0, path);
                if (path !== '/read') assert.equal(s.tone, 'error', path);
                const request = received.filter(r => r.path === path).at(-1);
                assert.equal(request.headers['x-csrf-token'], undefined, path);
                assert.equal(request.headers.accept, 'text/html', path);
                await p.close();
            }
        });
        await t.test('不同 GET 入口共用目标时只接受最新请求，各自目标互不失效', async () => {
            for (const older of ['/read', '/status/409', '/json']) {
                for (const newer of ['/read', '/status/422']) {
                    const p = await setup();
                    await p.evaluate(({ older, newer }) => {
                        const host = document.createElement('section');
                        host.setAttribute('data-write-host', '');
                        host.innerHTML = `<button id="older" type="button" hx-get="${older}?delay=150&message=older" hx-target="#result" hx-swap="innerHTML">older</button><button id="newer" type="button" hx-get="${newer}?delay=10&message=newer" hx-target="#result" hx-swap="innerHTML">newer</button><div id="read-status" data-write-status hidden></div>`;
                        document.body.appendChild(host); htmx.process(host);
                    }, { older, newer });
                    const started = p.waitForRequest(request => request.url().includes('delay=150'));
                    await p.click('#older'); await started;
                    await p.click('#newer'); await settle(p, 2);
                    const s = await state(p);
                    assert.equal(s.result, newer === '/read' ? 'newereditagain' : 'original', older + ' -> ' + newer);
                    assert.equal(await p.$eval('#read-status', node => node.textContent), newer === '/read' ? '' : 'rejected 422');
                    assert.equal(s.toasts.length, 0); assert.equal(s.successes, 0); assert.equal(s.progress, false);
                    assert.equal(await p.$$eval('#older, #newer', nodes => nodes.some(node => node.hasAttribute('aria-busy'))), false);
                    await p.close();
                }
            }
            const p = await setup();
            await p.evaluate(() => {
                const host = document.createElement('section');
                host.innerHTML = '<button id="older" hx-get="/read?delay=100&message=older" hx-target="#result">older</button><button id="newer" hx-get="/read?delay=10&message=newer" hx-target="#other">newer</button><div id="other"></div>';
                document.body.appendChild(host); htmx.process(host);
            });
            await p.click('#older'); await p.click('#newer'); await settle(p, 2);
            assert.equal((await state(p)).result, 'oldereditagain');
            assert.equal(await p.$eval('#other', node => node.textContent), 'newereditagain');
            assert.equal((await state(p)).successes, 0);
            await p.close();
        });
        await t.test('原生字段语义：checkbox、disabled、form 外字段与键盘具名提交', async () => {
            const p = await setup('/native-fields', '', '<input name="disabledValue" value="skip" disabled><input type="checkbox" name="enabled" value="true" checked><input type="checkbox" name="unchecked" value="true"><input type="number" name="quantity" value="0"><input name="empty" value="">');
            await p.evaluate(() => document.body.insertAdjacentHTML('beforeend', '<input form="f" name="external" value="outside">'));
            await p.focus('#save'); await p.keyboard.press('Enter'); await settle(p);
            const values = new URLSearchParams(received.filter(r => r.path === '/native-fields').at(-1).body);
            assert.equal(values.get('intent'), 'save'); assert.equal(values.get('external'), 'outside');
            assert.equal(values.get('enabled'), 'true'); assert.equal(values.has('unchecked'), false);
            assert.equal(values.has('disabledValue'), false); assert.equal(values.get('quantity'), '0');
            assert.equal(values.get('empty'), '');
            await p.close();
        });
        await t.test('本地动态抽屉与真实下拉扫描：触摸、滚动、键盘、失败保留与关闭后迟到响应', async () => {
            for (const width of [1440, 768, 375]) {
                const p = await setup('/unused', '', '', width, true);
                const errors = [];
                p.on('pageerror', error => errors.push(error.message));
                await p.evaluate(() => {
                    document.querySelector('[data-write-host]').remove();
                    document.body.insertAdjacentHTML('beforeend', '<button id="open" data-drawer-open="#edit-template">open</button><template id="edit-template"><form data-write hx-post="/status/422" hx-target="#result" hx-swap="innerHTML" hx-sync="this:drop" hx-disable="findAll button:not(:disabled)"><input name="title" value="draft"><select name="choice" data-ui-select><option value="a">A</option><option value="b">B</option></select><div data-write-status hidden></div><button id="save" type="submit">save</button></form></template><aside data-drawer hidden><header><span data-drawer-title></span><button data-drawer-close>close</button></header><div data-drawer-body></div></aside><div data-drawer-mask hidden></div><div id="result">original</div>');
                });
                for (const name of ['_util.js', 'select.js', 'drawer.js', 'index.js']) {
                    await p.addScriptTag({ content: fs.readFileSync(__dirname + '/' + name, 'utf8') });
                }
                await p.tap('#open');
                await p.waitForSelector('[data-drawer] .wbs-trigger');
                await p.tap('.wbs-trigger');
                await p.mouse.wheel({ deltaY: 50 });
                await p.keyboard.press('ArrowDown'); await p.keyboard.press('Enter');
                assert.equal(await p.$eval('[data-drawer] select', node => node.value), 'b');
                await p.evaluate(() => { WBUI.scan(document); WBUI.scan(document); });
                assert.equal(await p.$$eval('.wbs-trigger', nodes => nodes.length), 1);
                await p.focus('#save'); await p.keyboard.press('Enter'); await settle(p);
                assert.match(await p.$eval('[data-write-status]', node => node.textContent), /rejected 422/);
                assert.equal(await p.$eval('[name=title]', node => node.value), 'draft');
                assert.equal(await p.$eval('select', node => node.value), 'b');
                await p.evaluate(() => {
                    document.querySelector('[data-drawer] form').setAttribute('hx-post', '/drawer-late?delay=150');
                    document.querySelector('#save').click();
                });
                await p.keyboard.press('Escape');
                await settle(p, 2);
                assert.equal(await p.$eval('[data-drawer]', node => node.hidden), true);
                assert.equal(await p.$eval('[data-drawer-body]', node => node.children.length), 0);
                assert.equal((await state(p)).successes, 0); assert.equal((await state(p)).result, 'original');
                assert.equal((await state(p)).progress, false);
                await p.tap('#open');
                await p.waitForSelector('[data-drawer] .wbs-trigger');
                assert.equal(await p.$$eval('.wbs-trigger', nodes => nodes.length), 1);
                await p.tap('.wbs-trigger'); await p.keyboard.press('ArrowDown'); await p.keyboard.press('Enter');
                assert.equal(await p.$eval('select', node => node.value), 'b');
                assert.deepEqual(errors, []);
                await p.close();
            }
        });
        await t.test('远程抽屉由 HTMX 加载 HTML：三视口、失败隔离及关闭重开后的迟到响应', async () => {
            for (const width of [1440, 768, 375]) {
                const p = await setup('/unused', '', '', width, true);
                const errors = [];
                p.on('pageerror', error => errors.push(error.message));
                await p.evaluate(() => {
                    document.querySelector('[data-write-host]').remove();
                    document.body.insertAdjacentHTML('beforeend', '<form id="background-form"><div id="background-error" data-write-status>bulk-error</div><button id="remote" type="button" data-drawer-open hx-get="/drawer" hx-target="#remote-body" hx-swap="innerHTML">remote</button></form><aside data-drawer data-write-host hidden><header><span data-drawer-title></span><button data-drawer-close>close</button></header><div id="remote-error" data-write-status hidden></div><div id="remote-body" data-drawer-body></div></aside><div data-drawer-mask hidden></div><div id="result">original</div>');
                });
                for (const name of ['_util.js', 'select.js', 'drawer.js', 'index.js']) {
                    await p.addScriptTag({ content: fs.readFileSync(__dirname + '/' + name, 'utf8') });
                }
                await p.evaluate(() => htmx.process(document.body));
                await p.tap('#remote'); await settle(p);
                assert.equal(await p.$eval('[data-drawer]', node => node.hidden), false);
                await p.waitForSelector('#remote-body .wbs-trigger');
                await p.tap('#remote-body .wbs-trigger');
                await p.mouse.wheel({ deltaY: 50 });
                await p.keyboard.press('ArrowDown'); await p.keyboard.press('Enter');
                assert.equal(await p.$eval('#remote-body select', node => node.value), 'b');
                await p.focus('#save'); await p.keyboard.press('Enter'); await settle(p, 2);
                assert.match(await p.$eval('#remote-body [data-write-status]', node => node.textContent), /rejected 422/);
                assert.equal(await p.$eval('#remote-body select', node => node.value), 'b');
                await p.keyboard.press('Escape');
                assert.equal(await p.evaluate(() => document.activeElement.id), 'remote');
                let finals = 2;
                for (const path of ['/status/401', '/status/422', '/json', '/login', '/network', '/redirect', '/hx', '/drawer-active', '/drawer-external', '/read']) {
                    await p.evaluate(path => document.querySelector('#remote').setAttribute('hx-get', path), path);
                    await p.tap('#remote'); await settle(p, ++finals);
                    assert.equal(await p.$eval('#remote-body', node => node.children.length), 0, path);
                    assert.equal(await p.$eval('#remote-error', node => node.hidden), false, path);
                    assert.equal(await p.$eval('#background-error', node => node.textContent), 'bulk-error', path);
                    assert.equal((await state(p)).successes, 0, path);
                    assert.equal((await state(p)).forbidden, 0, path);
                    await p.keyboard.press('Escape');
                }
                for (const path of ['/drawer', '/status/409', '/json']) {
                    await p.evaluate(path => document.querySelector('#remote').setAttribute('hx-get', path + '?delay=150&message=obsolete'), path);
                    const started = p.waitForRequest(request => request.url().includes('delay=150'));
                    await p.tap('#remote'); await started;
                    await p.keyboard.press('Escape');
                    await p.evaluate(() => document.querySelector('#remote').setAttribute('hx-get', '/drawer?message=current'));
                    await p.tap('#remote'); await settle(p, finals += 2);
                    assert.equal(await p.$eval('#remote-body input', node => node.value), 'current');
                    assert.equal(await p.$eval('#remote-error', node => node.hidden), true);
                    assert.equal(await p.$eval('[data-drawer]', node => node.hidden), false);
                    assert.equal(await p.$eval('#remote', node => node.hasAttribute('aria-busy')), false);
                    await p.keyboard.press('Escape');
                }
                assert.equal((await state(p)).successes, 0);
                assert.deepEqual(errors, []);
                await p.close();
            }
            assert.equal(received.filter(request => request.path === '/forbidden').length, 0);
            for (const request of received.filter(request => request.path === '/drawer')) {
                assert.equal(request.headers.accept, 'text/html');
                assert.equal(request.headers['x-csrf-token'], undefined);
            }
        });
        await t.test('下拉移除后断开观察与旧监听，节点移动不销毁，重新挂载只增强一次', async () => {
            const p = await setup();
            await p.evaluate(() => {
                window.selectDisconnects = 0;
                const NativeObserver = window.MutationObserver;
                window.MutationObserver = class extends NativeObserver {
                    observe(node, options) { this.select = node.tagName === 'SELECT'; super.observe(node, options); }
                    disconnect() { if (this.select) window.selectDisconnects++; super.disconnect(); }
                };
                document.body.insertAdjacentHTML('beforeend', '<section id="select-host"><select id="disposable"><option value="a">A</option><option value="b">B</option></select></section><section id="move-target"></section>');
            });
            for (const name of ['_util.js', 'select.js', 'index.js']) {
                await p.addScriptTag({ content: fs.readFileSync(__dirname + '/' + name, 'utf8') });
            }
            await p.waitForSelector('#select-host .wbs-trigger');
            await p.evaluate(() => {
                window.oldHost = document.querySelector('#select-host');
                window.oldSelect = document.querySelector('#disposable');
                window.oldTrigger = oldHost.querySelector('.wbs-trigger');
                window.oldMenu = oldHost.querySelector('.wbs-menu');
                document.querySelector('#move-target').appendChild(oldHost);
                oldSelect.appendChild(new Option('C', 'c'));
            });
            await p.waitForFunction(() => oldMenu.children.length === 3);
            assert.equal(await p.evaluate(() => window.selectDisconnects), 0);
            await p.click('#select-host .wbs-trigger');
            await p.evaluate(() => window.oldHost.remove());
            await p.waitForFunction(() => window.selectDisconnects === 1, { timeout: 1000 });
            await p.evaluate(() => {
                oldTrigger.click();
                oldTrigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
                oldMenu.children[1].click();
                oldSelect.appendChild(new Option('D', 'd'));
            });
            await p.evaluate(() => new Promise(resolve => setTimeout(resolve, 20)));
            assert.deepEqual(await p.evaluate(() => ({ value: oldSelect.value, opened: oldTrigger.getAttribute('aria-expanded'),
                menuHidden: oldMenu.hidden, count: oldMenu.children.length })), { value: 'a', opened: 'false', menuHidden: true, count: 3 });
            await p.evaluate(() => document.body.appendChild(oldHost));
            await p.waitForFunction(() => document.querySelector('#select-host .wbs-trigger') !== window.oldTrigger);
            await p.evaluate(() => { WBUI.scan(document); WBUI.scan(document); });
            assert.equal(await p.$$eval('#select-host .wbs-trigger', nodes => nodes.length), 1);
            assert.equal(await p.$$eval('#select-host .wbs-option', nodes => nodes.length), 4);
            await p.click('#select-host .wbs-trigger');
            await p.keyboard.press('ArrowDown'); await p.keyboard.press('Enter');
            assert.equal(await p.$eval('#disposable', node => node.value), 'b');
            await p.close();
        });
        await t.test('明确 multipart 保留文件与 boundary', async () => {
            const p = await setup('/upload', 'hx-encoding="multipart/form-data"');
            await p.evaluate(() => {
                const transfer = new DataTransfer(); transfer.items.add(new File(['upload-content'], 'sample.txt'));
                document.querySelector('[type=file]').files = transfer.files;
            });
            await p.click('#save'); await settle(p);
            const r = received.filter(r => r.path === '/upload').at(-1);
            assert.match(r.headers['content-type'], /^multipart\/form-data; boundary=/);
            assert.match(r.body, /filename="sample.txt"/); assert.match(r.body, /upload-content/);
            assert.equal((await state(p)).successes, 1);
            await p.close();
        });
        await t.test('复杂编辑器只注册请求对象；结果仍为 HTML，无 JSON 结果注册表', async () => {
            const p = await setup('/editor', 'data-write-builder="document"');
            await p.evaluate(() => {
                WBUI.writeBase.registerPayloadBuilder('document', form => ({ title: form.elements.title.value, rows: [{ id: '9007199254740993', quantity: 0 }] }));
            });
            await p.click('#save'); await settle(p);
            const r = received.filter(r => r.path === '/editor').at(-1);
            assert.match(r.headers['content-type'], /^application\/json/);
            assert.deepEqual(JSON.parse(r.body), { title: 'keep', rows: [{ id: '9007199254740993', quantity: 0 }] });
            assert.match((await state(p)).result, /saved/);
            assert.equal(await p.evaluate(() => typeof WBUI.writeBase.registerResultHandler), 'undefined');
            await p.close();
        });
        await t.test('缺少 token、未注册对象提供器、外站请求不发送', async () => {
            for (const kind of ['csrf', 'builder', 'origin']) {
                const p = await setup('/not-sent');
                await p.evaluate(kind => {
                    const f = document.querySelector('form');
                    if (kind === 'csrf') { f.elements.csrf_token.remove(); document.querySelector('meta').remove(); }
                    if (kind === 'builder') f.setAttribute('data-write-builder', 'unknown');
                    if (kind === 'origin') f.setAttribute('hx-post', 'https://example.invalid/write');
                }, kind);
                const before = received.filter(r => r.path === '/not-sent').length;
                await p.click('#save'); await settle(p);
                assert.equal((await state(p)).tone, 'error'); assert.equal((await state(p)).toasts.length, 1);
                assert.equal(received.filter(r => r.path === '/not-sent').length, before);
                await p.close();
            }
        });
        await t.test('访客会话由模板声明，不按 URL 前缀猜测且不能混用后台 token', async () => {
            const p = await setup('/visitor', 'data-write-session="visitor"');
            await p.evaluate(() => { window.WPSiteSession = { csrfToken: 'visitor-token' }; });
            await p.click('#save'); await settle(p);
            assert.equal(received.filter(r => r.path === '/visitor').at(-1).headers['x-csrf-token'], 'visitor-token');
            assert.equal(await p.evaluate(() => WPSiteSession.csrfToken), 'rotated');
            await p.close();
            const missing = await setup('/visitor-missing', 'data-write-session="visitor"');
            await missing.click('#save'); await settle(missing);
            assert.equal(received.filter(r => r.path === '/visitor-missing').length, 0);
            await missing.close();
        });
        await t.test('访客多表单乱序与数据读取迟到不回退令牌，移除表单仍接收真实轮换', async () => {
            for (const oldRotates of [false, true]) {
                const older = oldRotates ? '/visitor-rotate' : '/visitor-echo';
                const newer = oldRotates ? '/visitor-echo' : '/visitor-rotate';
                const p = await setup(older + '?delay=150', 'data-write-session="visitor"');
                await p.evaluate(newer => {
                    window.WPSiteSession = { csrfToken: 'visitor-token' };
                    const other = document.querySelector('#f').cloneNode(true);
                    other.id = 'visitor-other';
                    other.setAttribute('hx-post', newer + '?delay=10');
                    other.setAttribute('hx-target', '#other-result');
                    for (const node of other.querySelectorAll('[id]')) node.removeAttribute('id');
                    document.body.append(other);
                    document.body.insertAdjacentHTML('beforeend', '<div id="other-result">other original</div>');
                    htmx.process(other);
                }, newer);
                const started = p.waitForRequest(request => request.url().includes(older + '?delay=150'));
                await p.click('#save'); await started;
                if (oldRotates) await p.evaluate(() => document.querySelector('#f').remove());
                await p.evaluate(() => document.querySelector('#visitor-other').requestSubmit());
                await settle(p, 2);
                assert.equal(await p.evaluate(() => WPSiteSession.csrfToken), 'rotated');
                assert.equal((await state(p)).successes, oldRotates ? 1 : 2);
                assert.equal((await state(p)).toasts.length, 0);
                await p.evaluate(() => document.querySelector('#visitor-other').requestSubmit());
                await settle(p, 3);
                assert.equal(received.filter(r => r.path === newer).at(-1).headers['x-csrf-token'], 'rotated');
                await p.close();
            }
            const p = await setup('/visitor-rotate', 'data-write-session="visitor"');
            await p.evaluate(() => { window.WPSiteSession = { csrfToken: 'visitor-token' }; });
            await p.addScriptTag({ content: fs.readFileSync(__dirname + '/site-data.js', 'utf8') });
            const readStarted = p.waitForRequest(request => request.url().includes('/api/site/visitor-echo'));
            await p.evaluate(() => {
                WPSiteData.register('csrf-test', node => { node.textContent = 'loaded'; });
                const read = document.createElement('div');
                read.id = 'session-read';
                read.setAttribute('data-site-read', '/api/site/visitor-echo?delay=150');
                read.setAttribute('data-site-view', 'csrf-test');
                document.body.append(read);
                WPSiteData.refresh(read);
            });
            await readStarted;
            await p.click('#save'); await settle(p);
            await p.waitForFunction(() => document.querySelector('#session-read').textContent === 'loaded');
            assert.equal(await p.evaluate(() => WPSiteSession.csrfToken), 'rotated');
            assert.equal((await state(p)).successes, 1);
            assert.equal((await state(p)).toasts.length, 0);
            await p.close();
        });
        await t.test('drop 防重复、确认取消、超时均正确收尾', async () => {
            const p = await setup('/slow?delay=100');
            await p.evaluate(() => { const f = document.querySelector('form'); f.requestSubmit(); f.requestSubmit(); });
            await settle(p);
            assert.equal(received.filter(r => r.path === '/slow').length, 1);
            assert.equal((await state(p)).successes, 1);
            await p.close();
            const cancel = await setup('/cancel', 'hx-confirm="确认提交？"');
            const dismissed = new Promise(resolve => cancel.once('dialog', async dialog => { await dialog.dismiss(); resolve(); }));
            await cancel.click('#save'); await dismissed; await settle(cancel);
            assert.equal(received.filter(r => r.path === '/cancel').length, 0);
            assert.equal((await state(cancel)).toasts.length, 0); assert.equal((await state(cancel)).progress, false);
            await cancel.close();
            const slow = await setup('/timeout?delay=150');
            await slow.evaluate(() => { htmx.config.defaultTimeout = 30; });
            await slow.click('#save'); await settle(slow);
            assert.equal((await state(slow)).tone, 'error'); assert.equal((await state(slow)).toasts.length, 1);
            assert.equal((await state(slow)).result, 'original'); await slow.close();
        });
        await t.test('同一宿主中的多个表单不串错误区，缺少本表单错误区也不借用其他表单', async () => {
            const p = await setup('/status/409');
            await p.evaluate(() => {
                const other = document.querySelector('form').cloneNode(true);
                other.id = 'other';
                other.setAttribute('hx-post', '/status/422');
                other.querySelector('[data-write-status]').remove();
                for (const node of other.querySelectorAll('[id]')) node.removeAttribute('id');
                document.querySelector('[data-write-host]').append(other);
                htmx.process(other);
                other.requestSubmit();
            });
            await settle(p);
            assert.equal(await p.$eval('#f [data-write-status]', node => node.textContent), '');
            assert.equal((await state(p)).toasts.length, 1);
            await p.evaluate(() => document.querySelector('#f').requestSubmit());
            await settle(p, 2);
            assert.match(await p.$eval('#f [data-write-status]', node => node.textContent), /rejected 409/);
            assert.equal((await state(p)).toasts.length, 1);
            await p.close();
        });
        await t.test('同一表单乱序成功或失败仅接受最后一次请求，忙碌态等待全部收尾', async () => {
            for (const oldPath of ['/race-old?delay=160&message=old', '/status/409?delay=160', '/json?delay=160']) {
                const p = await setup(oldPath);
                await p.evaluate(() => {
                    const form = document.querySelector('form');
                    form.removeAttribute('hx-sync');
                    form.removeAttribute('hx-disable');
                    form.insertAdjacentHTML('beforeend', '<button type="button" id="newer" hx-post="/race-new?message=new" hx-target="#result" hx-sync="this:drop" data-write>newer</button>');
                    htmx.process(form);
                    form.requestSubmit();
                    document.querySelector('#newer').click();
                });
                await p.waitForFunction(() => window.finals === 1);
                assert.match((await state(p)).result, /new/);
                assert.equal(await p.$eval('#f', node => node.getAttribute('aria-busy')), 'true');
                assert.equal((await state(p)).progress, true);
                await settle(p, 2);
                const s = await state(p);
                assert.match(s.result, /new/); assert.doesNotMatch(s.result, /old/);
                assert.equal(s.successes, 1); assert.equal(s.toasts.length, 0);
                assert.equal(s.progress, false);
                assert.equal(await p.$eval('#f', node => node.getAttribute('aria-busy')), null);
                await p.close();
            }
        });
        await t.test('成功导航消费 Jet 链接；表单移除后迟到响应无副作用', async () => {
            const p = await setup('/navigate', 'data-write-nav="[data-write-next]"');
            await Promise.all([p.waitForNavigation(), p.click('#save')]);
            assert.equal(new URL(p.url()).pathname, '/saved/a%2Fb'); await p.close();
            const removed = await setup('/removed?delay=100');
            await removed.evaluate(() => { const f = document.querySelector('form'); f.requestSubmit(); f.remove(); });
            await removed.waitForFunction(() => window.finals >= 1);
            assert.equal((await state(removed)).result, 'original'); assert.equal((await state(removed)).successes, 0);
            assert.equal((await state(removed)).progress, false); await removed.close();
        });
    } finally {
        await browser.close();
        await new Promise(resolve => server.close(resolve));
    }
});
