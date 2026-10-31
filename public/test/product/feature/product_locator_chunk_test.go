package feature

import (
	"context"
	"fmt"
	"testing"
)

func TestPublishedEntityPathsNotTruncatedAtBatchSize(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	const total = 60
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		slug := fmt.Sprintf("chunk-entity-%02d", i)
		id := f.createProduct(t, fmt.Sprintf("分片实体 %02d", i), slug, "描述", 10, 20)
		f.publish(t, id, "/"+slug)
		ids = append(ids, id)
	}

	paths, err := f.pres.PublishedEntityPaths(ctx, f.projectID, "product", "en-AU", ids)
	if err != nil {
		t.Fatalf("批量解析路径失败: %v", err)
	}
	if len(paths) != total {
		t.Fatalf("应解析出全部 %d 条路径，实际 %d（第 %d 条之后被丢掉了？）", total, len(paths), 50)
	}

	for i := 50; i < total; i++ {
		if paths[ids[i]] == "" {
			t.Fatalf("第 %d 个实体没有解析出路径（截断回归）: %s", i, ids[i])
		}
	}
}
