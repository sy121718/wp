package usermcp

// customer_tools_test.go — 客户工具两个的元信息、入参映射与正文可读性。
//
// 正文断言不是「测文案」：模型的回答完全来自 Text（Data 只给渲染层），
// 正文里少了哪个数，用户就看不到哪个数。这里钉的是几件会直接决定回答对错的事：
//   · id 必须逐条出现在结果里 —— 它是接着调 customer_get 的唯一钥匙；
//   · 「状态不过滤」必须映射成 dto 的 -1，**不能**是 0（0 是「已停用」，
//     零值直接透传会让不传状态的提问只返回停用账号，而结果看起来完全正常）；
//   · 注册结束日必须落在当日最后一秒（闭区间），传零点会整批漏掉「结束那天注册的人」；
//   · 状态文案走 userenums 的真源，不自己写一份中文表。
//
// 注意测试传参用 map 而不是顾客结构体：结构体序列化会把零值写成 ""，
// 而 mcp 的 Enum 白名单**拒绝空串**（validate.go 的 validateValue）——
// 模型不传某个可选参数时 JSON 里根本没有那个键，两者不是一回事。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
)

// stubQuery 假 reader：记录入参、回放固定结果。
type stubQuery struct {
	gotList *userdto.CustomerListReq
	gotGet  uint64

	listRes *userdto.CustomerListResp
	getRes  *userdto.CustomerResp
	err     error
}

func (s *stubQuery) ListCustomers(_ context.Context, req *userdto.CustomerListReq) (*userdto.CustomerListResp, error) {
	s.gotList = req
	return s.listRes, s.err
}

func (s *stubQuery) GetCustomer(_ context.Context, id uint64) (*userdto.CustomerResp, error) {
	s.gotGet = id
	return s.getRes, s.err
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化入参失败: %v", err)
	}
	return b
}

func mustQueryTools(t *testing.T, stub *stubQuery) map[string]mcp.Tool {
	t.Helper()
	tools, err := QueryTools(stub)
	if err != nil {
		t.Fatalf("取客户工具集失败: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("客户工具应暴露 2 个，实得 %d", len(tools))
	}
	byName := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name()] = tool
	}
	return byName
}

func TestQueryToolsRejectsNilReader(t *testing.T) {
	if _, err := QueryTools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错（装配期接线缺陷要在启动时炸掉）")
	}
}

func TestCustomerFindDeclaresFuzzyKeyword(t *testing.T) {
	tools := mustQueryTools(t, &stubQuery{})
	find, ok := tools["customer_find"]
	if !ok {
		t.Fatal("缺少 customer_find")
	}
	// 描述必须把「关键词能匹配哪几列」说出来：模型靠描述决定用户给的昵称能不能直接用。
	desc := find.Description()
	for _, want := range []string{"邮箱", "用户名", "昵称", "展示名"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("customer_find 描述里应写明 keyword 匹配 %s：%q", want, desc)
		}
	}
}

func TestCustomerFindDefaultFiltersAreNotDisabled(t *testing.T) {
	stub := &stubQuery{listRes: &userdto.CustomerListResp{}}
	tools := mustQueryTools(t, stub)
	// 空参数（模型什么都没传）：状态与邮箱验证都必须落到「不过滤」，而不是零值 0。
	if _, err := tools["customer_find"].Invoke(context.Background(), mustRaw(t, map[string]any{})); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotList.Status != userdto.CustomerStatusAll {
		t.Fatalf("不传 status 应映射为 %d（不过滤），实得 %d —— 0 是「已停用」",
			userdto.CustomerStatusAll, stub.gotList.Status)
	}
	if stub.gotList.EmailVerified != userdto.EmailVerifiedAll {
		t.Fatalf("不传 emailVerified 应映射为 %d（不过滤），实得 %d",
			userdto.EmailVerifiedAll, stub.gotList.EmailVerified)
	}
	if stub.gotList.RegisteredFrom != nil || stub.gotList.RegisteredTo != nil {
		t.Fatal("不传日期时两端都应为 nil（该端不限）")
	}
}

