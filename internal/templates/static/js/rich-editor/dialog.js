// rich-editor/dialog.js — 扩展块（表格 / 手风琴）的模态编辑框。
//
// 用原生 <dialog> + showModal()：焦点圈闭、Esc 关闭、Tab 在框内循环都由浏览器提供，
// 不需要自己实现（键盘可达是这批扩展的硬要求之一）。
// 所有文案由调用方给出（本文件只渲染结构），便于后续接 i18n。
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    if (SRE.dialog) {
        return;
    }

    function buildField(field) {
        var wrap = document.createElement('label');
        wrap.className = 'sre-dialog-field';
        var caption = document.createElement('span');
        caption.className = 'sre-dialog-field__label';
        caption.textContent = field.label;
        wrap.appendChild(caption);

        var input;
        if (field.type === 'textarea') {
            input = document.createElement('textarea');
            input.rows = field.rows || 6;
            input.value = field.value == null ? '' : field.value;
        } else {
            input = document.createElement('input');
            input.type = field.type === 'number' ? 'number' : 'text';
            if (field.type === 'number') {
                input.min = field.min == null ? 1 : field.min;
                input.max = field.max == null ? 12 : field.max;
                input.step = 1;
            }
            input.value = field.value == null ? '' : field.value;
        }
        input.name = field.name;
        if (field.placeholder) {
            input.placeholder = field.placeholder;
        }
        input.className = field.type === 'textarea' ? 'sre-dialog-input sre-dialog-input--area' : 'sre-dialog-input';
        wrap.appendChild(input);
        return wrap;
    }

    // open 弹出一个模态编辑框。
    //   options.title     标题（中文）
    //   options.fields    字段数组 {name, label, type, value, min, max, placeholder, rows}
    //   options.extra     「增删行列」这类辅助按钮 [{label, onClick(values)}]
    //   options.onSubmit  (values) => void，确定时调用
    // onCancel 省略时仅关闭。
    function open(options) {
        var previous = document.querySelector('dialog.sre-dialog');
        if (previous) {
            previous.remove();
        }
        var dlg = document.createElement('dialog');
        dlg.className = 'sre-dialog';

        var form = document.createElement('form');
        form.method = 'dialog';
        form.className = 'sre-dialog-form';

        var heading = document.createElement('h2');
        heading.className = 'sre-dialog-title';
        heading.textContent = options.title;
        form.appendChild(heading);

        var body = document.createElement('div');
        body.className = 'sre-dialog-fields';
        var fields = options.fields || [];
        for (var i = 0; i < fields.length; i++) {
            body.appendChild(buildField(fields[i]));
        }
        form.appendChild(body);

        function readValues() {
            var values = {};
            var inputs = body.querySelectorAll('.sre-dialog-input');
            for (var j = 0; j < inputs.length; j++) {
                var raw = inputs[j].value;
                values[inputs[j].name] = inputs[j].type === 'number' ? parseInt(raw, 10) : raw;
            }
            return values;
        }

        if (options.extra && options.extra.length) {
            var extraRow = document.createElement('div');
            extraRow.className = 'sre-dialog-extra';
            for (var k = 0; k < options.extra.length; k++) {
                (function (item) {
                    var btn = document.createElement('button');
                    btn.type = 'button';
                    btn.className = 'btn btn-sm';
                    btn.textContent = item.label;
                    btn.addEventListener('click', function () {
                        item.onClick(readValues(), {
                            set: function (name, value) {
                                var input = body.querySelector('.sre-dialog-input[name="' + name + '"]');
                                if (input) {
                                    input.value = value;
                                }
                            }
                        });
                    });
                    extraRow.appendChild(btn);
                })(options.extra[k]);
            }
            form.appendChild(extraRow);
        }

        var actions = document.createElement('div');
        actions.className = 'sre-dialog-actions';
        var cancel = document.createElement('button');
        cancel.type = 'button';
        cancel.className = 'btn btn-ghost';
        cancel.textContent = options.cancelLabel || '取消';
        cancel.addEventListener('click', function () {
            dlg.close('cancel');
        });
        var submit = document.createElement('button');
        submit.type = 'submit';
        submit.className = 'btn btn-primary';
        submit.textContent = options.submitLabel || '确定';
        actions.appendChild(cancel);
        actions.appendChild(submit);
        form.appendChild(actions);

        form.addEventListener('submit', function (event) {
            event.preventDefault();
            var values = readValues();
            dlg.close('ok');
            dlg.remove();
            if (options.onSubmit) {
                options.onSubmit(values);
            }
        });

        dlg.appendChild(form);
        document.body.appendChild(dlg);
        dlg.addEventListener('close', function () {
            dlg.remove();
        });
        dlg.showModal();
        return dlg;
    }

    SRE.dialog = { open: open };
})();
