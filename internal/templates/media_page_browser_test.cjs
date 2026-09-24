// Run with: node --test internal/templates/media_page_browser_test.cjs
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const puppeteer = require('/home/sky/.dsh/profiles/desktop/node_modules/puppeteer-core');

const root = __dirname;
const script = fs.readFileSync(path.join(root, 'static/js/media-admin.js'), 'utf8');
const ids = [
  'media-lib', 'ml-tree', 'ml-tree-search', 'ml-search', 'ml-grid', 'ml-table-body',
  'ml-table', 'ml-empty', 'ml-pager', 'ml-page-info', 'ml-prev', 'ml-next',
  'ml-view-grid', 'ml-view-list', 'ml-bulk-bar', 'ml-bulk-count',
  'ml-check-all-grid', 'ml-check-all-table', 'ml-batch-download', 'ml-batch-delete',
  'ml-upload-btn', 'ml-file-input', 'ml-drop', 'ml-upload-category',
  'ml-category-add', 'ml-cat-save', 'ml-detail', 'ml-detail-close',
];

test('media AJAX grid/table selection and confirmed deletion', async () => {
  const browser = await puppeteer.launch({
    executablePath: '/usr/bin/google-chrome', headless: true,
    args: ['--no-sandbox', '--disable-gpu'],
  });
  try {
    const page = await browser.newPage();
    const html = ids.map((id) => {
      if (id === 'ml-table-body') return '<table class="media-table-wrap"><tbody id="ml-table-body"></tbody></table>';
      if (id === 'ml-grid') return '<div id="ml-grid"></div>';
      if (id === 'ml-check-all-grid' || id === 'ml-check-all-table') return `<input type="checkbox" id="${id}">`;
      if (id.startsWith('ml-view-') || id.startsWith('ml-batch-')) return `<button id="${id}">${id}</button>`;
      return `<div id="${id}"></div>`;
    }).join('');
    await page.setContent(`<div class="media-grid-select"></div>${html}`);
    await page.setJavaScriptEnabled(true);
    await page.evaluate(() => {
      window.calls = [];
      window.MediaLib = {
        list: () => Promise.resolve({ list: [
          { id: 1, file_name: 'a.png', file_type: 'image', storage_type: 'local' },
          { id: 2, file_name: 'b.png', file_type: 'image', storage_type: 'local' },
        ], total: 2 }),
        api: (route, opts) => {
          if (route === 'category/tree') return Promise.resolve([]);
          window.calls.push([route, opts && opts.body && opts.body.id]);
          return Promise.resolve({});
        },
        renderTree: () => {}, filterTree: () => [], thumbUrl: () => '',
        typeLabel: () => '图片', formatSize: () => '1 KB', formatTime: () => '',
      };
      window.WBUI = {
        confirm: (_message, callback) => { window.confirmAction = callback; },
        toast: (message) => { window.notice = message; },
        // notifyError 走模态 alert；mock 必须接住它 —— 否则回落到原生 window.alert，
        // headless 里原生对话框会永久阻塞页面事件循环，后续 evaluate 全部超时。
        alert: (message) => { window.alertShown = message; },
        busy: (button) => { button.disabled = true; return () => { button.disabled = false; }; },
      };
    });
    await page.addScriptTag({ content: script });
    await page.waitForFunction(() => document.querySelectorAll('#ml-grid .media-card').length === 2, { timeout: 3000 }).catch(async (err) => {
      throw new Error(`${err.message}; init=${await page.evaluate(() => window.__mediaInitError)}; ready=${await page.evaluate(() => document.readyState)}; cards=${await page.$eval('#ml-grid', (e) => e.innerHTML)}`);
    });

    await page.evaluate(() => {
      document.getElementById('media-lib').appendChild(document.getElementById('ml-check-all-grid'));
      document.getElementById('media-lib').appendChild(document.getElementById('ml-check-all-table'));
      document.getElementById('media-lib').appendChild(document.getElementById('ml-grid'));
      document.getElementById('media-lib').appendChild(document.getElementById('ml-table'));
      document.getElementById('media-lib').appendChild(document.getElementById('ml-table-body').closest('table'));
    });
    await page.click('#ml-check-all-grid');
    assert.equal(await page.$eval('#ml-bulk-count', (e) => e.textContent), '已选 2 项');
    await page.evaluate(() => {
      for (const id of ['ml-view-grid', 'ml-view-list', 'ml-batch-delete', 'ml-batch-download', 'ml-bulk-bar', 'ml-bulk-count']) {
        document.getElementById('media-lib').appendChild(document.getElementById(id));
      }
    });
    await page.click('#ml-view-list');
    await page.waitForFunction(() => document.querySelectorAll('#ml-table-body tr').length === 2);
    assert.equal(await page.$eval('#ml-check-all-table', (e) => e.checked), true);
    await page.click('#ml-table-body input[data-ml-check-item="1"]');
    assert.equal(await page.$eval('#ml-bulk-count', (e) => e.textContent), '已选 1 项');
    assert.equal(await page.$eval('#ml-check-all-table', (e) => e.indeterminate), true);
    await page.click('#ml-batch-delete');
    await page.evaluate(() => window.confirmAction());
    await page.waitForFunction(() => window.calls.some(([route]) => route === 'delete'));
    assert.deepEqual(await page.evaluate(() => window.calls.filter(([route]) => route === 'delete')), [['delete', 2]]);
    assert.equal(await page.$eval('#ml-bulk-bar', (e) => e.hidden), true);
    assert.equal(await page.evaluate(() => window.__mediaInitError || ''), '');
    await page.click('#ml-check-all-table');
    await page.click('#ml-batch-delete');
    await page.evaluate(() => document.getElementById('ml-check-all-table').click());
    await page.evaluate(() => window.confirmAction());
    assert.equal(await page.evaluate(() => window.calls.filter(([route]) => route === 'delete').length), 1);
  } finally {
    await browser.close();
  }
});
