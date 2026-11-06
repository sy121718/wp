// 编辑器宿主行为夹具：真实 Chrome + 真实静态资产 + 宿主桩端点。
// 覆盖 page / block / template / instance 四种宿主 × 1440 / 768 / 375 三视口。
// 只验证「工具运行机制 + 应用宿主适配」的行为闭环；生产后台鉴权环境不可得时，
// 不把它当登录链路的验收。
const puppeteerPath = process.env.GOWP_PUPPETEER_MODULE || '/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core';
const puppeteer = require(puppeteerPath);

const origin = process.env.GOWP_EDITOR_BASE_URL;
if (!origin) throw new Error('缺少 GOWP_EDITOR_BASE_URL');

// 每种宿主的预览端点与保存端点：预览只能打自己的预览端点。
const MODES = {
    page: { preview: '/workbench/preview', save: /\/api\/page\/draft\/save/, publish: true },
    block: { preview: '/workbench/block/preview', save: /\/admin\/blocks\/save-content/, publish: false, returns: '/admin/blocks' },
    template: { preview: '/workbench/template/preview', save: /\/api\/contenttemplate\/update/, publish: false },
    instance: { preview: '/workbench/template/preview', save: /\/workbench\/instance\/save/, publish: false }
};

const WRITE_PATH = /draft\/save|blocks\/save-content|contenttemplate\/update|\/workbench\/instance\/save|page\/build|page\/publish/;

