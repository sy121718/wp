package analyticsservice

import (
	"errors"
	"testing"
	"time"

	analyticsdto "go_wp/internal/module/analytics/dto"
)

func fixedNow() time.Time { return time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC) }

func newTestService() *Service {
	s := NewService(nil, "test-pepper")
	s.now = fixedNow
	return s
}

func TestSanitizeTrackPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/about", "/about"},
		{"  /about  ", "/about"},
		{"/product?id=1", "/product"},
		{"/product#reviews", "/product"},

		{"about", ""},
		{"", ""},
		{"//evil.example.com/x", "//evil.example.com/x"},
	}
	for _, c := range cases {
		if got := sanitizeTrackPath(c.in); got != c.want {
			t.Errorf("sanitizeTrackPath(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
	long := "/" + string(make([]byte, maxPathLen+50))
	if got := sanitizeTrackPath(long); len(got) != maxPathLen {
		t.Errorf("超长路径应截断到 %d，实际 %d", maxPathLen, len(got))
	}
}

func TestUAClass(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Mobile/15E148", "mobile"},
		{"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X)", "tablet"},
		{"Mozilla/5.0 (Android 14; Mobile)", "mobile"},
		{"Mozilla/5.0 (X11; Linux x86_64) Chrome/120", "desktop"},
		{"Googlebot/2.1 (+http://www.google.com/bot.html) Mobile", "bot"},
		{"curl/8.4.0", "bot"},
	}
	for _, c := range cases {
		if got := uaClass(c.in); got != c.want {
			t.Errorf("uaClass(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeReferrer(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"example.com", "example.com"},
		{"https://Example.com/path?x=1", "example.com"},
		{"Example.COM/path", "example.com"},
	}
	for _, c := range cases {
		if got := normalizeReferrer(c.in); got != c.want {
			t.Errorf("normalizeReferrer(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestBuildEntityRejectsForeignInput(t *testing.T) {
	s := newTestService()
	bad := []*analyticsdto.CollectReq{
		{ProjectID: "not-a-uuid", Path: "/a"},
		{ProjectID: "", Path: "/a"},
		{ProjectID: "2f1a4c0e-0000-4000-8000-000000000001", Path: "about"},
		{ProjectID: "2f1a4c0e-0000-4000-8000-000000000001", Path: ""},
	}
	for _, req := range bad {
		if _, ok := s.buildEntity(req); ok {
			t.Errorf("应丢弃的请求却被接受: %+v", req)
		}
	}
	good := &analyticsdto.CollectReq{
		ProjectID: "2f1a4c0e-0000-4000-8000-000000000001",
		Path:      "/about?utm_source=x",
		Lang:      "zh-CN",
		Referrer:  "https://ref.example.com/x",
		Session:   "s1712345678",
		Visitor:   "v1700000000-3",
		IP:        "203.0.113.7",
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) Chrome/120",
	}
	e, ok := s.buildEntity(good)
	if !ok {
		t.Fatalf("合法请求被丢弃: %+v", good)
	}
	if e.Path != "/about" {
		t.Errorf("路径应截掉查询串，实际 %q", e.Path)
	}
	if e.ReferrerHost != "ref.example.com" {
		t.Errorf("来源应只留域名，实际 %q", e.ReferrerHost)
	}
	if e.UAClass != "desktop" {
		t.Errorf("UA 分类应为 desktop，实际 %q", e.UAClass)
	}

	for name, val := range map[string]string{"ip": e.IPHash, "session": e.SessionID, "visitor": e.VisitorHash} {
		if val == "" {
			t.Errorf("%s 应已哈希落库，实际为空", name)
		}
		for _, raw := range []string{good.IP, good.Session, good.Visitor} {
			if val == raw {
				t.Errorf("%s 存了明文 %q", name, raw)
			}
		}
	}
}

func TestHashAnon(t *testing.T) {
	s := newTestService()
	if got := s.hashAnon(""); got != "" {
		t.Errorf("空值应得空串，实际 %q", got)
	}
	if s.hashAnon("a") != s.hashAnon("a") {
		t.Error("同一输入两次哈希不一致（不可复现的计数去重）")
	}
	if s.hashAnon("a") == s.hashAnon("b") {
		t.Error("不同输入撞哈希")
	}

	other := NewService(nil, "other-pepper")
	if s.hashAnon("same") == other.hashAnon("same") {
		t.Error("不同 pepper 得到同一哈希")
	}
}

func TestCollectIsSilent(t *testing.T) {
	s := newTestService()
	if err := s.Collect(nil, nil); err != nil {
		t.Errorf("nil 请求应静默返回 nil，实际 %v", err)
	}
	if err := s.Collect(nil, &analyticsdto.CollectReq{ProjectID: "bad"}); err != nil {
		t.Errorf("坏请求应静默返回 nil，实际 %v", err)
	}
}

func TestNormalizeWindow(t *testing.T) {
	s := newTestService()

	from, to, err := s.normalizeWindow("", "")
	if err != nil {
		t.Fatalf("默认窗口报错: %v", err)
	}
	if !to.Equal(fixedNow().AddDate(0, 0, 1)) {
		t.Errorf("默认结束应为明天零点（含今天），实际 %s", to)
	}

	if !from.Equal(to.AddDate(0, 0, -defaultRangeDays)) {
		t.Errorf("默认起始应为结束前 %d 天，实际 %s（结束 %s）", defaultRangeDays, from, to)
	}
	if got := to.AddDate(0, 0, -1); !from.AddDate(0, 0, defaultRangeDays-1).Equal(got) {
		t.Errorf("默认窗口应含今天在内的 %d 个自然日，实际 [%s, %s)", defaultRangeDays, from, to)
	}

	from, to, err = s.normalizeWindow("2026-01-01", "2026-01-10")
	if err != nil {
		t.Fatalf("显式窗口报错: %v", err)
	}
	if !from.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("显式窗口换算错误: [%s, %s)", from, to)
	}

	_, to, err = s.normalizeWindow("2026-01-01", "2099-01-01")
	if err != nil {
		t.Fatalf("未来结束日期应收敛而不是报错: %v", err)
	}
	if !to.Equal(fixedNow().AddDate(0, 0, 1)) {
		t.Errorf("未来结束日期未收敛，实际 %s", to)
	}

	if _, _, err = s.normalizeWindow("2026-02-01", "2026-01-01"); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("起止颠倒应报 ErrInvalidRange，实际 %v", err)
	}

	if _, _, err = s.normalizeWindow("2000-01-01", "2026-01-15"); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("超长跨度应报 ErrInvalidRange，实际 %v", err)
	}

	if _, _, err = s.normalizeWindow("not-a-date", ""); err != nil {
		t.Errorf("非法日期应回退默认窗口，实际 %v", err)
	}
}

func TestNormalizePathPaging(t *testing.T) {
	if page, limit := normalizePathPaging(0, 0); page != 1 || limit != defaultPathLimit {
		t.Errorf("默认分页应为 (1, %d)，实际 (%d, %d)", defaultPathLimit, page, limit)
	}
	if page, limit := normalizePathPaging(-5, 9999); page != 1 || limit != maxPathLimit {
		t.Errorf("越界分页应收敛为 (1, %d)，实际 (%d, %d)", maxPathLimit, page, limit)
	}
}
