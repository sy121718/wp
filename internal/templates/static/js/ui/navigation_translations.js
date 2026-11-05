/* 导航译文表单 → 结构化行数组；发送、CSRF 与失败区交给 write-base 基座。
   行是真正的结构化输入（context/hash/target 三元组），明确按 JSON 提交。 */
(function (global) {
    'use strict';
    var ui = global.WBUI;
    if (!ui || !ui.writeBase || typeof ui.writeBase.registerPayloadBuilder !== 'function') { return; }

    ui.writeBase.registerPayloadBuilder('navigation-translations', function (form) {
        var fields = new FormData(form);
        var rows = [];
        form.querySelectorAll('.nav-tr-row').forEach(function (row) {
            var item = {context: '', hash: '', target: ''};
            row.querySelectorAll('input[name]').forEach(function (node) {
                var name = node.getAttribute('name');
                if (Object.prototype.hasOwnProperty.call(item, name)) { item[name] = node.value; }
            });
            rows.push(item);
        });
        return {
            action: 'translations',
            lang: fields.get('lang') || '',
            project: fields.get('project') || '',
            rows: rows
        };
    });
})(window);
