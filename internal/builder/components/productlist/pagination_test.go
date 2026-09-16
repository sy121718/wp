package productlist

// pagination_test.go — 集合源分页下推（审计 PERF-019）。
//
// 覆盖两件事：**第 2 页起**按页取数（offset 下推到集合源），**第 1 页保持原路径**
// （构建期只走这一支，换取法会改发布产物字节）。

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"go_wp/internal/builder/core"
)

// fakePager 支持按页取数的集合源，记录最近一次分页参数。
type fakePager struct {
	fakeCollection
	GotOffset int
	GotLimit  int
	Total     int
	Called    bool
}

func (f *fakePager) ResolveCollectionPage(_ context.Context, _ string, q core.CollectionQuery) (core.CollectionPage, error) {
	f.Called = true
	f.GotOffset, f.GotLimit = q.Offset, q.Limit
	items := make([]map[string]any, 0, q.Limit)
	for i := 0; i < q.Limit; i++ {
		items = append(items, map[string]any{
			"id":    "p" + strconv.Itoa(q.Offset+i),
			"title": "第 " + strconv.Itoa(q.Offset+i) + " 条",
		})
	}
	return core.CollectionPage{Items: items, Total: f.Total}, nil
}

// TestBuildViewPushesPaginationToSQL 第 2 页起把 offset 交给集合源。
//
// 这条断言的就是审计 PERF-019 要修的行为：此前翻页只能在「一次取回的集合源上限条」里
// 切内存，超出上限的数据永远翻不到（翻到第 N 页拿到的仍是前 100 条里那一段，再往后是空页，
// 而分页控件还在）。
func TestBuildViewPushesPaginationToSQL(t *testing.T) {
	pager := &fakePager{Total: 50}
	var p Props
	if err := json.Unmarshal(propsOf(t, withFields(map[string]any{"page": 2, "pageSize": 4})), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	view, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{Collection: pager})
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if !pager.Called {
		t.Fatal("第 2 页应当走按页取数（ResolveCollectionPage），实际仍走「整批取回再切内存」")
	}
	if pager.GotOffset != 4 || pager.GotLimit != 4 {
		t.Fatalf("分页参数应下推 offset=4 limit=4，实际 offset=%d limit=%d", pager.GotOffset, pager.GotLimit)
	}
	if len(view.Cards) != 4 {
		t.Fatalf("本页应有 4 张卡，实际 %d", len(view.Cards))
	}
	if !view.HasNext {
		t.Fatal("总量 50 且停在第 2 页，应判定还有下一页")
	}
}

// TestBuildViewFirstPageKeepsBatchPath 第 1 页必须保持原路径。
//
// 构建期只走这一支（发布产物恒定 page=1）。首屏两种取法在**结果**上等价，
// 但字节等价只有原路径能保证 —— 这条用例把它钉住，避免以后有人「顺手统一」。
func TestBuildViewFirstPageKeepsBatchPath(t *testing.T) {
	pager := &fakePager{Total: 50}
	var p Props
	if err := json.Unmarshal(propsOf(t, withFields(map[string]any{"page": 1, "pageSize": 4})), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	if _, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{Collection: pager}); err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if pager.Called {
		t.Fatal("第 1 页不应走按页取数：构建期只走这一支，换取法会改发布产物字节")
	}
}

// TestBuildViewDegradesWithoutPager 集合源没有分页能力时退回整批取回，不报错。
//
// 能力缺失只该让翻页退化成它本来的样子，不该让整块列表渲染不出来。
func TestBuildViewDegradesWithoutPager(t *testing.T) {
	coll := &fakeCollection{items: []map[string]any{{"id": "a", "title": "A"}}}
	var p Props
	if err := json.Unmarshal(propsOf(t, withFields(map[string]any{"page": 2, "pageSize": 4})), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	if _, err := BuildView(nodeOf(t, nil), &p, &core.RenderContext{Collection: coll}); err != nil {
		t.Fatalf("没有分页能力时不应报错，实际: %v", err)
	}
}
