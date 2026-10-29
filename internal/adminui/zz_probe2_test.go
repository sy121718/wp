package adminui

import (
	"testing"

	analyticsdto "go_wp/internal/module/analytics/dto"
	orderdto "go_wp/internal/module/order/dto"
	maildto "go_wp/internal/module/mail/dto"
)

func TestProbePageTitles(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"analytics", &analyticsdto.AnalyticsPageResp{Title: "访问统计"}},
		{"orderList", &orderdto.OrderListPageResp{Title: "订单"}},
		{"pendingShip", &orderdto.PendingShipPageView{Title: "待发货"}},
	}
	for _, c := range cases {
		m := ViewMap(c.v)
		_, lower := m["title"]
		_, upper := m["Title"]
		t.Logf("%s: title=%v Title=%v keys=%d", c.name, lower, upper, len(m))
	}
	_ = maildto.TemplatesPageResp{}
}
