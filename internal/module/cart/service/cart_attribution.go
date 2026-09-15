package cartservice

// cart_attribution.go — 访客追踪 cookie → 订单归因快照。
//
// 这是「采集链路」的收口端：track.js 在浏览器里按 Sourcebuster 的覆盖规则写 cookie，
// 下单那一刻由这里把 cookie 解释成订单要存的那份快照，然后冗余进 orders.attribution。
//
// 为什么必须在下单那一刻定格：cookie 会过期、UTM 参数会被随手改、访客下次来的来源
// 可能完全不同。订单要留下的是**下单时看到的那一份**，事后任何变动都不该改写历史订单。
//
// 全部 cookie 都缺席时返回 nil（列落 "{}"）：无 JS、禁用 cookie、后台代客下单都会
// 走到这里，那时的正确结果是「没有归因数据」，而不是一个装满空串的对象 ——
// 后者在分析里会被当成「有归因但都是空」，与「没有归因」是两件事。

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	cartdto "go_wp/internal/module/cart/dto"
	ordercontract "go_wp/internal/module/order/contract"
)

// buildAttribution 组装归因快照（auth：全部为空时返回 nil）。
func buildAttribution(c cartdto.TrackCookies, userAgent string, now time.Time) *ordercontract.Attribution {
	cur := parseTrackKV(c.Current)
	fst := parseTrackKV(c.First)
	sess := parseTrackKV(c.Session)
	vis := parseTrackKV(c.Visitor)
	trail := parseTrail(c.Trail, now)
	if len(cur) == 0 && len(fst) == 0 && len(sess) == 0 && len(trail) == 0 {
		return nil
	}

	attr := &ordercontract.Attribution{
		SourceType: kv(cur, "t"),
		Referrer:   kv(cur, "r"),
		UTM:        utmOf(cur),
		Ad: ordercontract.AdInfo{
			GCLID:   kv(cur, "id_gclid"),
			FBCLID:  kv(cur, "id_fbclid"),
			TTCLID:  kv(cur, "id_ttclid"),
			MSCLKID: kv(cur, "id_msclkid"),
			ClickID: kv(cur, "cid"),
		},
		Session: ordercontract.SessionInfo{
			Entry:           kv(sess, "e"),
			Pages:           atoiSafe(kv(sess, "p")),
			Count:           atoiSafe(kv(vis, "n")),
			StartTime:       unixRFC3339(kv(sess, "st")),
			DurationSeconds: durationFrom(kv(sess, "st"), now),
		},
		Device: ordercontract.DeviceInfo{
			// UA 由服务端从请求头取（客户端自报的 UA 可以随手改，而服务端拿到的是
			// 这次请求真正带过来的那个）；设备类型与屏幕由采集脚本给（只有浏览器知道）。
			Type:      kv(sess, "d"),
			UserAgent: strings.TrimSpace(userAgent),
			Screen:    kv(sess, "sc"),
		},
		First: ordercontract.FirstTouch{
			SourceType: kv(fst, "t"),
			Referrer:   kv(fst, "r"),
			UTM:        utmOf(fst),
			Landing:    kv(fst, "rp"),
			At:         unixRFC3339(kv(fst, "ts")),
		},
		// Landing 是「第一次到站的那一页」；本次会话的入口在 Session.Entry。
		// 多会话场景下两者不同，分开存正是为了不把它们混成一个概念。
		Landing: kv(fst, "rp"),
		Trail:   trail,
	}
	return attr
}

// utmOf 从 kv 取 UTM 家族。
func utmOf(m map[string]string) ordercontract.UTMInfo {
	return ordercontract.UTMInfo{
		Source:   kv(m, "s"),
		Medium:   kv(m, "m"),
		Campaign: kv(m, "c"),
		Content:  kv(m, "n"),
		Term:     kv(m, "k"),
		ID:       kv(m, "i"),
	}
}

// kv 取键值（不存在返回空串 —— 归因字段的空值就是「没有」，不需要区分「空」与「缺」）。
func kv(m map[string]string, key string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[key])
}

// parseTrackKV 解析 track.js 写的 "k=v&k=v" 串（值是逐段 URL 编码的）。
func parseTrackKV(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]string, 8)
	for _, seg := range strings.Split(raw, "&") {
		idx := strings.Index(seg, "=")
		if idx <= 0 {
			continue
		}
		key := seg[:idx]
		val, err := url.QueryUnescape(seg[idx+1:])
		if err != nil {
			// 解不开就留原值：归因是分析数据，不该为一个坏字符丢掉整条记录。
			val = seg[idx+1:]
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseTrail 解析浏览轨迹（JSON 数组，每项 [路径, 标题, unix 秒]）。
//
// 停留秒数由**相邻两条的时间差**推出：最后一条用「下单时刻 - 进入时刻」——
// 访客正好在下单页停留着，那一段停留恰恰是最有分析价值的一段。
func parseTrail(raw string, now time.Time) []ordercontract.TrailPage {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return nil
	}
	var rows [][]any
	if jerr := json.Unmarshal([]byte(decoded), &rows); jerr != nil {
		return nil
	}
	type entry struct {
		path  string
		title string
		at    int64
	}
	entries := make([]entry, 0, len(rows))
	for _, r := range rows {
		if len(r) < 3 {
			continue
		}
		p, _ := r[0].(string)
		t, _ := r[1].(string)
		ts, _ := r[2].(float64)
		if strings.TrimSpace(p) == "" {
			continue
		}
		entries = append(entries, entry{path: p, title: t, at: int64(ts)})
	}
	if len(entries) == 0 {
		return nil
	}
	out := make([]ordercontract.TrailPage, 0, len(entries))
	for i, e := range entries {
		var secs int
		if i+1 < len(entries) {
			secs = int(entries[i+1].at - e.at)
		} else {
			secs = int(now.Unix() - e.at)
		}
		if secs < 0 {
			secs = 0
		}
		out = append(out, ordercontract.TrailPage{
			URL:     e.path,
			Title:   e.title,
			At:      unixRFC3339(strconv.FormatInt(e.at, 10)),
			Seconds: secs,
		})
	}
	return out
}

// unixRFC3339 unix 秒 → RFC3339（UTC）。
//
// 用 UTC 而不是本地时区：订单归因会跨时区做报表，存一个「本地时间」等于把
// 服务器时区偷偷写进了历史数据里，而那个时区将来一定会变。
func unixRFC3339(s string) string {
	sec := atoi64Safe(s)
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// durationFrom 从起始 unix 秒算到 now 的秒数。
func durationFrom(start string, now time.Time) int {
	sec := atoi64Safe(start)
	if sec <= 0 {
		return 0
	}
	d := int(now.Unix() - sec)
	if d < 0 {
		return 0
	}
	return d
}

// atoiSafe 宽松解析（解析不出来当 0）。
func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// atoi64Safe 宽松解析（解析不出来当 0）。
func atoi64Safe(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
