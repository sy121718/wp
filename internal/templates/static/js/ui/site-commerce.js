/* 购物车视图只消费后端金额、库存与订单事实；所有写操作交给 write-base。 */
(function (global) {
    'use strict';
    if (global.__wpSiteCommerceReady) return;
    global.__wpSiteCommerceReady = true;
    var views = {};
    function registerView(name, renderer) { views[name] = renderer; global.WPSiteData.register(name, renderer); }
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
    registerView('cartState',function(root) {
        var key=root.querySelector('[name="requestId"]');
        if(key && !key.value) key.value=global.crypto.randomUUID();
    });
    registerView('cartCount',function(root,data){ root.textContent=String(data.itemCount); });
    registerView('cart',function(root,data){
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
            if(global.document.activeElement!==quantity && !quantity.hasAttribute('data-cart-dirty')) quantity.value=String(item.quantity);
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
    function refreshCarts() {
        global.document.querySelectorAll('[data-site-cart][data-site-read]').forEach(function(node){global.WPSiteData.refresh(node);});
    }
    function checkoutResult(form,data) {
        var root=form.closest('[data-site-checkout-root]') || form;
        visible(root,'[data-checkout-result]',true);
        text(root,'[data-checkout-title]',label(root,data.paid?'paid':'pending'));
        text(root,'[data-checkout-order]',data.orderNo);
        text(root,'[data-checkout-total]',money(data.total,data.currency));
        visible(root,'[data-checkout-account]',data.accountMailed);
        visible(root,'[data-checkout-payment-error]',!!data.paymentError);
        // 订单已创建后保留同一幂等键和输入；未付款也不能重复建单。
        form.querySelectorAll('button[type="submit"]').forEach(function(button){button.setAttribute('data-was-disabled','1');button.disabled=true;});
        if (global.WPSiteActions) global.WPSiteActions.run(form,'success');
    }
    global.document.addEventListener('input',function(event){
        var input=event.target;
        if(input.matches && input.matches('.sky-cart-qty [name="quantity"]')) input.setAttribute('data-cart-dirty','');
    });
    global.WBUI.writeBase.registerResultHandler('commerce.cart',function(ctx){
        var quantity=ctx.form.querySelector('[name="quantity"]');
        if(quantity) quantity.removeAttribute('data-cart-dirty');
        refreshCarts();
        return true;
    });
    global.WBUI.writeBase.registerResultHandler('commerce.checkout',function(ctx){
        checkoutResult(ctx.form,ctx.data || {});
        refreshCarts();
        return true;
    });
    if (global.WPSiteData.registerPreview) {
        function previewSnapshot(state) {
            if (state === 'empty') return {items:[],total:0,itemCount:0,lineCount:0,currency:'CNY',empty:true};
            return {items:[{variantId:'preview-item',productName:'预览商品 · 亚麻手提包',variantLabel:'自然色 · 标准款',unitPrice:4900,lineTotal:9800,quantity:2,available:state==='out-of-stock'?0:null,maxQuantity:state==='out-of-stock'?0:99,missing:false}],total:9800,itemCount:2,lineCount:1,currency:'CNY',empty:false};
        }
        global.WPSiteData.registerPreview('cart',function(root,state){
            views.cart(root,previewSnapshot(state));
            var status={loading:'正在加载购物车…','service-error':'预览：购物车暂时无法读取，请重试。','validation-error':'预览：请填写有效数量。',submitting:'正在更新购物车…'}[state];
            if(status) text(root,'[data-site-status]',status);
            if(state==='loading') visible(root,'[data-cart-items], [data-cart-total-wrap], [data-cart-clear], [data-cart-checkout]',false);
        });
        global.WPSiteData.registerPreview('cartCount',function(root,state){views.cartCount(root,previewSnapshot(state));});
        global.WPSiteData.registerPreview('cartState',function(root,state){
            if(state==='success' && root.hasAttribute('data-site-checkout')) checkoutResult(root,{paid:true,orderNo:'PREVIEW-ORDER',total:9800,currency:'CNY',accountMailed:false});
            var message={'validation-error':'预览：请检查必填信息。','service-error':'预览：服务暂时不可用，填写内容已保留。','variant-required':'预览：请选择商品规格。',submitting:'预览：正在提交…'}[state];
            if(message) { text(root,'[data-write-status]',message); visible(root,'[data-write-status]',true); }
        });
    }
})(window);