func TestCustomerFindMapsStatusAndVerified(t *testing.T) {
	cases := []struct {
		name     string
		args     map[string]any
		wantSt   int
		wantMail int
	}{
		{"显式 disabled", map[string]any{"status": "disabled"}, userenums.StatusDisabled, userdto.EmailVerifiedAll},
		{"显式 active", map[string]any{"status": "active"}, userenums.StatusActive, userdto.EmailVerifiedAll},
		{"显式 pending", map[string]any{"status": "pending"}, userenums.StatusPending, userdto.EmailVerifiedAll},
		{"显式 all", map[string]any{"status": "all"}, userdto.CustomerStatusAll, userdto.EmailVerifiedAll},
		{"未验证邮箱", map[string]any{"emailVerified": "no"}, userdto.CustomerStatusAll, userdto.EmailVerifiedNo},
		{"已验证邮箱", map[string]any{"emailVerified": "yes"}, userdto.CustomerStatusAll, userdto.EmailVerifiedYes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQuery{listRes: &userdto.CustomerListResp{}}
			tools := mustQueryTools(t, stub)
			if _, err := tools["customer_find"].Invoke(context.Background(), mustRaw(t, tc.args)); err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if stub.gotList.Status != tc.wantSt {
				t.Fatalf("status 映射错误：want %d，got %d", tc.wantSt, stub.gotList.Status)
			}
			if stub.gotList.EmailVerified != tc.wantMail {
				t.Fatalf("emailVerified 映射错误：want %d，got %d", tc.wantMail, stub.gotList.EmailVerified)
			}
		})
	}
}

// 白名单之外的取值必须在声明期被拦下（安全边界，不是便利设施）。
func TestCustomerFindRejectsUnknownStatus(t *testing.T) {
	tools := mustQueryTools(t, &stubQuery{listRes: &userdto.CustomerListResp{}})
	_, err := tools["customer_find"].Invoke(context.Background(), mustRaw(t, map[string]any{"status": "ACTIVE"}))
	if err == nil {
		t.Fatal("白名单之外的取值应被拦下，而不是宽容映射（宽容写法属于 handler，不该穿过 schema）")
	}
	var argsErr *mcp.ArgsError
	if !errors.As(err, &argsErr) {
		t.Fatalf("应是 *mcp.ArgsError，实得 %T（%v）", err, err)
	}
}

func TestCustomerFindDayWindowCoversWholeDay(t *testing.T) {
	stub := &stubQuery{listRes: &userdto.CustomerListResp{}}
	tools := mustQueryTools(t, stub)
	_, err := tools["customer_find"].Invoke(context.Background(), mustRaw(t, map[string]any{
		"registeredFrom": "2026-09-01",
		"registeredTo":   "2026-09-30",
	}))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotList.RegisteredFrom == nil || stub.gotList.RegisteredTo == nil {
		t.Fatal("给了日期就应两端都非 nil")
	}
	from := stub.gotList.RegisteredFrom.Time()
	if from.Hour() != 0 || from.Minute() != 0 || from.Second() != 0 {
		t.Fatalf("起始日应是当日零点，实得 %s", from.Format("15:04:05"))
	}
	to := stub.gotList.RegisteredTo.Time()
	if to.Format("2006-01-02") != "2026-09-30" || to.Hour() != 23 || to.Minute() != 59 {
		t.Fatalf("结束日应落在当日 23:59（闭区间含当天），实得 %s", to.Format("2006-01-02 15:04:05"))
	}
}

func TestCustomerFindRejectsBadDate(t *testing.T) {
	stub := &stubQuery{listRes: &userdto.CustomerListResp{}}
	tools := mustQueryTools(t, stub)
	_, err := tools["customer_find"].Invoke(context.Background(), mustRaw(t, map[string]any{
		"registeredFrom": "2026/09/01",
	}))
	if err == nil {
		t.Fatal("非法日期应当报错，而不是当成没传（那会让用户以为筛过了）")
	}
	if !strings.Contains(err.Error(), "registeredFrom") {
		t.Fatalf("报错要指明是哪个参数：%v", err)
	}
}

func TestCustomerFindClampsLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, customerFindDefaultLimit},
		{-3, customerFindDefaultLimit},
		{5, 5},
		{999, customerFindMaxLimit},
	}
	for _, tc := range cases {
		stub := &stubQuery{listRes: &userdto.CustomerListResp{}}
		tools := mustQueryTools(t, stub)
		if _, err := tools["customer_find"].Invoke(context.Background(), mustRaw(t, map[string]any{"limit": tc.in})); err != nil {
			t.Fatalf("调用失败: %v", err)
		}
		if stub.gotList.Limit != tc.want {
			t.Fatalf("limit %d 应夹取成 %d，实得 %d", tc.in, tc.want, stub.gotList.Limit)
		}
	}
}

