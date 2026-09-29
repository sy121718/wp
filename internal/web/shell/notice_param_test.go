package shell

// notice_param_test.go — 带参数受控回执（notice_param.go）的边界测试（纯逻辑，不碰数据库）。
//
// 守的是四件**出错时静默**的事：
//
//  1. 白名单外的 key 必须落 fallback（旧形态下这里能塞进任意整句伪造提示）；
//  2. 命中白名单时句子由**词条**决定 —— 用 pkg/i18n.InjectForTest 注入中英两版
//     （英文值带 EN- 前缀，与中文毫无相似度），任何一处没按当前语言取词都会立刻暴露；
//  3. 参数缺失 / 非法 / 超上限一律丢弃整条回执（落 fallback），绝不把缺失的计数当 0
//     渲染成一条看起来像系统结论的「已停用 0 个」；
//  4. 输出里永远不出现字面占位符（{n} / {count}）—— 那既不是文案也不是数据，
//     只能靠人上报才会被发现。
//
// 另钉写侧：非法输入**不写半个键**（半条回执比不显示更误导），合法输入能被读侧读回。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
)

const (
	noticeParamKeyDisabled  = "admin.customers.bulk.result.disabled"
	noticeParamKeyNone      = "admin.customers.bulk.result.none"
	noticeParamKeyStaleName = "admin.customers.bulk.result.stale"
)

// noticeParamInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var noticeParamInjectedEntries = map[string]map[string]string{
	noticeParamKeyDisabled: {
		"zh-CN": "批量操作：已停用 {n} 个，{m} 个未处理。",
		"en-US": "EN-BULK disabled {n}, {m} skipped.",
	},
	noticeParamKeyNone: {
		"zh-CN": "批量操作：没有可处理的账号。",
		"en-US": "EN-BULK nothing to do.",
	},
	// 占位符名（{count}）与调用点声明的参数名（n / m）对不上 —— 用来验证残留判据。
	noticeParamKeyStaleName: {
		"zh-CN": "已清理 {count} 条。",
		"en-US": "EN-BULK cleaned {count}.",
	},
}

// noticeParamSpec 被测槽位（读写两侧共用同一份配置，与生产用法一致）。
var noticeParamSpec = FacingNoticeSpec{
	Slot: "done",
	Keys: map[string]string{
		noticeParamKeyDisabled:  "批量操作：已停用 {n} 个，{m} 个未处理。",
		noticeParamKeyNone:      "批量操作：没有可处理的账号。",
		noticeParamKeyStaleName: "已清理 {count} 条。",
	},
	Params: []FacingNoticeParam{
		{Name: "n", Max: MaxBulkIDs},
		{Name: "m", Max: MaxBulkIDs},
	},
}

const noticeParamFallback = "ERR-FALLBACK"

