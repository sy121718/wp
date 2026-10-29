// 业务预设只提交受控来源；AST权威在服务端注册表，页面只插全局块引用。
const bindings = new WeakMap();
let registeredBase = null;

export function presetIdentity(preset) {
    return preset.presetKey ? 'business:' + preset.presetKey + '@' + preset.presetVersion : preset.id;
}

function insertReference(self, block, target) {
    if (target && self.findNode(target.id)) self.insertComponent(self.globalRefItem(block), target.id, target.placement);
    else self.insertComponent(self.globalRefItem(block));
    self.pendingInsertTarget = null;
    self.showEdit();
}

function bindResult(base) {
    if (registeredBase === base) return;
    base.registerResultHandler('workbench.presetCreate', function (ctx) {
        const binding = bindings.get(ctx.form);
        if (!binding) return false;
        const { self, preset, meta, dialog, status, target } = binding;
        const data = ctx.data;
        if (!data || !data.id || data.projectId !== meta.projectId || data.reuseMode !== 'global' ||
            data.presetKey !== preset.presetKey || data.presetVersion !== preset.presetVersion) {
            status.textContent = '区块已创建，但来源回执不匹配。请刷新全局块后检查。';
            status.setAttribute('data-tone', 'error');
            return true;
        }
        // 调色板只留引用事实，创建回执里的document不作为前端预设源。
        const block = { id: data.id, projectId: data.projectId, name: data.name, kind: data.kind,
            category: data.category, reuseMode: data.reuseMode, version: data.version,
            presetKey: data.presetKey, presetVersion: data.presetVersion };
        const blocks = meta.blocks || (meta.blocks = []);
        const previous = blocks.findIndex(item => item.id === block.id);
        if (previous < 0) blocks.push(block);
        else blocks[previous] = block;
        self.renderPaletteBlocks();
        insertReference(self, block, target);
        dialog.close();
        return true;
    });
    registeredBase = base;
}

function element(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text != null) node.textContent = text;
    return node;
}

// 命名仅在第一次创建时需要；同预设已有实例由用户明确选择，不自动取第一个。
export function openBusinessPreset(self, preset, meta) {
    if (!preset || !preset.presetKey || !Number.isInteger(preset.presetVersion) || preset.presetVersion < 1) return;
    if (!meta.projectId) { self.setNotice('请先选择站点工程，再创建业务区块。'); return; }
    const base = window.WBUI && window.WBUI.writeBase;
    if (!base || !base.registerResultHandler) { self.setNotice('写入基座未就绪，请刷新编辑器后重试。'); return; }
    const previous = document.getElementById('wb-business-preset-dialog');
    if (previous) { previous.querySelector('[name="name"]').focus(); return; }
    bindResult(base);
    const opener = document.activeElement;
    const target = self.pendingInsertTarget && { ...self.pendingInsertTarget };
    const dialog = element('dialog', 'wb-confirm wb-business-preset-dialog');
    dialog.id = 'wb-business-preset-dialog';
    dialog.setAttribute('aria-labelledby', 'wb-business-preset-title');
    const form = element('form', 'wb-confirm-inner');
    form.method = 'post';
    form.setAttribute('data-write', 'custom');
    form.setAttribute('data-write-url', '/api/block/create');
    form.setAttribute('data-write-handler', 'workbench.presetCreate');
    form.setAttribute('data-write-status', '#wb-business-preset-status');
    const title = element('h3', 'wb-confirm-title', preset.label || '业务预设');
    title.id = 'wb-business-preset-title';
    const description = element('p', 'wb-confirm-msg', preset.description || '命名后创建可复用区块，再插入当前页面。以后可在全局块中直接引用。');
    form.append(title, description);
    const existing = (meta.blocks || []).filter(block => block.projectId === meta.projectId && block.reuseMode === 'global' && block.presetKey === preset.presetKey && block.presetVersion === preset.presetVersion);
    if (existing.length) {
        const choices = element('div', 'wb-business-preset-existing');
        choices.append(element('p', 'wb-confirm-msg', '引用已有区块：'));
        existing.forEach(block => {
            const choose = element('button', 'btn', block.name);
            choose.type = 'button';
            choose.setAttribute('aria-label', '引用已有区块：' + block.name);
            choose.addEventListener('click', () => {
                if (form.getAttribute('aria-busy') === 'true') return;
                insertReference(self, block, target);
                dialog.close();
            });
            choices.append(choose);
        });
        form.append(choices);
    }
    const field = element('div', 'wb-field');
    const label = element('label', '', existing.length ? '或创建另一份，填写新名称' : '区块名称');
    label.htmlFor = 'wb-business-preset-name';
    const name = element('input');
    name.id = 'wb-business-preset-name';
    name.type = 'text'; name.name = 'name'; name.required = true; name.maxLength = 100;
    name.value = preset.label || '';
    name.setAttribute('autocomplete', 'off');
    name.setAttribute('aria-describedby', 'wb-business-preset-name-hint');
    const hint = element('p', 'wb-confirm-msg', '同工程内名称必须唯一。创建后可修改内容，影响范围由引用关系决定。');
    hint.id = 'wb-business-preset-name-hint';
    field.append(label, name, hint);
    form.append(field);
    const fields = { projectId: meta.projectId, presetKey: preset.presetKey, presetVersion: preset.presetVersion, kind: preset.kind || 'block', category: preset.category || 'general', reuseMode: 'global' };
    Object.keys(fields).forEach(key => {
        const input = element('input'); input.type = 'hidden'; input.name = key; input.value = String(fields[key]); form.append(input);
    });
    const status = element('p', 'wb-business-preset-status');
    status.id = 'wb-business-preset-status'; status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite');
    form.append(status);
    const actions = element('div', 'wb-confirm-actions');
    const cancel = element('button', 'btn', '取消'); cancel.type = 'button';
    const submit = element('button', 'btn btn-primary', '创建并引用'); submit.type = 'submit';
    cancel.addEventListener('click', () => { if (form.getAttribute('aria-busy') !== 'true') dialog.close(); });
    dialog.addEventListener('cancel', event => { if (form.getAttribute('aria-busy') === 'true') event.preventDefault(); });
    dialog.addEventListener('close', () => {
        bindings.delete(form);
        dialog.remove();
        if (opener && opener.isConnected) opener.focus();
    });
    actions.append(cancel, submit); form.append(actions); dialog.append(form); document.body.append(dialog);
    bindings.set(form, { self, preset, meta, dialog, status, target });
    dialog.showModal();
    name.focus(); name.select();
}
