package runtimefragment

// user_account_forms_test.go — 账号中心四个片段的端点级测试。
//
// 这一层守的是四件容易静默失效的事：
//   · **未登录必须得到引导**，而不是一个提交必然失败的表单，也不是 401（HTMX 不替换 401）；
//   · 渲染出来的是**本人的**数据 —— 收窄端口拿到的 userID 只能来自会话；
//   · **会话令牌绝不能出现在输出里**（它能换一个已登录会话）；
//   · 端口未接入时降级为可见文案，而不是空表单（空白的「资料」会让访客以为资料被清空了）。

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
)

// fakeVisitorAccount 假的账号读取端口（记录收到的 userID 与令牌）。
type fakeVisitorAccount struct {
	account  *userdto.AccountResp
	sessions []*userdto.SessionItem
	err      error

	gotID    uint64
	gotToken string
}

func (f *fakeVisitorAccount) AccountOf(_ context.Context, userID uint64) (*userdto.AccountResp, error) {
	f.gotID = userID
	if f.err != nil {
		return nil, f.err
	}
	return f.account, nil
}

func (f *fakeVisitorAccount) SessionsOf(_ context.Context, userID uint64, currentToken string) ([]*userdto.SessionItem, error) {
	f.gotID = userID
	f.gotToken = currentToken
	if f.err != nil {
		return nil, f.err
	}
	return f.sessions, nil
}

// withAccountPort 临时替换账号端口，用例结束后恢复（包级变量，测试不并行）。
func withAccountPort(t *testing.T, p usercontract.VisitorAccountPort) {
	t.Helper()
	visitorAccountPort = p
	t.Cleanup(func() { visitorAccountPort = nil })
}

// callAccountForm 直接调片段端点，返回响应体（注入访客身份 / CSRF token / 会话令牌）。
func callAccountForm(t *testing.T, typeName, query string, visitorID uint64, csrf, visitorToken string) string {
	t.Helper()
	return callAccountFormResp(t, typeName, query, visitorID, csrf, visitorToken).Body.String()
}

// callAccountFormResp 同上，但要拿到状态码（失败路径必须能分辨「空片段」与「报错」）。
func callAccountFormResp(t *testing.T, typeName, query string, visitorID uint64, csrf, visitorToken string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/_fragments/"+typeName+"?"+query, nil)
	c.Params = gin.Params{{Key: "type", Value: typeName}}
	if visitorID != 0 {
		c.Set(usercontract.VisitorContextKey, visitorID)
	}
	if csrf != "" {
		c.Set(usercontract.VisitorCSRFContextKey, csrf)
	}
	if visitorToken != "" {
		c.Set(usercontract.VisitorTokenContextKey, visitorToken)
	}
	FragmentEndpoint(c)
	return w
}

// TestAccountFormsGuideGuest 未登录：四个形态都给引导，都不渲染表单。
func TestAccountFormsGuideGuest(t *testing.T) {
	for _, name := range []string{
		accountFragmentProfile, accountFragmentPreference,
		accountFragmentPassword, accountFragmentSessions,
	} {
		t.Run(name, func(t *testing.T) {
			body := callAccountForm(t, name, "projectId=proj-1", 0, "csrf-1", "")
			if !strings.Contains(body, "请先登录") {
				t.Fatalf("未登录应给引导；实际：%s", body)
			}
			if strings.Contains(body, "<form") {
				t.Fatalf("未登录不该渲染任何表单（提交必然失败）；实际：%s", body)
			}
		})
	}
}

// TestAccountProfileRendersOwnAccount 已登录：渲染本人资料，且 userID 取自会话。
func TestAccountProfileRendersOwnAccount(t *testing.T) {
	port := &fakeVisitorAccount{account: &userdto.AccountResp{
		Username: "alice", Email: "a@b.c", Nickname: "小爱", Gender: 2,
		PageSize: 20, ProfileVisibility: "members", EmailNotify: true,
		RegisteredAtText: "2026-01-01 10:00:00",
	}}
	withAccountPort(t, port)

	body := callAccountForm(t, accountFragmentProfile, "projectId=proj-1&next=%2Fmy-account", 42, "csrf-9", "tok-secret")
	for _, want := range []string{
		`action="/user/account/profile"`,
		`name="csrf_token" value="csrf-9"`,
		`value="小爱"`,
		`name="next" value="/my-account"`,
		"alice",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("资料片段缺少 %q；实际：%s", want, body)
		}
	}
	if port.gotID != 42 {
		t.Fatalf("端口应收到会话里的 userID 42，实际 %d", port.gotID)
	}
	if strings.Contains(body, "tok-secret") {
		t.Fatal("会话令牌绝不能出现在片段输出里（它能换一个已登录会话）")
	}
}

