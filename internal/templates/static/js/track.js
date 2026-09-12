/* track.js — 流量来源 / 广告归因采集（构建期无条件内联进每一页产物）。
 *
 * 读 UTM 家族、广告点击 id、referrer，按 Sourcebuster.js 的三条覆盖规则写 cookie：
 * UTM 与自然搜索永远覆盖；typein 永不覆盖；referral 仅在无会话时覆盖。
 * 下单时由服务端读这些 cookie 组装 orders.attribution（解析在 cart 模块）。
 *
 * 为什么在浏览器端：静态产物同一份字节发给所有访客，流量来源只有访客的浏览器看得见。
 * cookie 非 HttpOnly（本脚本读写）、SameSite=Lax（跨站 POST 不携带）。
 * 无 JS 时没有这些 cookie，订单归因落空对象 —— 采集是增强，不是下单前置。
 *
 * 改这里的 cookie 名要同步 Go 侧唯一的定义处：internal/module/runtimefragment/track_cookies.go
 */
(function () {
  'use strict';

  var C_SRC = 'gw_src';
  var C_FIRST = 'gw_first';
  var C_SESS = 'gw_sess';
  var C_TRAIL = 'gw_trail';
  var C_VIS = 'gw_vis';
  var PERSIST_DAYS = 180;
  var VISITOR_DAYS = 365;
  var MAX_TRAIL = 8;
  var MAX_TITLE = 60;
  var MAX_PATH = 120;

  var SEARCH = ['google.', 'bing.', 'yahoo.', 'baidu.', 'yandex.', 'duckduckgo.', 'ask.',
    'ecosia.', 'sogou.', 'so.com', 'naver.', 'qwant.', 'brave.', 'startpage.', '360.cn'];

  // 广告点击 id：只有点击 id、没有 utm 时按平台的默认值归类（与 Sourcebuster 的 gclid 规则一致）。
  var CLICK_IDS = [
    { key: 'gclid', platform: 'google', medium: 'cpc', campaign: 'google_cpc' },
    { key: 'gbraid', platform: 'google', medium: 'cpc', campaign: 'google_cpc' },
    { key: 'wbraid', platform: 'google', medium: 'cpc', campaign: 'google_cpc' },
    { key: 'fbclid', platform: 'facebook', medium: 'cpc', campaign: 'facebook_cpc' },
    { key: 'ttclid', platform: 'tiktok', medium: 'cpc', campaign: 'tiktok_cpc' },
    { key: 'msclkid', platform: 'bing', medium: 'cpc', campaign: 'bing_cpc' }
  ];

  function readCookie(name) {
    var parts = document.cookie ? document.cookie.split('; ') : [];
    for (var i = 0; i < parts.length; i++) {
      var idx = parts[i].indexOf('=');
      if (idx > 0 && parts[i].slice(0, idx) === name) return parts[i].slice(idx + 1);
    }
    return '';
  }

  function writeCookie(name, value, days) {
    var c = name + '=' + value + '; Path=/; SameSite=Lax';
    if (days) c += '; Max-Age=' + (days * 24 * 60 * 60);
    if (location.protocol === 'https:') c += '; Secure';
    document.cookie = c;
  }

  // 解析 "k=v&k=v"（值逐段 URL 编码）与构造它的逆操作。
  function parseKV(raw) {
    var out = {};
    if (!raw) return out;
    var s;
    try { s = decodeURIComponent(raw); } catch (e) { return out; }
    var parts = s.split('&');
    for (var i = 0; i < parts.length; i++) {
      var idx = parts[i].indexOf('=');
      if (idx > 0) out[parts[i].slice(0, idx)] = parts[i].slice(idx + 1);
    }
    return out;
  }

  // 只对值编码、不整体再编码：整体编码会让服务端必须解两次，少解一次的表现是「中文来源名变成 %E4%B8%AD」。
  function buildKV(obj) {
    var parts = [];
    for (var k in obj) {
      if (!Object.prototype.hasOwnProperty.call(obj, k)) continue;
      var v = obj[k];
      if (v === '' || v === null || v === undefined) continue;
      parts.push(k + '=' + encodeURIComponent(String(v)));
    }
    return parts.join('&');
  }

  // 设备类型：UA 优先（iPad 的 UA 才认得出平板），无 UA 时按视口宽度兜底。
  function deviceType() {
    var ua = navigator.userAgent || '';
    if (/iPad|Tablet|PlayBook|Silk/i.test(ua)) return 'tablet';
    if (/Mobi|Android|iPhone|iPod|Windows Phone/i.test(ua)) return 'mobile';
    if (!ua) {
      var w = window.innerWidth || 0;
      if (w && w < 768) return 'mobile';
      if (w && w < 1025) return 'tablet';
    }
    return 'desktop';
  }

  function hostOf(url) {
    try { return new URL(url, location.href).hostname.toLowerCase(); } catch (e) { return ''; }
  }

  function matchDomain(host, list) {
    for (var i = 0; i < list.length; i++) if (host.indexOf(list[i]) >= 0) return list[i];
    return '';
  }

  function nowSec() { return Math.floor(Date.now() / 1000); }

  // 本次页面的查询参数（键统一小写，容忍 UTM_SOURCE 这种写法）。
  function queryParams() {
    var out = {};
    var q = location.search;
    if (!q || q.length < 2) return out;
    var pairs = q.slice(1).split('&');
    for (var i = 0; i < pairs.length; i++) {
      var idx = pairs[i].indexOf('=');
      if (idx <= 0) continue;
      var k = pairs[i].slice(0, idx).toLowerCase();
      var v = pairs[i].slice(idx + 1);
      try { v = decodeURIComponent(v.replace(/\+/g, ' ')); } catch (e) { /* 保留原值 */ }
      if (!out[k]) out[k] = v;
    }
    return out;
  }

  // 判定本次触达；返回 null 表示站内跳转（不产生新来源）。
  function detect() {
    var q = queryParams();
    var ref = document.referrer || '';
    var refHost = ref ? hostOf(ref) : '';
    var selfHost = location.hostname.toLowerCase();
    var click = null;
    var i;

    for (i = 0; i < CLICK_IDS.length; i++) {
      if (q[CLICK_IDS[i].key]) {
        // 不复用常量对象：就地改它会污染后续判定。
        click = {
          key: CLICK_IDS[i].key, platform: CLICK_IDS[i].platform,
          medium: CLICK_IDS[i].medium, campaign: CLICK_IDS[i].campaign,
          value: q[CLICK_IDS[i].key]
        };
        break;
      }
    }

    if (q.utm_source || click) {
      return {
        t: 'utm',
        s: q.utm_source || (click ? click.platform : ''),
        m: q.utm_medium || (click ? click.medium : ''),
        c: q.utm_campaign || (click ? click.campaign : ''),
        n: q.utm_content || '', k: q.utm_term || '', i: q.utm_id || '',
        r: refHost, click: click
      };
    }
    if (refHost && refHost !== selfHost) {
      var isSearch = matchDomain(refHost, SEARCH);
      return {
        t: isSearch ? 'organic' : 'referral',
        s: refHost, m: isSearch ? 'organic' : 'referral',
        c: '', n: '', k: '', r: refHost
      };
    }
    // 站内跳转不算新来源，也不落地首触。
    if (refHost && refHost === selfHost) return null;
    return { t: 'typein', s: '(direct)', m: '(none)', c: '', n: '', k: '', r: '' };
  }

  // 覆盖规则（见文件头三条）。
  function shouldOverride(candidate, existing, hasSession) {
    if (!existing) return true;
    if (candidate.t === 'typein') return false;
    if (candidate.t === 'utm' || candidate.t === 'organic' || candidate.t === 'paid') return true;
    if (candidate.t === 'referral') return !hasSession;
    return false;
  }

  function touch() {
    var sess = parseKV(readCookie(C_SESS));
    var hasSession = !!sess.e;

    var pages = parseInt(sess.p, 10);
    if (isNaN(pages) || pages < 0) pages = 0;
    pages += 1;
    if (!sess.e) { sess.e = location.pathname; sess.st = String(nowSec()); }
    sess.p = String(pages);
    if (!sess.d) sess.d = deviceType();
    sess.sc = screen.width + 'x' + screen.height;
    // 不传 days 即会话 cookie（关掉浏览器就重新开始一次会话）。
    writeCookie(C_SESS, buildKV({ e: sess.e, p: sess.p, st: sess.st, d: sess.d, sc: sess.sc }), 0);

    // 访客计数跨会话持久（会话 cookie 数不清「第几次来」）。
    var vis = parseKV(readCookie(C_VIS));
    var visits = parseInt(vis.n, 10);
    if (isNaN(visits) || visits < 0) visits = 0;
    if (!hasSession) visits += 1;
    if (!vis.f) vis.f = String(nowSec());
    writeCookie(C_VIS, buildKV({ n: String(visits), f: vis.f }), VISITOR_DAYS);

    var det = detect();
    if (det) {
      var cur = parseKV(readCookie(C_SRC));
      if (shouldOverride(det, cur, hasSession)) {
        var next = {
          t: det.t, s: det.s, m: det.m, c: det.c, n: det.n, k: det.k, i: det.i,
          r: det.r, ts: String(nowSec())
        };
        if (det.click && det.click.value) { next['id_' + det.click.key] = det.click.value; next.cid = det.click.value; }
        writeCookie(C_SRC, buildKV(next), PERSIST_DAYS);
      }
    }

    // 首触：首触是 typein / referral 时，后来的 UTM 仍然覆盖它 ——
    //「先直接输网址逛了一圈，后来点广告进来下单」，广告才是这次转化的来源。
    var first = parseKV(readCookie(C_FIRST));
    if (det) {
      var strong = det.t === 'utm' || det.t === 'paid' || det.t === 'organic';
      var firstWeak = !first.t || first.t === 'typein' || first.t === 'referral';
      if (!first.t || (strong && firstWeak)) {
        var f = {
          t: det.t, s: det.s, m: det.m, c: det.c, n: det.n, k: det.k,
          r: det.r, rp: location.pathname, ts: String(nowSec())
        };
        if (det.click && det.click.value) f['id_' + det.click.key] = det.click.value;
        writeCookie(C_FIRST, buildKV(f), PERSIST_DAYS);
      }
    }

    // 轨迹：同路径只更新时间戳（刷新不该灌满轨迹），最多留最近 8 条。
    var trail = [];
    try {
      trail = JSON.parse(decodeURIComponent(readCookie(C_TRAIL)) || '[]');
      if (!(trail instanceof Array)) trail = [];
    } catch (e) { trail = []; }
    var path = location.pathname.slice(0, MAX_PATH);
    var title = (document.title || '').slice(0, MAX_TITLE);
    var ts = nowSec();
    var last = trail.length ? trail[trail.length - 1] : null;
    if (last && last.length >= 3 && last[0] === path) {
      last[1] = title;
      last[2] = ts;
    } else {
      trail.push([path, title, ts]);
    }
    while (trail.length > MAX_TRAIL) trail.shift();
    // 轨迹是 cookie，写爆了浏览器会静默丢掉整个 cookie —— 超限就砍最旧的几条。
    var encoded = '';
    while (trail.length > 0) {
      encoded = encodeURIComponent(JSON.stringify(trail));
      if (encoded.length <= 3500) break;
      trail.shift();
    }
    if (encoded) writeCookie(C_TRAIL, encoded, 0);
  }

  try { touch(); } catch (e) { /* 采集永不影响页面本身 */ }
})();
