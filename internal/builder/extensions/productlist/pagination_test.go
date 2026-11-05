package productlist

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strconv"
	"testing"

	"go_wp/pkg/builder/core"
)

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