// TestAccountPreferenceRendersOwnPreference 偏好表单：勾选态与提交目标。
func TestAccountPreferenceRendersOwnPreference(t *testing.T) {
	withAccountPort(t, &fakeVisitorAccount{account: &userdto.AccountResp{
		PageSize: 50, ProfileVisibility: "private", Timezone: "Asia/Shanghai",
		EmailNotify: true, SmsNotify: false, ShowOnline: true,
	}})

	body := callAccountForm(t, accountFragmentPreference, "projectId=proj-1", 7, "csrf-7", "")
	for _, want := range []string{
		`action="/user/account/preference"`,
		`name="pageSize" min="1" max="200" value="50"`,
		`value="private" selected`,
		`value="Asia/Shanghai"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("偏好片段缺少 %q；实际：%s", want, body)
		}
	}
	// EmailNotify=true 与 SmsNotify=false 必须渲染出不同的勾选态。
	if !strings.Contains(body, `name="emailNotify" value="1" checked`) {
		t.Fatalf("已开启的邮件通知应勾选；实际：%s", body)
	}
	if !strings.Contains(body, `name="smsNotify" value="1">`) {
		t.Fatalf("未开启的短信通知不该勾选；实际：%s", body)
	}
}

// TestAccountSessionsMarksCurrentDevice 登录设备：当前设备不给「踢出」，且判定用令牌原值。
func TestAccountSessionsMarksCurrentDevice(t *testing.T) {
	port := &fakeVisitorAccount{sessions: []*userdto.SessionItem{
		{ID: 1, UserAgent: "当前这台浏览器", Current: true, CreatedText: "2026-01-01 10:00:00"},
		{ID: 2, UserAgent: "别的浏览器", LastActiveText: "刚刚", CreatedText: "2026-01-02 10:00:00"},
	}}
	withAccountPort(t, port)

	body := callAccountForm(t, accountFragmentSessions, "projectId=proj-1", 42, "csrf-9", "tok-secret")
	if !strings.Contains(body, "当前设备") {
		t.Fatalf("应标出当前设备；实际：%s", body)
	}
	if !strings.Contains(body, `value="2"`) {
		t.Fatalf("非当前设备应有踢出按钮；实际：%s", body)
	}
	if strings.Contains(body, `value="1"`) {
		t.Fatalf("当前设备不该有踢出按钮（那个操作叫退出登录）；实际：%s", body)
	}
	if !strings.Contains(body, `action="/user/account/sessions/revoke-others"`) {
		t.Fatalf("应提供「退出其它设备」；实际：%s", body)
	}
	if port.gotToken != "tok-secret" {
		t.Fatalf("当前设备判定需要会话令牌原值，端口实际收到 %q", port.gotToken)
	}
	if strings.Contains(body, "tok-secret") {
		t.Fatal("会话令牌绝不能出现在片段输出里")
	}
}

// TestAccountSessionsEmpty 没有会话记录时给一句说明，而不是空列表。
func TestAccountSessionsEmpty(t *testing.T) {
	withAccountPort(t, &fakeVisitorAccount{})
	body := callAccountForm(t, accountFragmentSessions, "projectId=proj-1", 42, "csrf-9", "")
	if !strings.Contains(body, "没有其它登录设备") {
		t.Fatalf("空列表应给说明；实际：%s", body)
	}
}

// TestAccountFormsDegradeWithoutPort 端口未接入：可见文案，不是空表单也不是 500。
func TestAccountFormsDegradeWithoutPort(t *testing.T) {
	withAccountPort(t, nil)
	for _, name := range []string{accountFragmentProfile, accountFragmentPreference, accountFragmentSessions} {
		t.Run(name, func(t *testing.T) {
			body := callAccountForm(t, name, "projectId=proj-1", 42, "csrf-1", "")
			if !strings.Contains(body, "暂不可用") {
				t.Fatalf("端口缺失时应给可见提示；实际：%s", body)
			}
			if strings.Contains(body, "<form") {
				t.Fatalf("端口缺失时不该渲染空表单；实际：%s", body)
			}
		})
	}
	// 改密码不读账号，端口缺失也照样渲染（它只需要 CSRF token）。
	body := callAccountForm(t, accountFragmentPassword, "projectId=proj-1", 42, "csrf-1", "")
	if !strings.Contains(body, `action="/user/account/password"`) {
		t.Fatalf("改密码片段不依赖账号端口，应正常渲染；实际：%s", body)
	}
}

// TestAccountPasswordFormNeverCarriesNext 改密码刻意不回跳：全部会话都被撤销了。
//
// 带上 next 会让「保存成功后回作者页」这件事在一个已经登出的会话上发生 ——
// 用户看到的是「提交完跳到我的账号页，然后被踢去登录页」。
func TestAccountPasswordFormNeverCarriesNext(t *testing.T) {
	body := callAccountForm(t, accountFragmentPassword, "projectId=proj-1&next=%2Fmy-account", 42, "csrf-1", "")
	if strings.Contains(body, "name=\"next\"") {
		t.Fatalf("改密码片段不该带回跳隐藏域；实际：%s", body)
	}
}

// TestAccountFormsWarnWithoutCSRF 没有 CSRF token 时必须能看出来。
//
// 渲染一个必然 403 的表单是最糟的形态：用户填完点提交，得到一句「CSRF 校验失败」，
// 而他根本不知道发生了什么。
func TestAccountFormsWarnWithoutCSRF(t *testing.T) {
	withAccountPort(t, &fakeVisitorAccount{account: &userdto.AccountResp{Username: "alice"}})
	for _, name := range []string{accountFragmentProfile, accountFragmentPassword} {
		t.Run(name, func(t *testing.T) {
			body := callAccountForm(t, name, "projectId=proj-1", 42, "", "")
			if !strings.Contains(body, "会话未就绪") {
				t.Fatalf("缺 CSRF token 时应给出可见提示；实际：%s", body)
			}
		})
	}
}

// TestAccountFormsSurfaceReadError 读账号失败要报错，不能降级成「你没有资料」。
//
// 后者会让一次服务端故障表现成数据问题，排查方向直接跑偏。
func TestAccountFormsSurfaceReadError(t *testing.T) {
	withAccountPort(t, &fakeVisitorAccount{err: errors.New("db down")})
	w := callAccountFormResp(t, accountFragmentProfile, "projectId=proj-1", 42, "csrf-1", "")
	if w.Code != 500 {
		t.Fatalf("读账号失败应让片段以 500 结束（服务端故障要暴露在状态码与日志里），实际 %d：%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "暂不可用") {
		t.Fatalf("读账号失败不该降级成「暂不可用」（那是端口缺失的语义）；实际：%s", w.Body.String())
	}
}
