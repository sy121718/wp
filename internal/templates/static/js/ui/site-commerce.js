/* 购物车视图只消费后端金额、库存与订单事实；所有写操作交给 write-base。 */
(function (global) {
    'use strict';
    function text(root, selector, value) { var node=root.querySelector(selector); if(node) node.textContent=value == null ? '' : String(value); }
    function money(cents, currency) {
        if (!Number.isSafeInteger(cents)) return '';
        var digits=String(Math.abs(cents)).padStart(3,'0');
        return (currency || '')+' '+(cents<0?'-':'')+digits.slice(0,-2)+'.'+digits.slice(-2);
    }
    function label(root,name) { return root.getAttribute('data-label-'+name) || ''; }
    function visible(root,selector,show) { root.querySelectorAll(selector).forEach(function(node){node.hidden=!show;}); }
    function stock(root,item) {
        if(item.missing) return label(root,'off-shelf');
        if(item.available===null || !Number.isSafeInteger(item.available)) return label(root,'unknown');
        if(item.available<=0) return label(root,'out');
        if(item.available<=5 || item.available<item.quantity) return label(root,'low').replace('%d',String(item.available));
        return label(root,'in');
    }
    global.WPSiteData.register('cartState',function(root) {
        var key=root.querySelector('[name="requestId"]');
        if(key && !key.value) key.value=global.crypto.randomUUID();
    });
    global.WPSiteData.register('cartCount',function(root,data){ root.textContent=String(data.itemCount); });
    global.WPSiteData.register('cart',function(root,data){
        var host=root.querySelector('[data-cart-items]'), template=root.querySelector('template[data-cart-row]');
        if(!host || !template || !Array.isArray(data.items)) return;
        var old=new Map();
        host.querySelectorAll('[data-variant-id]').forEach(function(row){old.set(row.getAttribute('data-variant-id'),row);});
        data.items.forEach(function(item){
            var row=old.get(item.variantId);
            if(!row) { row=template.content.firstElementChild.cloneNode(true); row.setAttribute('data-variant-id',item.variantId); }
            old.delete(item.variantId);
            row.classList.toggle('is-missing',!!item.missing);
            text(row,'[data-cart-name]',item.productName || (item.missing ? label(root,'off-shelf') : ''));
            text(row,'[data-cart-variant]',item.variantLabel);
            text(row,'[data-cart-unit]',item.missing?'—':money(item.unitPrice,data.currency));
            text(row,'[data-cart-line]',item.missing?'—':money(item.lineTotal,data.currency));
            text(row,'[data-cart-stock]',stock(root,item));
            var quantity=row.querySelector('[name="quantity"]');
            if(global.document.activeElement!==quantity) quantity.value=String(item.quantity);
            quantity.max=String(item.maxQuantity);
            quantity.disabled=!!item.missing;
            var update=row.querySelector('button:not([data-cart-remove])'); if(update) update.hidden=!!item.missing;
            row.querySelector('[name="variantId"]').value=item.variantId;
            host.appendChild(row);
        });
        old.forEach(function(row){row.remove();});
        visible(root,'[data-cart-empty]',data.empty);
        visible(root,'[data-cart-total-wrap], [data-cart-clear], [data-cart-checkout]',!data.empty);
        text(root,'[data-cart-total]',money(data.total,data.currency));
        text(root,'[data-site-status]','');
    });
    global.document.addEventListener('write:success',function(event){
        var detail=event.detail || {}, form=event.target;
        if(!detail.url || detail.url.indexOf('/api/site/cart/')!==0) return;
        if(form.hasAttribute('data-site-checkout')) {
            var data=detail.data || {};
            visible(form,'[data-checkout-result]',true);
            text(form,'[data-checkout-title]',label(form,data.paid?'paid':'pending'));
            text(form,'[data-checkout-order]',data.orderNo);
            text(form,'[data-checkout-total]',money(data.total,data.currency));
            visible(form,'[data-checkout-account]',data.accountMailed);
            // 已建单后保留结果，禁用再次下单；幂等键不变，支付失败也不重复建单。
            form.querySelectorAll('button[type="submit"]').forEach(function(button){button.setAttribute('data-was-disabled','1');button.disabled=true;});
        }
        global.document.querySelectorAll('[data-site-cart][data-site-read]').forEach(function(node){global.WPSiteData.refresh(node);});
    });
})(window);
