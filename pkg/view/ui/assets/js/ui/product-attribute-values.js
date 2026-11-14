/* 属性值行只编辑 DOM；写入由写基座按显式 JSON 对象提交，响应仍由 Jet 输出。 */
(function (global) {
    'use strict';
    document.addEventListener('click', function (event) {
        var add = event.target.closest('[data-attribute-add]');
        if (add) {
            var host = add.closest('[data-attribute-values]');
            var fragment = host.querySelector('[data-attribute-prototype]').content.cloneNode(true);
            host.querySelector('[data-attribute-value-list]').appendChild(fragment);
            host.querySelector('[data-attribute-value-list]').lastElementChild.querySelector('[data-attribute-field="label"]').focus();
            return;
        }
        var remove = event.target.closest('[data-attribute-remove]');
        if (remove) remove.closest('[data-attribute-value-row]').remove();
    });
    var ui = global.WBUI;
    if (!ui || !ui.writeBase) return;
    ui.writeBase.registerPayloadBuilder('product-attribute-values', function (form) {
        var fields = new FormData(form);
        var values = [];
        form.querySelectorAll('[data-attribute-value-row]').forEach(function (row) {
            var item = {id: '', key: '', label: '', sort: 0, enabled: true};
            row.querySelectorAll('[data-attribute-field]').forEach(function (node) {
                var name = node.getAttribute('data-attribute-field');
                if (name === 'enabled') item.enabled = !!node.checked;
                else if (name === 'sort') item.sort = Number(node.value || 0);
                else item[name] = node.value;
            });
            values.push(item);
        });
        return {projectId: fields.get('projectId'), id: fields.get('id'), values: values};
    });

    /* 属性组新建/编辑：同一张抽屉里既有组字段（name/key/sort/isVariation）又有值行。
       组字段走普通 name 输入，值行走 data-attribute-field 行；两者都要进对象，
       否则服务端拿到的组字段是空的（只有 values）。 */
    ui.writeBase.registerPayloadBuilder('product-attribute-group', function (form) {
        var fields = new FormData(form);
        var values = [];
        form.querySelectorAll('[data-attribute-value-row]').forEach(function (row) {
            var item = {id: '', key: '', label: '', sort: 0, enabled: true};
            row.querySelectorAll('[data-attribute-field]').forEach(function (node) {
                var name = node.getAttribute('data-attribute-field');
                if (name === 'enabled') item.enabled = !!node.checked;
                else if (name === 'sort') item.sort = Number(node.value || 0);
                else item[name] = node.value;
            });
            values.push(item);
        });
        var payload = {
            projectId: fields.get('projectId') || '',
            key: fields.get('key') || '',
            name: fields.get('name') || '',
            sort: Number(fields.get('sort') || 0),
            isVariation: fields.get('isVariation') === '1'
        };
        // 新建抽屉没有 id；把空 id 也提交上去会让线上键超出 create DTO 的字段表。
        if (fields.get('id')) payload.id = fields.get('id');
        // 值行只在新建抽屉里渲染：编辑时没有行就不提交 values。
        // 编辑 DTO 不含 values，带上空数组只是给服务端一个会被忽略的键。
        if (values.length > 0) payload.values = values;
        return payload;
    });
})(window);
