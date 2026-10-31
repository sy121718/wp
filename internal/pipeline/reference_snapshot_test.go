package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	json "go_wp/pkg/jsonx"
)

type referenceRows struct {
	rows  []blockcontract.BlockResp
	calls int
}

func (r *referenceRows) List(context.Context, *blockcontract.ListReq) ([]blockcontract.BlockResp, error) {
	r.calls++
	out := make([]blockcontract.BlockResp, len(r.rows))
	copy(out, r.rows)
	for i := range out {
		out[i].Document = append(json.RawMessage(nil), out[i].Document...)
	}
	return out, nil
}

type referenceMenu struct{}

func (referenceMenu) ResolveMenu(string, string) ([]core.NavigationItem, error) {
	return []core.NavigationItem{{Label: "目录", URL: "/", Children: []core.NavigationItem{{Label: "面板", URL: "/", PanelBlockID: "panel"}}}}, nil
}
func (r referenceMenu) ResolveNavigation(p, id string) ([]core.NavigationItem, error) {
	return r.ResolveMenu(p, id)
}

func TestReferenceSnapshotFreezesRecursiveRepeatedAndMultilingualReads(t *testing.T) {
	ctx := context.Background()
	set, _, err := ComponentSetWithPlugins(nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := templateDoc("copy", "OLD-FROZEN-CONTENT")
	root := []byte(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.globalref","props":{"blockId":"outer"}},{"id":"b","type":"core.globalref","props":{"blockId":"outer"}}]}`)
	rows := &referenceRows{rows: []blockcontract.BlockResp{
		{ID: "outer", ProjectID: "p", ReuseMode: "global", Version: 4, Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"nested","type":"core.globalref","props":{"blockId":"inner"}}]}`)},
		{ID: "inner", ProjectID: "p", ReuseMode: "global", Version: 7, Document: inner},
		{ID: "unrelated", Version: 1, Document: json.RawMessage(`not-json`)},
	}}
	snapshot, err := FreezeReferences(ctx, "p", root, rows, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rows.calls != 1 || len(snapshot.Blocks) != 2 || snapshot.Blocks["inner"].Version != 7 {
		t.Fatalf("必须单次读取且只保存真实递归闭包: calls=%d snapshot=%+v", rows.calls, snapshot)
	}
	ctx = WithReferenceSnapshot(ctx, snapshot)
	render := func(lang string) string {
		t.Helper()
		page, e := builder.ParsePage(root)
		if e != nil {
			t.Fatal(e)
		}
		frozen := ReferenceSnapshotFromContext(ctx, "p")
		compiled, e := builder.Compile(page, builder.WithComponentSet(set), builder.WithBlockResolver(frozen), builder.WithCompileMode(builder.CompileModePublish), builder.WithLanguage(lang))
		if e != nil {
			t.Fatal(e)
		}
		html, e := builder.RenderDocument(compiled)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Count(html, "OLD-FROZEN-CONTENT") != 2 || strings.Contains(html, "NEW-LIVE-CONTENT") {
			t.Fatalf("重复引用/语言必须使用同一快照: %s", html)
		}
		return html
	}
	first := render("zh-CN")
	rows.rows[1].Version = 8
	rows.rows[1].Document = templateDoc("copy", "NEW-LIVE-CONTENT")
	render("en-US")
	if got := render("zh-CN"); got != first {
		t.Fatal("相同输入必须字节稳定")
	}
	if _, err = snapshot.ResolveBlockRoot("unrelated"); err == nil {
		t.Fatal("快照缺项不能读取候选或最新来源")
	}
	if rows.calls != 1 {
		t.Fatalf("编译不能再次查块源: %d", rows.calls)
	}

	plan := PublicationPlan{SiteLangs: []string{"zh-CN"}, DefaultLang: "zh-CN", References: snapshot}
	copyPlan := plan.Normalize()
	copyPlan.References.Blocks["inner"] = FrozenBlock{Version: 99}
	if plan.References.Blocks["inner"].Version != 7 {
		t.Fatal("计划规范化必须深复制")
	}
	if ReferenceSnapshotFromContext(ctx, "other") != nil {
		t.Fatal("快照不能跨工程复用")
	}
}

func TestReferenceSnapshotIncludesStructureAndBoundMenuPanel(t *testing.T) {
	doc := []byte(`{"settings":{"structure":{"headerTemplateId":"header"}},"root":[{"id":"menu","type":"core.nav","props":{"menu":"header"}}]}`)
	rows := &referenceRows{rows: []blockcontract.BlockResp{
		{ID: "panel", Version: 2, Document: templateDoc("panel-copy", "PANEL")},
		{ID: "header-block", Version: 3, Document: templateDoc("header-copy", "HEADER")},
	}}
	header := []byte(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"header-ref","type":"core.globalref","props":{"blockId":"header-block"}}]}`)
	snapshot, err := FreezeReferences(context.Background(), "p", doc, rows, stubPort{docs: map[string][]byte{"header": header}}, referenceMenu{})
	if err != nil {
		t.Fatal(err)
	}
	if rows.calls != 1 || len(snapshot.Blocks) != 2 || len(snapshot.Templates) != 1 {
		t.Fatalf("结构模板与动态菜单面板必须一起固定: %+v calls=%d", snapshot, rows.calls)
	}
	frozen, err := snapshot.ResolveStructureDocument(context.Background(), "p", "header")
	if err != nil || string(frozen) != string(header) {
		t.Fatalf("结构模板原文不等: %s %v", frozen, err)
	}
	if _, err = snapshot.ResolveStructureDocument(context.Background(), "other", "header"); err == nil {
		t.Fatal("结构模板工程不匹配必须失败")
	}
}

func TestReferenceSnapshotLeavesCyclesToExistingCompiler(t *testing.T) {
	root := []byte(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a","type":"core.globalref","props":{"blockId":"loop"}}]}`)
	rows := &referenceRows{rows: []blockcontract.BlockResp{{ID: "loop", Version: 1, Document: root}}}
	frozen, err := FreezeReferences(context.Background(), "p", root, rows, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := builder.ParsePage(root)
	set, _, err := ComponentSetWithPlugins(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = builder.Compile(page, builder.WithComponentSet(set), builder.WithBlockResolver(frozen), builder.WithCompileMode(builder.CompileModePublish))
	if err == nil || !strings.Contains(fmt.Sprint(err), "循环") {
		t.Fatalf("既有循环检测必须保留: %v", err)
	}
}
