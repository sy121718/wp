/* 订单服务提供事实；此脚本只回填静态外壳和管理本实例的阅读/申请步骤。 */
(function (global) {
    'use strict';
    if (global.WPSiteOrdersReady || !global.WPSiteData) return;
    global.WPSiteOrdersReady = true;
    var document = global.document, lastAuthenticated;
    function owner(node) { return node && node.closest('[data-site-orders]'); }
    function part(root, name) { return root.querySelector('[data-orders-panel="' + name + '"]'); }
    function label(root, key) { return root.getAttribute('data-label-' + key.toLowerCase()) || ''; }
    function text(root, selector, value) { var el = root.querySelector(selector); if (el) el.textContent = value == null ? '' : String(value); }
    function money(value, currency) { var cents = Number(value); return (currency || '') + ' ' + (Number.isFinite(cents) ? (cents / 100).toFixed(2) : '—'); }
    function time(value) { var date = new Date(Number(value)); return Number.isFinite(date.getTime()) ? date.toLocaleString() : ''; }
    function status(root, value, isReturn) { var key = isReturn && (value === 'completed' || value === 'cancelled') ? 'return' + value : value; return label(root, key) || value || ''; }
    function show(root, name, focus) {
        var panel = part(root, name); if (!panel) return;
        root.setAttribute('data-orders-active', name);
        root.querySelectorAll('[data-orders-panel]').forEach(function (panel) { panel.hidden = panel.getAttribute('data-orders-panel') !== name; });
        if (focus && panel.focus) panel.focus();
    }
    function clearStatus(root) { text(root, '[data-site-status]', ''); var retry = root.querySelector('[data-orders-retry]'); if (retry) retry.hidden = true; }
    function refresh(root) { return global.WPSiteData.refresh(root); }
    function read(root, name, orderID) {
        var host = root.querySelector('[data-order-read="' + name + '"]'); if (!host) return;
        var url = new URL(root.getAttribute('data-orders-' + name + '-url'), global.location.href);
        url.searchParams.set('orderId', orderID);
        host.setAttribute('data-site-read', url.pathname + url.search); refresh(host);
    }
    function renderList(root, data) {
        var list = root.querySelector('[data-orders-list]'), template = root.querySelector('template[data-orders-row]');
        if (!list || !template) return;
        list.replaceChildren();
        var items = data && data.list || [];
        items.forEach(function (item) {
            var row = template.content.firstElementChild.cloneNode(true);
            text(row, '[data-order-number]', item.orderNo); text(row, '[data-order-time]', time(item.createTime));
            text(row, '[data-order-status]', status(root, item.status)); text(row, '[data-order-total]', money(item.total, item.currency));
            row.querySelector('[data-order-open]').setAttribute('data-order-id', String(item.id)); list.appendChild(row);
        });
        var url = new URL(root.getAttribute('data-site-read'), global.location.href), offset = Number(url.searchParams.get('offset') || 0), limit = Number(url.searchParams.get('limit') || 10);
        root.querySelector('[data-orders-empty]').hidden = items.length !== 0;
        root.querySelector('[data-orders-prev]').disabled = offset === 0;
        root.querySelector('[data-orders-next]').disabled = offset + items.length >= Number(data && data.total || 0);
        text(root, '[data-orders-count]', data && data.total || 0);
        var wasGuest = !root.querySelector('[data-orders-guest]').hidden;
        root.querySelector('[data-orders-guest]').hidden = true; clearStatus(root);
        if (wasGuest || !root.getAttribute('data-orders-active')) show(root, 'list', false);
        root._ordersLimit = limit;
    }
    function renderDetail(host, data) {
        var root = owner(host); if (!root || !data || !data.head) return;
        if (root._ordersSelected && String(data.head.id) !== root._ordersSelected) return;
        root._ordersDetail = data; var head = data.head;
        if(root._ordersReturnable)root.querySelector('[data-orders-apply]').disabled=!root._ordersReturnable.canRequest;
        text(host, '[data-detail-number]', head.orderNo); text(host, '[data-detail-time]', time(head.createTime)); text(host, '[data-detail-status]', status(root, head.status));
        ['subtotal','discount','shipping','tax','total'].forEach(function (name) { var field = {discount:'discountTotal',shipping:'shippingTotal',tax:'taxTotal'}[name] || name; text(host, '[data-detail-' + name + ']', money(head[field], head.currency)); });
        text(host, '[data-detail-address]', [head.shipName,head.shipPhone,[head.shipCountry,head.shipProvince,head.shipCity,head.shipDistrict,head.shipAddress,head.shipZip].filter(Boolean).join(' ')].filter(Boolean).join('\n'));
        text(host, '[data-detail-payment]', head.paymentMethodTitle || head.paymentMethod || '—'); text(host, '[data-detail-remark]', head.remark || '—');
        var list = host.querySelector('[data-detail-items]'), template = host.querySelector('template[data-order-item-row]'); list.replaceChildren();
        (data.items || []).forEach(function (item) { var row = template.content.firstElementChild.cloneNode(true); text(row,'[data-item-name]',item.productName); text(row,'[data-item-variant]',item.variantLabel || item.sku); text(row,'[data-item-quantity]','× ' + item.quantity); text(row,'[data-item-total]',money(item.lineTotal,head.currency)); list.appendChild(row); });
        var history = host.querySelector('[data-detail-history]'), logTemplate = host.querySelector('template[data-order-log-row]'); history.replaceChildren();
        (data.logs || []).forEach(function (log) { var row = logTemplate.content.firstElementChild.cloneNode(true); text(row,'[data-log-status]',status(root,log.toStatus)); text(row,'[data-log-time]',time(log.createTime)); history.appendChild(row); });
        text(host,'[data-site-status]','');
        if(root._ordersReturns)renderReturns(root.querySelector('[data-order-read="returns"]'),root._ordersReturns);
    }
    function renderReturnable(host, data) {
        var root = owner(host); if (!root) return; root._ordersReturnable = data;
        host.querySelector('[data-orders-apply]').hidden = !(data && data.canRequest);
        host.querySelector('[data-orders-apply]').disabled = !root._ordersDetail;
        host.querySelector('[data-orders-no-returnable]').hidden = !!(data && data.canRequest);
        text(host,'[data-site-status]','');
    }
    function renderReturns(host, data) {
        var root = owner(host); if (!root) return; root._ordersReturns=data;
        var list = host.querySelector('[data-orders-returns]'), template = host.querySelector('template[data-order-return-row]'); list.replaceChildren();
        var currency = root._ordersDetail && root._ordersDetail.head.currency;
        (data && data.list || []).forEach(function (item) { var row = template.content.firstElementChild.cloneNode(true); text(row,'[data-return-number]',item.returnNo); text(row,'[data-return-reason]',item.reason); text(row,'[data-return-status]',status(root,item.status,true)); text(row,'[data-return-amount]',money(item.refundAmount,currency)); list.appendChild(row); });
        text(host,'[data-site-status]','');
    }
    function openOrder(root, id) {
        root._ordersSelected = String(id); root._ordersDetail = null; root._ordersReturnable = null; root._ordersReturns=null;
        text(root,'[data-detail-number]',''); root.querySelector('[data-detail-items]').replaceChildren();
        root.querySelector('[data-orders-apply]').hidden = true;
        show(root,'detail',true); ['detail','returnable','returns'].forEach(function (name) { read(root,name,id); });
    }
    function prepareReturn(root) {
        var data = root._ordersDetail, available = root._ordersReturnable; if (!data || !available || !available.canRequest) return false;
        var form = part(root,'return').querySelector('form'), orderID = String(data.head.id), oldOrder = form.querySelector('[name="orderId"]').value;
        if (oldOrder === orderID && form.querySelector('[name="requestId"]').value && !form._ordersCompleted) { show(root,'return',true); return true; }
        form.querySelector('[name="orderId"]').value = orderID;
        form.querySelector('[name="requestId"]').value = global.WPSitePreview && global.WPSitePreview.active ? '' : global.crypto.randomUUID();
        form._ordersCompleted = false; form.removeAttribute('data-orders-dirty');
        form.querySelector('[name="reason"]').value = '';
        var error = form.querySelector('[data-write-status]'); if (error) { error.textContent = ''; error.hidden = true; }
        var list = form.querySelector('[data-orders-return-lines]'), template = form.querySelector('template[data-order-return-item]'); list.replaceChildren();
        var byID = new Map(); (available.items || []).forEach(function(item){byID.set(String(item.orderItemId),item);});
        (data.items || []).forEach(function (item) {
            var fact = byID.get(String(item.id)); if (!fact || fact.returnable <= 0) return;
            var row = template.content.firstElementChild.cloneNode(true);
            text(row,'[data-return-item-name]',item.productName); text(row,'[data-return-item-variant]',item.variantLabel || item.sku); text(row,'[data-return-item-max]',fact.returnable);
            row.querySelector('[name="orderItemId"]').value = String(item.id); row.querySelector('[name="quantity"]').max = String(fact.returnable); list.appendChild(row);
        });
        show(root,'return',true); return true;
    }
    function renderResult(root, data) {
        var currency = root._ordersDetail && root._ordersDetail.head.currency;
        text(root,'[data-orders-result-number]',data.returnNo); text(root,'[data-orders-result-amount]',money(data.refundAmount,currency)); show(root,'result',true);
    }
    global.WPSiteData.register('ordersList',renderList);
    global.WPSiteData.register('orderDetail',renderDetail);
    global.WPSiteData.register('orderReturnable',renderReturnable);
    global.WPSiteData.register('orderReturns',renderReturns);
    if (global.WBUI && global.WBUI.writeBase) global.WBUI.writeBase.registerResultHandler('commerce.orders',function(ctx){
        var root = owner(ctx.form); if (!root) return false;
        ctx.form._ordersCompleted = true; ctx.form.removeAttribute('data-orders-dirty'); renderResult(root,ctx.data || {});
        document.querySelectorAll('[data-site-orders]').forEach(function(other){refresh(other); if(other._ordersSelected){read(other,'returnable',other._ordersSelected);read(other,'returns',other._ordersSelected);}});
        return true;
    });
    document.addEventListener('click',function(event){
        var trigger = event.target.closest('[data-order-open], [data-orders-show], [data-orders-apply], [data-orders-prev], [data-orders-next], [data-orders-retry]'); if (!trigger) return;
        var root = owner(trigger); if (!root) return;
        if (trigger.hasAttribute('data-order-open')) { openOrder(root,trigger.getAttribute('data-order-id')); return; }
        if (trigger.hasAttribute('data-orders-show')) { show(root,trigger.getAttribute('data-orders-show'),true); return; }
        if (trigger.hasAttribute('data-orders-apply')) { prepareReturn(root); return; }
        if (trigger.hasAttribute('data-orders-retry')) { refresh(root._ordersRetryTarget || root); return; }
        var url = new URL(root.getAttribute('data-site-read'),global.location.href), limit = Number(url.searchParams.get('limit') || 10), offset = Number(url.searchParams.get('offset') || 0);
        url.searchParams.set('offset',String(Math.max(0,offset + (trigger.hasAttribute('data-orders-next') ? limit : -limit)))); root.setAttribute('data-site-read',url.pathname + url.search); refresh(root);
    });
    document.addEventListener('change',function(event){
        var target = event.target, root = owner(target); if (!root) return;
        if (target.hasAttribute('data-orders-filter')) { var url = new URL(root.getAttribute('data-site-read'),global.location.href); url.searchParams.set('status',target.value); url.searchParams.delete('offset'); root.setAttribute('data-site-read',url.pathname + url.search); refresh(root); }
        if (target.hasAttribute('data-return-select')) target.closest('.sky-orders-return-line').querySelectorAll('[name="orderItemId"], [name="quantity"]').forEach(function(input){input.disabled = !target.checked;});
    });
    document.addEventListener('input',function(event){var form=event.target.closest('form[data-write-handler="commerce.orders"]');if(form)form.setAttribute('data-orders-dirty','true');});
    document.addEventListener('site:data:error',function(event){
        var root=owner(event.target);if(!root)return;var code=event.detail && event.detail.status;
        if(code===401){root.querySelector('[data-orders-guest]').hidden=false;root.querySelectorAll('[data-orders-panel]').forEach(function(panel){panel.hidden=true;});text(root,'[data-site-status]','');}
        else if(code===404)text(event.target,'[data-site-status]',label(root,'NotFound'));
        else if(code===403)text(event.target,'[data-site-status]',label(root,'Forbidden'));
        root._ordersRetryTarget=event.target;root.querySelector('[data-orders-retry]').hidden=false;
    });
    document.addEventListener('site:action',function(event){var root=owner(event.target);if(root && event.detail && event.detail.instanceId===root.getAttribute('data-component-instance')){show(root,event.detail.part,true);if(event.detail.part==='list')refresh(root);}});
    document.addEventListener('site:data',function(event){
        var data=event.detail && event.detail.data;if(!event.target.hasAttribute('data-user-form') || !data || typeof data.authenticated!=='boolean' || data.authenticated===lastAuthenticated)return;
        lastAuthenticated=data.authenticated;document.querySelectorAll('[data-site-orders]').forEach(refresh);
    });
    if(global.WPSiteData.registerPreview){
        var sample={head:{id:1001,orderNo:'PREVIEW-20261029',status:'paid',currency:'CNY',subtotal:5800,total:5800,discountTotal:0,shippingTotal:0,taxTotal:0,createTime:1793232000000,shipName:'示例收件人',shipAddress:'预览地址'},items:[{id:101,productName:'示例商品',variantLabel:'默认规格',quantity:2,lineTotal:5800}],logs:[{toStatus:'paid',createTime:1793232000000}]};
        global.WPSiteData.registerPreview('ordersList',function(root,state){
            if(state==='unauthenticated'){root.querySelector('[data-orders-guest]').hidden=false;root.querySelectorAll('[data-orders-panel]').forEach(function(panel){panel.hidden=true;});clearStatus(root);return;}
            if(state==='service-error'||state==='forbidden'||state==='not-found'||state==='loading'){text(root,'[data-site-status]',label(root,state==='loading'?'Loading':state==='forbidden'?'Forbidden':state==='not-found'?'NotFound':'Unavailable'));return;}
            renderList(root,{list:state==='empty'?[]:[sample.head],total:state==='empty'?0:1});
            if(state==='validation-error'||state==='submitting'||state==='success'){
                root._ordersSelected='1001';renderDetail(root.querySelector('[data-order-read="detail"]'),sample);renderReturnable(root.querySelector('[data-order-read="returnable"]'),{canRequest:true,items:[{orderItemId:101,returnable:2}]});prepareReturn(root);
                if(state==='success')renderResult(root,{returnNo:'PREVIEW-RMA-001',refundAmount:2900});
                else{var form=part(root,'return').querySelector('form');text(form,'[data-write-status]',label(root,state==='submitting'?'Loading':'ReturnPlaceholder'));form.querySelector('[data-write-status]').hidden=false;}
            }
        });
        global.WPSiteData.registerPreview('orderDetail',function(host){renderDetail(host,sample);});
        global.WPSiteData.registerPreview('orderReturnable',function(host){renderReturnable(host,{canRequest:true,items:[{orderItemId:101,returnable:2}]});});
        global.WPSiteData.registerPreview('orderReturns',function(host){renderReturns(host,{list:[]});});
    }
})(window);
