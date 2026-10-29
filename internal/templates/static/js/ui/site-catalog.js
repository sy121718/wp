/* 列表和搜索只消费JSON；作者模板、交互状态与展示格式留在前端。 */
(function (global) {
    'use strict';
    var filters = ['categoryId','categoryIds','categoryMode','brandId','brandIds','brandMode','tagId','tagIds','tagMode','onSale','minPrice','maxPrice','minRating'];
    function filterKey(key) { return filters.indexOf(key) >= 0 || /^option\.[A-Za-z0-9_-]{1,64}$/.test(key); }
    function safeURL(value, media) {
        if (typeof value !== 'string' || !value.trim()) return '';
        try { var u = new URL(value, global.location.href); return /^https?:$/.test(u.protocol) && (!media || /^https?:\/\//i.test(value)) ? u.href : ''; } catch (_) { return ''; }
    }
    function money(value, currency) {
        if (!Number.isSafeInteger(value)) return '';
        var digits = String(Math.abs(value)).padStart(3, '0');
        return (currency || '') + (value < 0 ? '-' : '') + digits.slice(0,-2) + '.' + digits.slice(-2);
    }
    function field(item, path) {
        var key = String(path || '').replace(/^(item|product)\./, '');
        if (key === 'defaultImage') return item.images && item.images[0];
        if (key === 'seoTitle') return item.name;
        if (key === 'seoDescription') return item.subtitle;
        if (key === 'minPrice' || key === 'priceRange') return item.price;
        return Object.prototype.hasOwnProperty.call(item,key) ? item[key] : '';
    }
    function base(root) {
        if (!root._catalogBase) root._catalogBase = root.getAttribute('data-site-read');
        return new URL(root._catalogBase,global.location.href);
    }
    function defaults(root) {
        var q=new URLSearchParams(root.getAttribute('data-catalog-default-query') || '');
        ['category','brand','tag'].forEach(function(key){if(!q.has(key+'Ids') && q.has(key+'Id')) q.set(key+'Ids',q.get(key+'Id'));});
        return q;
    }
    function current(root) {
        var q=defaults(root), locationQuery=new URLSearchParams(global.location.search);
        locationQuery.forEach(function(value,key) {
            if (filterKey(key) || ['q','page','pageSize','orderBy','layout','columns'].indexOf(key)>=0) q.set(key,value);
        });
        ['category','brand','tag'].forEach(function(key){if(locationQuery.has(key+'Id') && !locationQuery.has(key+'Ids')) q.set(key+'Ids',locationQuery.get(key+'Id'));});
        return q;
    }
    function prepareCatalog(root) {
        var u=base(root), q=current(root), filter={};
        try { filter=JSON.parse(u.searchParams.get('filter') || '{}') || {}; } catch (_) {}
        q.forEach(function(value,key) { if(filterKey(key)) { if(value) filter[key]=value; else delete filter[key]; } });
        u.searchParams.set('filter',JSON.stringify(filter));
        if(q.get('orderBy')) u.searchParams.set('sort',q.get('orderBy'));
        ['q','page'].forEach(function(key){if(q.has(key)) u.searchParams.set(key,q.get(key));});
        if(Number(q.get('pageSize'))>0) u.searchParams.set('pageSize',q.get('pageSize'));
        root.setAttribute('data-site-read',u.pathname+u.search);
        syncControls(root);
        var items=root.querySelector('.sky-product-list-items');
        if(items) {
            items.style.display=q.get('layout')==='list'?'flex':'grid';
            items.style.flexDirection=q.get('layout')==='list'?'column':'';
            var columns=q.get('columns');
            if(/^[234]$/.test(columns)) items.style.setProperty('--catalog-columns','repeat('+columns+',minmax(0,1fr))'); else items.style.removeProperty('--catalog-columns');
        }
    }
    function action(root, link) {
        var seed=defaults(root);
        var target=new URLSearchParams((link.getAttribute('data-catalog-query')||'').replace(/^\?/,''));
        return {seed:seed,target:target};
    }
    function selection(action,key) {
        var a=(action.seed.get(key)||'').split(',').filter(Boolean), b=(action.target.get(key)||'').split(',').filter(Boolean);
        return a.filter(function(x){return b.indexOf(x)<0;}).concat(b.filter(function(x){return a.indexOf(x)<0;}));
    }
    function changeQuery(root, link) {
        var q=current(root), a=action(root,link), explicitPage=a.target.has('page') && a.target.get('page');
        a.target.forEach(function(value,key) {
            if(/^(categoryIds|brandIds|tagIds|option\.)/.test(key)) {
                var toggles=selection(a,key), selected=(q.get(key)||'').split(',').filter(Boolean);
                var remove=toggles.length>0 && toggles.every(function(x){return selected.indexOf(x)>=0;});
                if(key==='categoryIds' && root.getAttribute('data-catalog-category-single')==='true') selected=remove?[]:toggles;
                else if(!toggles.length) selected=value.split(',').filter(Boolean);
                else toggles.forEach(function(x){ var at=selected.indexOf(x); if(remove && at>=0) selected.splice(at,1); else if(!remove && at<0) selected.push(x); });
                q.set(key,selected.join(','));
            } else q.set(key,value);
        });
        if(!explicitPage) q.set('page','1');
        return q;
    }
    function syncControls(root) {
        var q=current(root);
        root.querySelectorAll('[data-catalog-query]').forEach(function(link){
            var a=action(root,link), active=true, count=0;
            a.target.forEach(function(value,key){if(key==='page') return;count++;
                if(/^(categoryIds|brandIds|tagIds|option\.)/.test(key)) {var chosen=(q.get(key)||'').split(','), values=selection(a,key);active=active && values.length>0 && values.every(function(x){return chosen.indexOf(x)>=0;});}
                else active=active && (q.get(key)||'')===value;
            });
            active=active && count>0;link.classList.toggle('is-active',active);
            if(active) link.setAttribute('aria-current','true');else link.removeAttribute('aria-current');
            if(link.hasAttribute('aria-checked')) {link.setAttribute('aria-checked',active?'all':'none');var box=link.querySelector('input');if(box) box.checked=active;}
        });
    }
    function navigate(root, q, push) {
        var u=new URL(global.location.href);
        q.forEach(function(value,key){u.searchParams.set(key,value);});
        if(push) global.history.pushState(null,'',u.pathname+u.search+u.hash);
        return global.WPSiteData.refresh(root);
    }
    function catalog(root,data) {
        var host=root.querySelector('[data-catalog-results]'), template=root.querySelector('template[data-catalog-card]');
        if(!host || !template || !data || !Array.isArray(data.items)) return;
        var list=global.document.createElement('ul'); list.className='sky-product-list-items';
        data.items.forEach(function(item) {
            var row=template.content.firstElementChild.cloneNode(true), href=safeURL(item.url,false), currency=root.getAttribute('data-catalog-currency');
            row.querySelectorAll('[data-catalog-value]').forEach(function(node){var value=field(item,node.getAttribute('data-catalog-value'));node.textContent=typeof value==='string'||typeof value==='number'?String(value):'';});
            row.querySelectorAll('[data-catalog-link]').forEach(function(node){if(href) node.href=href; else node.removeAttribute('href');});
            row.querySelectorAll('[data-catalog-image]').forEach(function(node){var value=field(item,node.getAttribute('data-catalog-image')), image=safeURL(Array.isArray(value)?value[0]:value,true);if(image) {node.src=image;node.alt=String(field(item,node.getAttribute('data-catalog-alt'))||item.name||'');} else node.parentElement.remove();});
            row.querySelectorAll('[data-catalog-price]').forEach(function(node){var path=node.getAttribute('data-catalog-price'), value=field(item,path), formatted=money(value,currency);if(/\.priceRange$/.test(path) && Number.isSafeInteger(item.maxPrice) && item.maxPrice!==item.price) formatted+=' – '+money(item.maxPrice,currency);node.textContent=formatted;node.hidden=!formatted;});
            row.querySelectorAll('[data-catalog-tags]').forEach(function(node){var tags=field(item,node.getAttribute('data-catalog-tags'));if(Array.isArray(tags)) tags.forEach(function(value){var tag=global.document.createElement('li');tag.className='sky-product-list-tag';tag.textContent=String(value);node.appendChild(tag);});});
            list.appendChild(row);
        });
        host.replaceChildren(list);
        if(data.items.length===0) {var empty=global.document.createElement('p');empty.className='sky-product-list-empty';empty.textContent=root.getAttribute('data-catalog-empty')||'';host.replaceChildren(empty);}
        var old=root.querySelector('[data-catalog-initial-pager]');if(old) old.hidden=true;
        var pager=root.querySelector('[data-catalog-pager]');
        if(pager) {
            pager.replaceChildren();pager.className='sky-product-list-pager';pager.hidden=data.total<=data.pageSize && data.page<=1;
            function pageLink(page,label) {var a=global.document.createElement('a'),q=current(root);q.set('page',String(page));a.className='sky-product-list-page';a.href='?'+q.toString();a.setAttribute('data-catalog-page',String(page));a.textContent=label;pager.appendChild(a);}
            if(data.page>1) pageLink(data.page-1,root.getAttribute('data-catalog-prev'));
            var count=global.document.createElement('span');count.className='sky-product-list-page-current';count.setAttribute('aria-current','page');count.textContent=String(data.page)+' / '+String(Math.max(1,Math.ceil(data.total/data.pageSize)));pager.appendChild(count);
            if(data.page*data.pageSize<data.total) pageLink(data.page+1,root.getAttribute('data-catalog-next'));
        }
        var status=root.querySelector('[data-site-status]');if(status) status.textContent='';
        prepareCatalog(root);
    }
    function prepareSearch(root) {
        var u=base(root), input=root.querySelector('[name="q"]');
        if(!root._searchReady) {if(input) input.value=new URLSearchParams(global.location.search).get('q')||'';root._searchReady=true;}
        u.searchParams.set('q',input?input.value:'');root.setAttribute('data-site-read',u.pathname+u.search);
    }
    function search(root,data) {
        var host=root.querySelector('[data-search-results]'), template=root.querySelector('template[data-search-hit]');
        if(!host || !template || !data) return;
        var list=global.document.createElement('ul');list.className='sky-search-list';
        (data.articles||[]).concat(data.products||[]).forEach(function(hit){
            var href=safeURL(hit.url,false);if(!href) return;
            var row=template.content.firstElementChild.cloneNode(true),title=row.querySelector('.sky-search-title'),excerpt=row.querySelector('.sky-search-excerpt');
            title.href=href;title.textContent=String(hit.title||hit.name||'');
            var raw=String(hit.excerpt||hit.subtitle||'').trim(), chars=Array.from(raw);excerpt.textContent=chars.slice(0,120).join('')+(chars.length>120?'…':'');excerpt.hidden=!raw;
            list.appendChild(row);
        });
        host.replaceChildren(list);
        var input=root.querySelector('[name="q"]'),query=input?input.value.trim():'',status=root.querySelector('[data-site-status]');
        if(status) status.textContent=list.childElementCount?'':(query?(root.getAttribute('data-search-no-results')||'').replace('%s',query):root.getAttribute('data-search-empty')||'');
    }
    global.WPSiteData.register('catalog',catalog,prepareCatalog);
    global.WPSiteData.register('search',search,prepareSearch);
    global.document.addEventListener('click',function(event){
        var link=event.target.closest('[data-catalog-query],[data-catalog-page]');if(!link || event.button!==0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        var root=link.closest('[data-site-catalog]');if(!root) return;
        event.preventDefault();var q=link.hasAttribute('data-catalog-page')?current(root):changeQuery(root,link);
        if(link.hasAttribute('data-catalog-page')) q.set('page',link.getAttribute('data-catalog-page'));
        navigate(root,q,true);
    });
    global.document.addEventListener('submit',function(event){
        var form=event.target;
        if(form.hasAttribute('data-site-search-form')) {event.preventDefault();var root=form.closest('[data-site-search]'),u=new URL(global.location.href);u.searchParams.set('q',form.querySelector('[name="q"]').value);global.history.pushState(null,'',u.pathname+u.search+u.hash);global.WPSiteData.refresh(root);}
        if(form.hasAttribute('data-catalog-price-form')) {event.preventDefault();var catalogRoot=form.closest('[data-site-catalog]'),q=current(catalogRoot);new FormData(form).forEach(function(value,key){if(filterKey(key)) q.set(key,String(value));});q.set('page','1');navigate(catalogRoot,q,true);}
    });
    global.addEventListener('popstate',function(){global.document.querySelectorAll('[data-site-catalog],[data-site-search]').forEach(function(root){if(root.hasAttribute('data-site-search')) root._searchReady=false;global.WPSiteData.refresh(root);});});
})(window);
