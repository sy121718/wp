const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');

// 体检结果改由 Jet 片段渲染：页面只声明表单与结果区，不再有 JSON→DOM 解释器。
test('SEO 体检页使用 HTMX + Jet 片段，不保留 JSON 解释器', () => {
    const page = fs.readFileSync(__dirname + '/../../../admin/analytics/seo.html', 'utf8');
    assert.match(page, /<form\b[^>]*hx-post="\/api\/publication\/seo-audit"[^>]*>/);
    assert.match(page, /hx-target="find \[data-seo-audit-result\]"/);
    assert.match(page, /data-seo-audit-result/);
    for (const gone of ['data-write-url', 'data-write-handler', 'data-write-latest', 'seo-audit-dictionary']) {
        assert.ok(!page.includes(gone), '页面仍残留 ' + gone);
    }
    assert.ok(!fs.existsSync(__dirname + '/seo-audit.js'), '不应保留 JSON→DOM 体检脚本');

    const fragment = fs.readFileSync(__dirname + '/../../../admin/analytics/seo_audit_result.html', 'utf8');
    for (const state of ['data-seo-audit-state="empty"', 'data-seo-audit-state="clean"', 'data-seo-audit-state="issues"']) {
        assert.ok(fragment.includes(state), '结果片段缺少档位 ' + state);
    }
    assert.ok(fragment.includes('seoAuditIssues'), '问题档应使用完整参数句');
});