(async () => {
    const browser = await puppeteer.launch({
        executablePath: process.env.GOWP_CHROME || '/usr/bin/google-chrome',
        headless: true,
        args: ['--no-sandbox', '--disable-gpu']
    });
    const results = [];
    try {
        for (const modeName of Object.keys(MODES)) {
            const mode = MODES[modeName];
            for (const width of [1440, 768, 375]) {
                const label = `${modeName}/${width}`;
                const page = await browser.newPage();
                page.setDefaultTimeout(8000);
                const errors = [];
                const writes = [];
                page.on('pageerror', error => errors.push(error.message));
                page.on('console', msg => { if (msg.type() === 'error') errors.push(msg.text()); });
                page.on('response', res => { if (res.status() >= 400) errors.push(res.status() + ' ' + res.url()); });
                page.on('request', req => { if (req.method() === 'POST') writes.push({ url: req.url(), body: req.postData() }); });
                // 实例宿主的 409 走 window.confirm（转为独立文档）；不接 dialog 会卡住页面。
                const dialogs = [];
                page.on('dialog', async dialog => { dialogs.push(dialog.message()); await dialog.accept(); });
                await page.setViewport({ width, height: 900, hasTouch: true });
                await page.goto(origin + '/' + modeName, { waitUntil: 'networkidle0' });

                // 等待编辑器装配。
                try {
                    await page.waitForFunction(() => window.__wb && window.__wb.doc && window.__wb.doc.root.length > 0);
                } catch (e) {
                    throw new Error(`${label} 编辑器未装配；控制台：${JSON.stringify(errors.slice(0, 8))}`);
                }

                // 三视口都必须无横向溢出。
                const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
                if (overflow > 1) throw new Error(`${label} 横向溢出 ${overflow}px`);

                // 组件库与结构树：工具契约数据（由应用 generated-contracts 注入）必须出现。
                await page.waitForSelector('#wb-palette .wb-palette-item');
                const paletteCount = await page.$$eval('#wb-palette .wb-palette-item', nodes => nodes.length);
                if (paletteCount === 0) throw new Error(`${label} 组件库为空`);

                // 树点击 → 画布选中联动 + 属性面板字段。
                const input = width === 375 ? 'touch' : width === 768 ? 'keyboard' : 'mouse+wheel+keyboard';
                if (input === 'touch') await page.tap('#wb-navigator-toggle');
                else await page.click('#wb-navigator-toggle');
                await page.waitForSelector('#wb-tree .wb-node');
                if (input === 'touch') await page.tap('#wb-tree .wb-node');
                else await page.click('#wb-tree .wb-node');
                await page.waitForFunction(() => window.__wb.selectedId === 'n1');
                await page.waitForSelector('#inspector-panel [data-wb-path="text"]');
                if ((await page.evaluate(() => window.__wb.selectedId)) !== 'n1') {
                    throw new Error(`${label} 画布/树选择未联动`);
                }

                // 真实输入：1440 鼠标+滚轮+键盘；768 键盘；375 触摸点击。
                const target = '#inspector-panel [data-wb-path="text"]';
                if (input === 'mouse+wheel+keyboard') {
                    await page.mouse.wheel({ deltaY: 240 });
                    await page.focus(target);
                    await page.keyboard.down('Control');
                    await page.keyboard.press('KeyA');
                    await page.keyboard.up('Control');
                    await page.keyboard.type('新的正文');
                    await page.keyboard.press('Tab');
                } else if (input === 'keyboard') {
                    await page.focus(target);
                    await page.keyboard.down('Control');
                    await page.keyboard.press('KeyA');
                    await page.keyboard.up('Control');
                    await page.keyboard.type('键盘输入');
                    await page.keyboard.press('Tab');
                } else {
                    const box = await page.$eval(target, el => { const r = el.getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 }; });
                    await page.touchscreen.tap(box.x, box.y);
                    await page.$eval(target, el => {
                        el.value = '触摸输入';
                        el.dispatchEvent(new Event('change', { bubbles: true }));
                    });
                }
                await page.waitForFunction(() => {
                    const node = window.__wb.doc.root[0];
                    return node.props.text === '新的正文' || node.props.text === '键盘输入' || node.props.text === '触摸输入';
                });

                // 撤销 / 重做。
                if (await page.$eval('#wb-undo', el => el.disabled)) throw new Error(`${label} 修改后撤销按钮仍禁用`);
                await page.click('#wb-undo');
                await page.waitForFunction(() => window.__wb.doc.root[0].props.text === '你好');
                await page.click('#wb-redo');
                await page.waitForFunction(() => {
                    const t = window.__wb.doc.root[0].props.text;
                    return t === '新的正文' || t === '键盘输入' || t === '触摸输入';
                });

                // 预览不允许写：刷新画布会提交预览表单，但除本宿主预览端点外不得出现任何写请求，
                // 也不得改变文档版本。
                const versionBeforePreview = await page.evaluate(() => window.__wb.draftVersion);
                const beforePreview = writes.length;
                const currentText = await page.evaluate(() => window.__wb.doc.root[0].props.text);
                // 先挂监听再触发：预览是 iframe 表单提交，帧里的旧内容会让「等旧文本」立刻返回。
                const previewRequest = page.waitForRequest(
                    req => req.method() === 'POST' && req.url().includes(mode.preview),
                    { timeout: 6000 });
                await page.evaluate(() => window.__wb.flushCanvas());
                await previewRequest;
                await page.waitForFunction(text => {
                    const frame = document.getElementById('wb-canvas');
                    const body = frame && frame.contentDocument && frame.contentDocument.body;
                    return body && body.textContent.includes('预览正文') && body.textContent.includes(text);
                }, { timeout: 6000 }, currentText);
                const previewWrites = writes.slice(beforePreview);
                const stray = previewWrites.filter(w => !w.url.includes(mode.preview) || WRITE_PATH.test(w.url));
                if (stray.length) throw new Error(`${label} 预览触发了写入：${JSON.stringify(stray.map(w => w.url))}`);
                if ((await page.evaluate(() => window.__wb.draftVersion)) !== versionBeforePreview) {
                    throw new Error(`${label} 预览改变了草稿版本`);
                }

                // 保存冲突：每个视口开始新一代，第一次 409。
                // 实例宿主按业务语义弹 confirm「转为独立文档」并自动重试；
                // 其余宿主进入错误态保留输入，由作者再点一次保存。
                await page.evaluate(() => fetch('/qa/reset', { method: 'POST' }));
                if (modeName === 'instance') {
                    await page.click('#wb-save-draft');
                    try {
                        await page.waitForFunction(() => window.__wb && window.__wb.saveState === 'saved' && !window.__wb.busy);
                    } catch (e) {
                        const dbg = await page.evaluate(() => ({ saveState: window.__wb && window.__wb.saveState, busy: window.__wb && window.__wb.busy }));
                        throw new Error(label + ' 实例冲突未按确认重试 ' + JSON.stringify(dbg) + ' dialogs=' + JSON.stringify(dialogs));
                    }
                    if (dialogs.filter(t => /版本冲突|独立文档/.test(t)).length !== 1) {
                        throw new Error(`${label} 实例 409 未弹确认：${JSON.stringify(dialogs)}`);
                    }
                    const retry = writes.filter(w => mode.save.test(w.url)).pop();
                    if (!retry || !/"confirmDetach":true/.test(retry.body || '')) {
                        throw new Error(`${label} 确认后重试未带 confirmDetach`);
                    }
                    if ((await page.evaluate(() => window.__wb.draftVersion)) !== 2) throw new Error(`${label} 实例保存后版本未更新`);
                } else {
                    await page.click('#wb-save-draft');
                    try {
                        await page.waitForFunction(() => window.__wb && window.__wb.saveState === 'error', { timeout: 5000 });
                    } catch (e) {
                        const dbg = await page.evaluate(() => ({ saveState: window.__wb && window.__wb.saveState, busy: window.__wb && window.__wb.busy, notice: window.__wb && window.__wb.notice }));
                        throw new Error(label + ' 首次保存未进入错误态 ' + JSON.stringify(dbg));
                    }
                    const conflictText = await page.$eval('#wb-status', el => el.textContent);
                    if (!/版本冲突/.test(conflictText)) throw new Error(`${label} 冲突后错误文案未保留: ${conflictText}`);
                    const retained = await page.evaluate(() => window.__wb.doc.root[0].props.text);
                    if (!['新的正文', '键盘输入', '触摸输入'].includes(retained)) throw new Error(`${label} 冲突后输入丢失`);
                    if (mode.returns) {
                        // 块宿主保存必须真实回跳：这条链路正是「工具 saveDraft → 应用 afterSave」的参数契约。
                        // 回跳是整页导航，必须与点击同时等待，不能在旧上下文里等 location。
                        const navigated = page.waitForNavigation({ waitUntil: 'domcontentloaded', timeout: 8000 });
                        await page.click('#wb-save-draft');
                        await navigated;
                        const landed = new URL(page.url()).pathname;
                        if (landed !== mode.returns) throw new Error(`${label} 块保存未回跳：${landed}`);
                    } else {
                        await page.click('#wb-save-draft');
                        await page.waitForFunction(() => window.__wb && window.__wb.saveState === 'saved');
                        if ((await page.evaluate(() => window.__wb.draftVersion)) !== 2) throw new Error(`${label} 保存后版本未更新`);
                    }
                }

                // 发布失败（仅页面宿主有发布入口）：保存成功但构建失败，按钮恢复且错误提示保留。
                if (mode.publish) {
                    await page.click('#wb-publish');
                    await page.waitForSelector('.wb-confirm-ok');
                    await page.click('.wb-confirm-ok');
                    await page.waitForFunction(() => window.__wb && window.__wb.saveState === 'error');
                    await page.waitForFunction(() => !window.__wb.busy);
                    const publishText = await page.$eval('#wb-status', el => el.textContent);
                    if (!/构建失败|失败/.test(publishText)) throw new Error(`${label} 发布失败未反馈: ${publishText}`);
                }

                const realErrors = errors.filter(e => /Uncaught|TypeError|ReferenceError|SyntaxError/.test(e));
                if (realErrors.length) throw new Error(`${label} 控制台错误: ${realErrors.join(' | ')}`);
                results.push({ mode: modeName, width, input, writes: writes.length });
                await page.close();
            }
        }
        console.log('EDITOR-BROWSER-OK');
        console.log(JSON.stringify(results));
    } finally {
        await browser.close();
    }
})().catch(err => {
    console.error('EDITOR-BROWSER-FAILED', err && err.stack || err);
    process.exit(1);
});
