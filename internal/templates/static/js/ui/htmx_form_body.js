/* ui/htmx_form_body.js — htmx 写表单的**请求体整形**（一处实现，全站生效）。
 *
 * 解决的是一个 formdata 与 JSON 之间的语义差：
 *
 *   JSON 客户端（MCP / AI / fetch）可以**省略**一个字段 —— 省略 = 本次不改。
 *   HTML 表单做不到：`<input name="sort" type="number" value="">` 提交时**一定**发
 *   `sort=`（空串）。后端 DTO 里 `*int` 收到空串会被绑成 `0`（不是 nil），
 *   于是「什么都没填」变成「把排序改成 0」—— 静默改数据，页面还显示保存成功。
 *
 * 实测确认过三种形态（独立探针，gin 同版本）：
 *   不发该键   → *int = nil  （本次不改）✅
 *   sort=      → *int = 0    （会写库）  ⚠️
 *   sort=0     → *int = 0    （正确）    ✅
 *
 * 所以这里在请求发出前把**空值键整个删掉**，让 formdata 的语义回到「省略 = 不改」。
 *
 * 为什么用 htmx:config:request 而不是 hx-params：
 *   hx-params 在 htmx 4 里**已删除**（官方 whats-new 列出）。v4 的正解就是事件式配置 ——
 *   在 config:request 里改 `ctx.request.body`（FormData 或 URLSearchParams）。
 *
 * 为什么删的是「空字符串」而不是「空白字符串」：
 *   两者都删。空格是人手误输入，把它当成「有值」会让 `sort=" "` 走到后端再报一次格式错，
 *   而用户看到的是自己根本没填过的字段报错。trim 后为空即视为未填。
 *
 * 边界：只处理写请求（POST/PUT/PATCH/DELETE）的 body。GET 的参数在 URL 上，
 * 空筛选参数本来就有既有语义（`?keyword=` 与不传 keyword 在多数列表页同义），不动它。
 */
(function () {
    'use strict';

    if (!window.htmx || typeof window.htmx.on !== 'function') return;

    // 会被整理的动词。GET/HEAD 没有 body，不在此列。
    var WRITE_VERBS = { post: 1, put: 1, patch: 1, delete: 1 };

    window.htmx.on('htmx:config:request', function (evt) {
        var ctx = (evt && evt.detail && evt.detail.ctx) || {};
        var req = ctx.request || {};
        var method = String(req.method || '').toLowerCase();
        if (!WRITE_VERBS[method]) return;

        var body = req.body;
        // v4 的 body 是 FormData 或 URLSearchParams，两者都有 forEach / delete / keys。
        if (!body || typeof body.forEach !== 'function' || typeof body.delete !== 'function') return;

        var drop = [];
        body.forEach(function (value, key) {
            // File 永远保留：空文件输入根本不会出现在 FormData 里，这里只是防御。
            if (typeof value === 'string' && value.trim() === '') drop.push(key);
        });
        for (var i = 0; i < drop.length; i++) body.delete(drop[i]);
    });
})();