// noticeParamSetup 注入词条；返回时清空缓存。
func noticeParamSetup(t *testing.T) {
	t.Helper()
	i18n.InjectForTest(noticeParamInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

// noticeParamCtx 组一个带语言与查询串的 GET 上下文。
func noticeParamCtx(t *testing.T, lang, target string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// noticeParamTarget 按槽位协议拼一个列表页 URL（key + 计数）。
func noticeParamTarget(key string, counts map[string]string) string {
	q := url.Values{}
	q.Set("doneKey", key)
	for name, v := range counts {
		q.Set("done"+strings.ToUpper(name[:1])+name[1:], v)
	}
	return "/admin/customers?" + q.Encode()
}

// TestFacingNoticeTextRejectsKeyOutsideWhitelist 白名单外的 key 一律落 fallback。
//
// 三种伪造形态都试：伪造一个同前缀的 key、直接把整句中文塞进 key、以及错误槽上
// 必须落归口文案（且不许把 key 或伪造文案泄露进响应文本）。
func TestFacingNoticeTextRejectsKeyOutsideWhitelist(t *testing.T) {
	noticeParamSetup(t)

	forged := []string{
		"admin.customers.bulk.result.fabricated",
		"admin.customers.bulk.result.disabled;rm -rf /",
		"批量操作：已停用 999 个，0 个未处理。",
	}
	for _, key := range forged {
		target := noticeParamTarget(key, map[string]string{"n": "3", "m": "1"})

		if got := FacingNoticeText(noticeParamCtx(t, "zh-CN", target), noticeParamSpec, ""); got != "" {
			t.Errorf("成功槽：白名单外 key %q 应落空串，实际 %q", key, got)
		}
		got := FacingNoticeText(noticeParamCtx(t, "en-US", target), noticeParamSpec, noticeParamFallback)
		if got != noticeParamFallback {
			t.Errorf("错误槽：白名单外 key %q 应落归口文案，实际 %q", key, got)
		}
		if strings.Contains(got, "fabricated") || strings.Contains(got, "999") {
			t.Errorf("fallback 里不允许出现被拒的 key 或伪造文案，实际 %q", got)
		}
	}
}

// TestFacingNoticeTextTranslatesByCurrentLanguage 命中白名单时句子由词条决定，按当前语言出译文。
func TestFacingNoticeTextTranslatesByCurrentLanguage(t *testing.T) {
	noticeParamSetup(t)

	target := noticeParamTarget(noticeParamKeyDisabled, map[string]string{"n": "3", "m": "1"})
	if got := FacingNoticeText(noticeParamCtx(t, "zh-CN", target), noticeParamSpec, ""); got != "批量操作：已停用 3 个，1 个未处理。" {
		t.Errorf("zh-CN 应给中文译文，实际 %q", got)
	}
	// 英文断言：结果里不能出现任何中文（说明句子的语言真的由读侧取词决定）。
	en := FacingNoticeText(noticeParamCtx(t, "en-US", target), noticeParamSpec, "")
	if en != "EN-BULK disabled 3, 1 skipped." {
		t.Errorf("en-US 应给英文译文，实际 %q", en)
	}
	if strings.ContainsAny(en, "批量操作已停用个未处理") {
		t.Errorf("英文界面下不应出现中文，实际 %q", en)
	}

	// 无占位符的词条同样按语言出：不能因为「少一个计数」就整条消失。
	noneTarget := noticeParamTarget(noticeParamKeyNone, nil)
	if got := FacingNoticeText(noticeParamCtx(t, "zh-CN", noneTarget), noticeParamSpec, ""); got != "批量操作：没有可处理的账号。" {
		t.Errorf("无参数词条 zh-CN 应正常显示，实际 %q", got)
	}
	if got := FacingNoticeText(noticeParamCtx(t, "en-US", noneTarget), noticeParamSpec, ""); got != "EN-BULK nothing to do." {
		t.Errorf("无参数词条 en-US 应正常显示，实际 %q", got)
	}

	// 恰好等于上限的计数放行（上限是闭区间）。
	edge := noticeParamTarget(noticeParamKeyDisabled, map[string]string{"n": "200", "m": "0"})
	if got := FacingNoticeText(noticeParamCtx(t, "zh-CN", edge), noticeParamSpec, ""); got != "批量操作：已停用 200 个，0 个未处理。" {
		t.Errorf("等于上限的计数应放行，实际 %q", got)
	}
}

// TestFacingNoticeTextUsesRegisteredOriginalWhenEntryMissing 库缺词条时用白名单里登记的兜底原文，
// 绝不把裸 key 摆到页面上（裸 key 只说明「没取到词」，不是给人看的句子）。
//
// 两种缺词形态都试：缓存整个空（i18n 未初始化 / key 未 seed），以及只注了别的语言
// （此时走 i18n 的默认语言回退 —— 回退到中文是可接受的，回退到裸 key 不是）。
func TestFacingNoticeTextUsesRegisteredOriginalWhenEntryMissing(t *testing.T) {
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	target := noticeParamTarget(noticeParamKeyDisabled, map[string]string{"n": "3", "m": "1"})

	i18n.InjectForTest(nil, nil)
	got := FacingNoticeText(noticeParamCtx(t, "en-US", target), noticeParamSpec, "")
	if got != "批量操作：已停用 3 个，1 个未处理。" {
		t.Errorf("词条缺失时应给白名单里登记的兜底原文，实际 %q", got)
	}
	if strings.Contains(got, noticeParamKeyDisabled) {
		t.Errorf("输出里不允许出现裸 key，实际 %q", got)
	}

	// 只有中文词条、请求英文：按降级链回退默认语言，仍是完整句子 + 占位符已填。
	i18n.InjectForTest(map[string]map[string]string{
		noticeParamKeyDisabled: {"zh-CN": "批量操作：已停用 {n} 个，{m} 个未处理。"},
	}, nil)
	if got := FacingNoticeText(noticeParamCtx(t, "en-US", target), noticeParamSpec, ""); got != "批量操作：已停用 3 个，1 个未处理。" {
		t.Errorf("缺当前语言词条应回退默认语言，实际 %q", got)
	}
}

// TestFacingNoticeTextDropsNoticeOnBadParams 参数缺失 / 非法 / 超上限 → 丢弃整条回执（落 fallback），
// 绝不把缺失的计数当 0 渲染。
func TestFacingNoticeTextDropsNoticeOnBadParams(t *testing.T) {
	noticeParamSetup(t)

	cases := []struct {
		name   string
		counts map[string]string
	}{
		{"参数缺失（词条需要 {m}）", map[string]string{"n": "3"}},
		{"参数为空串", map[string]string{"n": "3", "m": ""}},
		{"非数字", map[string]string{"n": "3", "m": "x"}},
		{"负数", map[string]string{"n": "3", "m": "-1"}},
		{"带加号", map[string]string{"n": "3", "m": "+1"}},
		{"带前导空白", map[string]string{"n": "3", "m": " 1"}},
		{"小数", map[string]string{"n": "3", "m": "1.0"}},
		{"超上限（Max=200）", map[string]string{"n": "3", "m": "201"}},
		{"超长数字串", map[string]string{"n": "3", "m": strings.Repeat("9", 40)}},
		{"计数由十六进制伪装", map[string]string{"n": "3", "m": "0x10"}},
	}
	for _, tc := range cases {
		target := noticeParamTarget(noticeParamKeyDisabled, tc.counts)
		got := FacingNoticeText(noticeParamCtx(t, "zh-CN", target), noticeParamSpec, noticeParamFallback)
		if got != noticeParamFallback {
			t.Errorf("%s：应丢弃整条回执落 fallback，实际 %q", tc.name, got)
			continue
		}
		// 取舍要点：宁可没有结论，也不能渲染出「已停用 0 个」这种看起来像系统结论的句子。
		if strings.Contains(got, "已停用") || strings.Contains(got, "{") {
			t.Errorf("%s：fallback 不得带半截结论或占位符，实际 %q", tc.name, got)
		}
	}
}

// TestFacingNoticeTextNeverEmitsLiteralPlaceholder 输出里永远不出现字面占位符。
//
// 词条用的占位符名（{count}）与调用点声明的参数名（n / m）对不上时，必须整条丢弃 ——
// 否则页面上会出现「已清理 {count} 条。」这种既不是文案也不是数据的字符串。
func TestFacingNoticeTextNeverEmitsLiteralPlaceholder(t *testing.T) {
	noticeParamSetup(t)

	targets := []string{
		noticeParamTarget(noticeParamKeyStaleName, map[string]string{"n": "3", "m": "1"}),
		noticeParamTarget(noticeParamKeyDisabled, map[string]string{"n": "3", "m": "1"}),
		noticeParamTarget(noticeParamKeyNone, nil),
		noticeParamTarget("admin.customers.bulk.result.fabricated", map[string]string{"n": "1"}),
	}
	for _, target := range targets {
		for _, lang := range []string{"zh-CN", "en-US"} {
			got := FacingNoticeText(noticeParamCtx(t, lang, target), noticeParamSpec, noticeParamFallback)
			if strings.Contains(got, "{") || strings.Contains(got, "}") {
				t.Errorf("%s / %s：输出里出现了字面占位符：%q", lang, target, got)
			}
		}
	}
}

// TestFacingNoticeTextNoKeyMeansNoNotice 没有 key 参数（正常访问列表页）时返回空串，
// 而不是 fallback —— 否则每次打开列表页都会冒出一条「系统内部错误」。
func TestFacingNoticeTextNoKeyMeansNoNotice(t *testing.T) {
	noticeParamSetup(t)

	for _, target := range []string{
		"/admin/customers",
		"/admin/customers?page=2&keyword=x",
		"/admin/customers?doneKey=",
		"/admin/customers?doneKey=%20%20",
	} {
		if got := FacingNoticeText(noticeParamCtx(t, "zh-CN", target), noticeParamSpec, noticeParamFallback); got != "" {
			t.Errorf("无回执（%s）应返回空串，实际 %q", target, got)
		}
	}
}

// TestFacingNoticeSpecInvalidIsRejected spec 写错时读侧落 fallback、写侧报错（不 panic、不静默）。
func TestFacingNoticeSpecInvalidIsRejected(t *testing.T) {
	noticeParamSetup(t)

	bad := []struct {
		name string
		spec FacingNoticeSpec
	}{
		{"槽位名为空", FacingNoticeSpec{Keys: map[string]string{"a.b": "x"}}},
		{"槽位名首字母大写", FacingNoticeSpec{Slot: "Done", Keys: map[string]string{"a.b": "x"}}},
		{"槽位名带横杠", FacingNoticeSpec{Slot: "do-ne", Keys: map[string]string{"a.b": "x"}}},
		{"白名单为空", FacingNoticeSpec{Slot: "done"}},
		{"白名单 key 含空格", FacingNoticeSpec{Slot: "done", Keys: map[string]string{"a b": "x"}}},
		{"参数名首字母大写", FacingNoticeSpec{Slot: "done", Keys: map[string]string{"a.b": "x"},
			Params: []FacingNoticeParam{{Name: "N", Max: 10}}}},
		{"参数重名", FacingNoticeSpec{Slot: "done", Keys: map[string]string{"a.b": "x"},
			Params: []FacingNoticeParam{{Name: "n"}, {Name: "n"}}}},
		{"参数过多", FacingNoticeSpec{Slot: "done", Keys: map[string]string{"a.b": "x"},
			Params: []FacingNoticeParam{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}}}},
	}
	for _, tc := range bad {
		if err := tc.spec.Validate(); err == nil {
			t.Errorf("%s：Validate 应报错", tc.name)
		}
		target := noticeParamTarget("a.b", nil)
		if got := FacingNoticeText(noticeParamCtx(t, "zh-CN", target), tc.spec, noticeParamFallback); got != noticeParamFallback {
			t.Errorf("%s：读侧应落 fallback，实际 %q", tc.name, got)
		}
		q := url.Values{}
		if err := SetFacingNoticeQuery(q, tc.spec, "a.b", nil); err == nil {
			t.Errorf("%s：写侧应报错", tc.name)
		}
		if len(q) != 0 {
			t.Errorf("%s：写侧报错时不得写入 query，实际 %v", tc.name, q)
		}
	}
}

// TestSetFacingNoticeQueryRoundTrip 写侧写入的 query 必须能被读侧读回成同一句话（读写同源）。
func TestSetFacingNoticeQueryRoundTrip(t *testing.T) {
	noticeParamSetup(t)

	q := url.Values{}
	if err := SetFacingNoticeQuery(q, noticeParamSpec, noticeParamKeyDisabled, map[string]int{"n": 3, "m": 1}); err != nil {
		t.Fatalf("合法写入不应报错：%v", err)
	}
	// 参数名由「槽位 + 驼峰」派生，与规范示例一致。
	if q.Get("doneKey") != noticeParamKeyDisabled || q.Get("doneN") != "3" || q.Get("doneM") != "1" {
		t.Fatalf("query 形状不符：%v", q.Encode())
	}
	target := "/admin/customers?" + q.Encode()
	if got := FacingNoticeText(noticeParamCtx(t, "en-US", target), noticeParamSpec, ""); got != "EN-BULK disabled 3, 1 skipped." {
		t.Errorf("写侧产物应能被读侧读回成同一句话，实际 %q", got)
	}

	// counts 里没给的参数不写进 query（「没有跳过项」的那一支）。
	q2 := url.Values{}
	if err := SetFacingNoticeQuery(q2, noticeParamSpec, noticeParamKeyDisabled, map[string]int{"n": 2}); err != nil {
		t.Fatalf("缺参数的写入不应报错：%v", err)
	}
	if q2.Has("doneM") || q2.Get("doneN") != "2" {
		t.Errorf("未提供的计数不应写进 query，实际 %v", q2.Encode())
	}
	// 该情形读侧按词条判：词条需要 {m} 而没有 → 丢弃整条（而不是显示「0 个未处理」）。
	if got := FacingNoticeText(noticeParamCtx(t, "zh-CN", "/admin/customers?"+q2.Encode()), noticeParamSpec, ""); got != "" {
		t.Errorf("缺计数应丢弃回执，实际 %q", got)
	}
}

// TestSetFacingNoticeQueryRejectsBadInput 写侧的三种编程错误：key 不在白名单、参数名拼错、
// 计数超范围 —— 全都报错且**一个键都不写**（半条回执会让页面显示一条残缺结论）。
func TestSetFacingNoticeQueryRejectsBadInput(t *testing.T) {
	noticeParamSetup(t)

	cases := []struct {
		name   string
		key    string
		counts map[string]int
	}{
		{"key 不在白名单", "admin.customers.bulk.result.fabricated", map[string]int{"n": 1}},
		{"key 是整句中文", "批量操作：已停用 3 个。", map[string]int{"n": 1}},
		{"参数名拼错", noticeParamKeyDisabled, map[string]int{"count": 1}},
		{"计数超上限", noticeParamKeyDisabled, map[string]int{"n": MaxBulkIDs + 1}},
		{"计数为负", noticeParamKeyDisabled, map[string]int{"n": -1}},
	}
	for _, tc := range cases {
		q := url.Values{"page": {"2"}}
		if err := SetFacingNoticeQuery(q, noticeParamSpec, tc.key, tc.counts); err == nil {
			t.Errorf("%s：应报错", tc.name)
		}
		if got := q.Encode(); got != "page=2" {
			t.Errorf("%s：报错时不得写入任何回执键，实际 %q", tc.name, got)
		}
	}

	if err := SetFacingNoticeQuery(nil, noticeParamSpec, noticeParamKeyNone, nil); err == nil {
		t.Error("nil url.Values 应报错而不是 panic")
	}
}
