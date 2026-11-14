/* editor-app/panel-requests.js — 工作台三块业务面板的 JSON 请求对象。
 *
 * 发送与交换全部交给写基座（ui/write-base.js）：CSRF、HTML 校验、失败区、
 * 迟到响应代数隔离都在那一处。这里只回答「这一跳要提交什么对象」。
 *
 * 面板输入是整份编辑器文档与主题对象 —— 复杂编辑器只提供结构化对象，
 * 不把 JSON stringify 进普通表单字段（internal/templates/CLAUDE.md「写操作」）。
 */
(function (global) {
    'use strict';
    var ui = global.WBUI;
    if (!ui || !ui.writeBase) { return; }

    // docOf 以编辑器实例的当前文档为准；测试/驱动环境没有实例时给空文档，
    // 键仍然存在（对象形状不因环境改变）。
    function docOf() {
        var wb = global.__wb;
        if (!wb || !wb.doc) { return { settings: {}, root: [] }; }
        return wb.doc;
    }

    // metaOf 读编辑器**当前**元数据（主题 id / 主题设置 / 草稿路径）。
    //
    // 以编辑器实例持有的 live meta 为准：保存成功后它会被合回新值，
    // 而 #wb-meta 数据岛是页面加载时的事实、不会再变。重新解析数据岛会让
    // 「保存一次颜色，再打开面板又提交旧值」——两个状态源并存。
    // 没有实例时（驱动环境）才退回数据岛。
    function metaOf() {
        var wb = global.__wb;
        if (wb && wb.meta) { return wb.meta; }
        var doc = global.document;
        var el = doc && doc.getElementById ? doc.getElementById('wb-meta') : null;
        if (!el || !el.textContent) { return {}; }
        try { return JSON.parse(el.textContent) || {}; } catch (e) { return {}; }
    }

    // 设置面板与「设置访问密码」是同一个端点、同一份 DTO：密码只从密码表单自己的
    // 隐藏域读（普通加载表单没有这个字段，取到空串 = 不改密码）。明文只在这一跳
    // 出现，不写进文档。
    ui.writeBase.registerPayloadBuilder('workbench-settings', function (form) {
        // 密码是宿主表单里的隐藏域（设置动作在提交前写入），普通加载表单没有它 ——
        // 取到空串即「本跳不改密码」。按 FormData 读，与其它对象提供器同一口径。
        var pwd = form ? new FormData(form).get('wb-access-password') : '';
        return { document: docOf(), accessPassword: pwd || '' };
    });

    ui.writeBase.registerPayloadBuilder('workbench-seo', function () {
        return { document: docOf(), url: metaOf().draftPath || '' };
    });

    ui.writeBase.registerPayloadBuilder('workbench-global', function () {
        var meta = metaOf();
        return { themeId: meta.themeId || '', settings: meta.themeSettings || {} };
    });

    // 页眉/页脚/公告槽隐藏域随片段下发（服务端事实）；这些字段不参与对象组装，
    // 它们由 wb-global-save 表单按原生表单编码提交。
})(window);
