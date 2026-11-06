// 加购归属由当前商品固定，作者只调整文案、数量显隐和样式。
import { commit } from '../../../editor/methods/controls/base.js';

export const retiredCartFields = ['productSource', 'productId', 'variantId', 'source', 'optionsField', 'variantsField', 'currency', 'variantMode', 'action', 'target', 'cartTarget'];

export function hasRetiredCartWiring(node) {
    return retiredCartFields.some(function (key) { return Object.prototype.hasOwnProperty.call(node.props || {}, key); }) || Object.keys(node.actions || {}).length > 0;
}

export function clearRetiredCartWiring(node) {
    var props = node.props || {};
    retiredCartFields.forEach(function (key) { delete props[key]; });
    delete node.actions;
}

export function addToCartPanel(ctx) {
    var root = document.createElement('div'); root.className = 'wb-field';
    var hint = document.createElement('p'); hint.className = 'wb-field-hint'; hint.style.color = 'var(--sky-c-text-muted, #6b7280)';
    hint.textContent = '自动加入当前商品';
    root.appendChild(hint);
    if (hasRetiredCartWiring(ctx.node)) {
        var note = document.createElement('p'); note.className = 'wb-field-hint';
        note.textContent = '这个旧按钮含有已停用的选品接线。转换后保留文案、数量框与样式，并使用当前商品。';
        var button = document.createElement('button'); button.type = 'button'; button.className = 'wb-btn wb-btn-secondary wb-btn-sm';
        button.textContent = '改为当前商品加购';
        button.addEventListener('click', function () {
            commit(ctx, 'props.label', ctx.node.props.label || ctx.node.props.buttonText || '加入购物车', function () { clearRetiredCartWiring(ctx.node); });
            ctx.self.syncInspector();
        });
        root.append(note, button);
    }
    ctx.panel.prepend(root);
}
