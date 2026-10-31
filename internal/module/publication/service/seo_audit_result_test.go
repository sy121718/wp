package pubservice

// seo_audit_result_test.go — 体检结论的**事实形状**（不是文案）。
//
// 这批测试要钉住的是三件事，与既有 seo_audit_test.go（算法本身）分工不同：
//
//  1. 七类检查项的输出只有 path/item/level/params，**没有 Message 成品句**
//     —— service 里出现中文拼句就等于把展示层搬进了业务层，换语言要重写一遍；
//  2. 重复标题的路径一个不少、Path 为空（它不是某一页的问题），
//     并且与 map 遍历顺序无关（同一份输入必须产出同一份结果）；
//  3. 空结果归一成 Issues = []（不是 nil）：JSON 里 null 与 [] 对前端是两种形状，
//     空态分支会因此多一条 if。
//
// 断言用等式而不是「各字段分别正确」：一条结论错了一个字段，往往是因为它
// 整体被拼错了对象，逐字段断言反而掩盖了形状漂移。

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	json "go_wp/pkg/jsonx"
)

// TestAuditIssueFacts 七类检查项各产一条事实结论，参数只带原值。
func TestAuditIssueFacts(t *testing.T) {
	// 一页把该报的问题全报出来：缺 title / 缺 description / 缺 canonical /
	// 无 alt 图片 / 死链；另一页与它同标题（触发重复）；再一页只声明一条 hreflang。
	broken := mustParse(t, "/broken",
		"<html><head>"+
			`<link rel="alternate" hreflang="en-US" href="/broken">`+
			"</head><body>"+
			`<img src="/media/hero.png">`+
			`<a href="/missing">gone</a>`+
			"</body></html>")
	dupA := mustParse(t, "/dup-a", auditPageHTML("重复标题", "d", "/dup-a", ""))
	dupB := mustParse(t, "/dup-b", auditPageHTML("重复标题", "d", "/dup-b", ""))
	active := map[string]bool{"/broken": true, "/dup-a": true, "/dup-b": true}

	issues := AuditSite([]AuditDocument{broken, dupA, dupB}, active)

	want := []AuditIssue{
		{Path: "/broken", Item: AuditTitleMissing, Level: AuditLevelError},
		{Path: "/broken", Item: AuditDescriptionMissing, Level: AuditLevelWarning},
		{Path: "/broken", Item: AuditCanonicalMissing, Level: AuditLevelWarning},
		{Path: "/broken", Item: AuditImageAltMissing, Level: AuditLevelWarning,
			Params: AuditIssueParams{Src: "/media/hero.png"}},
		{Path: "/broken", Item: AuditInternalLinkBroken, Level: AuditLevelError,
			Params: AuditIssueParams{Href: "/missing"}},
		{Path: "/broken", Item: AuditHreflangIncomplete, Level: AuditLevelWarning},
		{Item: AuditTitleDuplicate, Level: AuditLevelWarning,
			Params: AuditIssueParams{Title: "重复标题", Paths: []string{"/dup-a", "/dup-b"}}},
	}
	if len(issues) != len(want) {
		t.Fatalf("结论条数 = %d，期望 %d：%+v", len(issues), len(want), issues)
	}
	for i := range want {
		// Params 含切片，不能直接比较结构体；用 reflect.DeepEqual 一次定型，
		// 避免逐字段比较时漏掉某个新增参数（漏掉的那个正好会静默漂移）。
		if !reflect.DeepEqual(issues[i], want[i]) {
			t.Errorf("第 %d 条结论 = %+v，期望 %+v", i, issues[i], want[i])
		}
	}

	// 重复标题必须是**一条**结论，且 Path 为空：把它算到首个命中页头上，
	// 运营只会去改那一页，其余页面的标题仍是重复的。
	if d := issues[len(issues)-1]; d.Path != "" {
		t.Errorf("重复标题结论不该有归属页面，实际 Path = %q", d.Path)
	}
	// 六类页面级结论里只有图片/死链带原值，其余四类三个参数全空
	// （omitempty 之后不应出现在 JSON 里 —— 见 TestAuditIssueJSONShape）。
	for _, it := range issues[:6] {
		switch it.Item {
		case AuditImageAltMissing:
			if it.Params.Src == "" || it.Params.Href != "" || it.Params.Title != "" || len(it.Params.Paths) != 0 {
				t.Errorf("图片结论参数错误：%+v", it.Params)
			}
		case AuditInternalLinkBroken:
			if it.Params.Href == "" || it.Params.Src != "" || it.Params.Title != "" || len(it.Params.Paths) != 0 {
				t.Errorf("死链结论参数错误：%+v", it.Params)
			}
		default:
			if !reflect.DeepEqual(it.Params, AuditIssueParams{}) {
				t.Errorf("%s 不该带参数：%+v", it.Item, it.Params)
			}
		}
	}
}

// TestAuditIssueNoMessageField 结论类型不得再带回成品句字段。
//
// 这是形状断言而不是语义断言：Message 一旦回来，模板与接口就会各抄一份文案，
// 而这里的中文句子永远跟不上词条。用 JSON 键集合判定 —— 加字段就等于改契约。
func TestAuditIssueNoMessageField(t *testing.T) {
	raw, err := json.Marshal(AuditIssue{Path: "/a", Item: AuditTitleMissing, Level: AuditLevelError})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var got map[string]any
	if uerr := json.Unmarshal(raw, &got); uerr != nil {
		t.Fatalf("反序列化失败: %v", uerr)
	}
	want := []string{"item", "level", "params", "path"} // 已排序：与下面 Sort 后的 keys 同序
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, want) {
		t.Fatalf("结论字段 = %v，期望 %v（出现了展示字段说明拼句回来了）", keys, want)
	}
}