func TestFindCustomersTextCarriesIDAndNeverLoggedIn(t *testing.T) {
	res := &userdto.CustomerListResp{
		Total: 2,
		List: []*userdto.CustomerResp{
			{
				ID: 7, Username: "zhangsan", Email: "z@example.com", DisplayName: "张三",
				Status: userenums.StatusActive, StatusLabel: "正常", EmailVerified: true,
				RegisteredAtText: "2026-09-01 10:22", LastLoginTimeText: "2026-10-05 09:11",
				LastLoginLocation: "上海",
			},
			{
				ID: 8, Username: "lisi", Email: "l@example.com",
				Status: userenums.StatusPending, EmailVerified: false,
				RegisteredAtText: "2026-10-04 08:00", Locked: true,
			},
		},
		Counters: userdto.CustomerCounters{Total: 120, Active: 100, Pending: 5, Disabled: 15, Locked: 2, Verified: 90, Unverified: 30},
	}
	text := findCustomersText(res)
	for _, want := range []string{
		"id=7", "id=8", // 接着调 customer_get 的钥匙
		"张三", "zhangsan", "z@example.com",
		"（上海）",             // 最后登录带归属地
		"从未登录",             // 待激活账号的常态，不能显示成空
		"（已锁定）",            // 锁定必须一眼可见
		"全部 120", "未验证 30", // 计数条回答的是「一共多少账号」
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, text)
		}
	}
}

func TestFindCustomersTextEmptyStillReportsCounters(t *testing.T) {
	text := findCustomersText(&userdto.CustomerListResp{
		Counters: userdto.CustomerCounters{Total: 3, Active: 3, Verified: 3},
	})
	if !strings.Contains(text, "没有符合条件") {
		t.Fatalf("空结果应直说：%s", text)
	}
	if !strings.Contains(text, "全部 3") {
		t.Fatalf("空结果仍应给账号分布（否则模型答不出「一共多少账号」）：%s", text)
	}
}

func TestCustomerGetRequiresID(t *testing.T) {
	stub := &stubQuery{}
	tools := mustQueryTools(t, stub)
	_, err := tools["customer_get"].Invoke(context.Background(), mustRaw(t, map[string]any{}))
	if err == nil {
		t.Fatal("customerId 为 0 应当在调用期被拦下")
	}
	var argsErr *mcp.ArgsError
	if !errors.As(err, &argsErr) {
		t.Fatalf("应是 *mcp.ArgsError，实得 %T（%v）", err, err)
	}
}

func TestCustomerGetPassesIDAndDescribes(t *testing.T) {
	stub := &stubQuery{getRes: &userdto.CustomerResp{
		ID: 42, Username: "wangwu", Email: "w@example.com", Nickname: "老王",
		Status: userenums.StatusDisabled, EmailVerified: false,
		RegisteredAtText: "2026-08-01 12:00", RegisterIP: "1.2.3.4", RegisterLocation: "北京",
		LastLoginTimeText: "2026-09-20 20:00", LoginFailureCount: 3,
	}}
	tools := mustQueryTools(t, stub)
	out, err := tools["customer_get"].Invoke(context.Background(), mustRaw(t, map[string]any{"customerId": 42}))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotGet != 42 {
		t.Fatalf("id 应原样透传，实得 %d", stub.gotGet)
	}
	for _, want := range []string{
		"老王", "id=42", "已停用", "未验证", "登录失败次数：3",
		"1.2.3.4", "北京", // 注册 IP 与归属地
	} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, out.Text)
		}
	}
}

func TestCustomerStatusOfFallsBackToEnumLabel(t *testing.T) {
	// StatusLabel 由出口填；工具是出口，所以自己填。留空时必须落到 userenums 的真源。
	c := &userdto.CustomerResp{Status: userenums.StatusDisabled}
	if got := customerStatusOf(c); got == "" || got == "0" {
		t.Fatalf("状态文案应落到 userenums 的中文兜底，实得 %q", got)
	}
	c2 := &userdto.CustomerResp{Status: userenums.StatusActive, StatusLabel: "Active"}
	if got := customerStatusOf(c2); got != "Active" {
		t.Fatalf("已有 StatusLabel 时应原样使用，实得 %q", got)
	}
}
