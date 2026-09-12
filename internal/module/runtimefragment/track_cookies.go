package runtimefragment

// track_cookies.go — 采集脚本写的 cookie 名（浏览器侧与服务端解析器之间的约定）。
//
// 名字的**写方**是构建期内联进产物的 track.js（internal/templates/static/js/track.js），
// 读方是这里。跨语言的字面量没办法合成一处，所以规则是：Go 侧只在这个文件里定义一次，
// 改名字时两边同时改。散在各个能力里各写一遍，就一定会出现「改了一个漏了另一个」。
//
// 归因 cookie 全部缺席时（无 JS、禁用 cookie、后台代客下单），订单归因落空对象 ——
// 采集是增强，不是下单的前置条件。

import (
	cartdto "go_wp/internal/module/cart/dto"
)

const (
	// trackCookieCurrent 本次会话的触达来源（对标 Sourcebuster 的 sbjs_current）。
	trackCookieCurrent = "gw_src"
	// trackCookieFirst 首次触达（对标 sbjs_first；WooCommerce 核心只映射了 current 那份）。
	trackCookieFirst = "gw_first"
	// trackCookieSession 会话事实：入口页 / 页数 / 开始时间 / 设备 / 屏幕。
	trackCookieSession = "gw_sess"
	// trackCookieTrail 浏览轨迹（下单前看过哪几页，按时间升序）。
	trackCookieTrail = "gw_trail"
	// trackCookieVisitor 访客计数：第几次来、第一次到站的时间（跨会话持久）。
	trackCookieVisitor = "gw_vis"
)

// trackCookiesOf 从请求里取出五个归因 cookie 的原始值。
func trackCookiesOf(r *Request) cartdto.TrackCookies {
	if r == nil || len(r.Cookies) == 0 {
		return cartdto.TrackCookies{}
	}
	return cartdto.TrackCookies{
		Current: r.Cookies[trackCookieCurrent],
		First:   r.Cookies[trackCookieFirst],
		Session: r.Cookies[trackCookieSession],
		Trail:   r.Cookies[trackCookieTrail],
		Visitor: r.Cookies[trackCookieVisitor],
	}
}
