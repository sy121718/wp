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
})(window);
