package pubservice

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	json "go_wp/pkg/jsonx"
)

func TestAuditIssueFacts(t *testing.T) {

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

		if !reflect.DeepEqual(issues[i], want[i]) {
			t.Errorf("第 %d 条结论 = %+v，期望 %+v", i, issues[i], want[i])
		}
	}

	if d := issues[len(issues)-1]; d.Path != "" {
		t.Errorf("重复标题结论不该有归属页面，实际 Path = %q", d.Path)
	}

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

func TestAuditIssueNoMessageField(t *testing.T) {
	raw, err := json.Marshal(AuditIssue{Path: "/a", Item: AuditTitleMissing, Level: AuditLevelError})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var got map[string]any
	if uerr := json.Unmarshal(raw, &got); uerr != nil {
		t.Fatalf("反序列化失败: %v", uerr)
	}
	want := []string{"item", "level", "params", "path"}
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, want) {
		t.Fatalf("结论字段 = %v，期望 %v（出现了展示字段说明拼句回来了）", keys, want)
	}
}

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

func TestAuditSiteDuplicateTitlesStableOrder(t *testing.T) {
	docs := []AuditDocument{
		mustParse(t, "/b1", auditPageHTML("beta", "d", "/b1", "")),
		mustParse(t, "/a1", auditPageHTML("alpha", "d", "/a1", "")),
		mustParse(t, "/b2", auditPageHTML("beta", "d", "/b2", "")),
		mustParse(t, "/a2", auditPageHTML("alpha", "d", "/a2", "")),
	}
	active := map[string]bool{"/a1": true, "/a2": true, "/b1": true, "/b2": true}

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
