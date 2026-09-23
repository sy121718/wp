// product_locator_chunk_test.go — 已上线路径的批量解析必须**分片**而不是截断。
//
// 守的是一个只在跨过阈值时才出现的缺陷：路径解析按 50 个实体一批（IN 列表不能
// 无限长），但曾经是截断 —— 第 51 个之后查不到路径，商品列表按「未发布」处理，
// 于是那些卡片只显示标题、点不动。
//
// 单品牌列表通常 ≤ 50 条，一切正常；只有多品牌并集 / 大分类才会跨过阈值，
// 表现为「勾上第二个筛选后，多出来的那些商品点不开」。
package feature

import (
	"context"
	"fmt"
	"testing"
)

// TestPublishedEntityPathsNotTruncatedAtBatchSize 60 个实体全部解析出路径（> 单批上限 50）。
//
// 走**真实发布管线**建实例（每次约 15ms，60 次不到一秒）：
// 手工插行的路子要同时满足两条复合外键（active_artifact 与 snapshot 都指向
// 「属于该实例」的行），绕过它们等于把夹具写成另一套实现，测出来的东西就不是产品了。
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
	// 尾部实体必须在结果里：截断只可能丢尾巴，前缀一直是对的 —— 这正是它难被发现的原因。
	for i := 50; i < total; i++ {
		if paths[ids[i]] == "" {
			t.Fatalf("第 %d 个实体没有解析出路径（截断回归）: %s", i, ids[i])
		}
	}
}
