package jet

import (
	"testing"

	"go_wp/pkg/facing"
)

type blockedFailure struct{ items []facing.BlockedItem }

func (b blockedFailure) Error() string { return "blocked" }

func (b blockedFailure) BlockedItems() []facing.BlockedItem { return b.items }

// 批量拒绝的逐条事实必须随错误事实到达模板数据：HTML 响应没有另一份 JSON data，
// 模板若拿不到 Items，就只能播报一句「失败了」，用户无从定位具体对象。
func TestFailureDataCarriesBlockedItems(t *testing.T) {
	err := blockedFailure{items: []facing.BlockedItem{
		{ID: "1", Name: "a", ReasonKey: "k", Fallback: "f"},
	}}
	data := BuildFailureData(409, facing.Fact{Key: "batchRejected", Business: true}, err)
	if len(data.Items) != 1 || data.Items[0].Name != "a" {
		t.Fatalf("批量拒绝明细必须随错误事实到达模板：%+v", data.Items)
	}
}

// 单条业务错误没有明细时保持为空，模板仍只播报一句。
func TestFailureDataWithoutBlockedItems(t *testing.T) {
	data := BuildFailureData(400, facing.Fact{Key: "single", Business: true}, nil)
	if len(data.Items) != 0 {
		t.Fatalf("单条错误不应凭空产生明细：%+v", data.Items)
	}
}