// TestAuditIssueJSONShape 参数按检查项出现：空值不出现在 JSON 里，重复标题带全部路径。
func TestAuditIssueJSONShape(t *testing.T) {
	dup, err := json.Marshal(AuditIssue{Item: AuditTitleDuplicate, Level: AuditLevelWarning,
		Params: AuditIssueParams{Title: "t", Paths: []string{"/a", "/b"}}})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var got struct {
		Params map[string]any `json:"params"`
	}
	if uerr := json.Unmarshal(dup, &got); uerr != nil {
		t.Fatalf("反序列化失败: %v", uerr)
	}
	if _, ok := got.Params["src"]; ok {
		t.Errorf("重复标题不该带 src：%v", got.Params)
	}
	if _, ok := got.Params["href"]; ok {
		t.Errorf("重复标题不该带 href：%v", got.Params)
	}
	paths, ok := got.Params["paths"].([]any)
	if !ok || len(paths) != 2 || paths[0] != "/a" || paths[1] != "/b" {
		t.Errorf("重复标题必须带全部命中路径，实际 %v", got.Params["paths"])
	}

	plain, err := json.Marshal(AuditIssue{Path: "/a", Item: AuditTitleMissing, Level: AuditLevelError})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var plainGot struct {
		Params map[string]any `json:"params"`
	}
	if uerr := json.Unmarshal(plain, &plainGot); uerr != nil {
		t.Fatalf("反序列化失败: %v", uerr)
	}
	if len(plainGot.Params) != 0 {
		t.Errorf("无参数结论的 params 应为空对象，实际 %v", plainGot.Params)
	}
}

// TestAuditResultEmpty 空结果：Issues = []（编码为 []，不是 null），Count = len(Issues)。
func TestAuditResultEmpty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scanned int
		in      []AuditIssue
	}{
		{name: "无产物", scanned: 0, in: nil},
		{name: "查过没问题", scanned: 5, in: []AuditIssue{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := newAuditResult(tc.scanned, tc.in)
			if got.Scanned != tc.scanned {
				t.Errorf("scanned = %d，期望 %d", got.Scanned, tc.scanned)
			}
			if got.Issues == nil {
				t.Fatalf("Issues 必须是空切片而不是 nil：JSON 里 null 与 [] 是两种形状")
			}
			if got.Count != len(got.Issues) {
				t.Errorf("count = %d，与 len(issues) = %d 不等", got.Count, len(got.Issues))
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			var back struct {
				Scanned int             `json:"scanned"`
				Count   int             `json:"count"`
				Issues  json.RawMessage `json:"issues"`
			}
			if uerr := json.Unmarshal(raw, &back); uerr != nil {
				t.Fatalf("反序列化失败: %v", uerr)
			}
			if string(back.Issues) != "[]" {
				t.Errorf("空结论编码 = %s，期望 []", back.Issues)
			}
			if back.Count != 0 || back.Scanned != tc.scanned {
				t.Errorf("空结论计数 = %+v", back)
			}
		})
	}
}

// TestAuditResultCountMatchesIssues 非空结果：Count 恒等于 len(Issues)。
func TestAuditResultCountMatchesIssues(t *testing.T) {
	issues := []AuditIssue{
		{Path: "/a", Item: AuditTitleMissing, Level: AuditLevelError},
		{Path: "/b", Item: AuditCanonicalMissing, Level: AuditLevelWarning},
	}
	got := newAuditResult(2, issues)
	if got.Count != len(issues) {
		t.Fatalf("count = %d，期望 %d", got.Count, len(issues))
	}
	if len(got.Issues) != len(issues) {
		t.Fatalf("issues 被改动：%+v", got.Issues)
	}
}

// TestAuditSiteDuplicateTitlesStableOrder 多个重复标题组的**输出顺序**是确定的。
//
// 标题分组原先是 map：range 的顺序每次运行都可能不同，于是同一份产物两次体检
// 给出顺序不同的结论列表（构建确定性，AGENTS.md 不变量 5）。现在按标题排序输出，
// 这里用两组重复标题钉住顺序本身 —— 只断言「都出现了」在顺序漂移时仍然全绿。
func TestAuditSiteDuplicateTitlesStableOrder(t *testing.T) {
	docs := []AuditDocument{
		mustParse(t, "/b1", auditPageHTML("beta", "d", "/b1", "")),
		mustParse(t, "/a1", auditPageHTML("alpha", "d", "/a1", "")),
		mustParse(t, "/b2", auditPageHTML("beta", "d", "/b2", "")),
		mustParse(t, "/a2", auditPageHTML("alpha", "d", "/a2", "")),
	}
	active := map[string]bool{"/a1": true, "/a2": true, "/b1": true, "/b2": true}

	// 分组键（标题）升序：alpha 组在前、beta 组在后；组内路径同样升序。
	var got []string
	for _, it := range AuditSite(docs, active) {
		if it.Item == AuditTitleDuplicate {
			got = append(got, it.Params.Title+"="+strings.Join(it.Params.Paths, ","))
		}
	}
	want := []string{"alpha=/a1,/a2", "beta=/b1,/b2"}
	if !slices.Equal(got, want) {
		t.Fatalf("重复标题输出顺序 = %v，期望 %v", got, want)
	}

	// 同一批输入再跑一次必须逐字相同（map 顺序随机时这里会抖）。
	again := make([]string, 0, len(got))
	for _, it := range AuditSite(docs, active) {
		if it.Item == AuditTitleDuplicate {
			again = append(again, it.Params.Title+"="+strings.Join(it.Params.Paths, ","))
		}
	}
	if !slices.Equal(again, got) {
		t.Fatalf("两次体检顺序不一致：%v vs %v", got, again)
	}
}
