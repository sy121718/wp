package unit

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"reflect"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/pkg/facing"
)

func TestBlockDeleteReportsReferenceDetail(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.SetReferenceUsageChecker(func(context.Context, string) ([]blockcontract.BlockUsage, error) {
		return []blockcontract.BlockUsage{
			{Kind: blockcontract.UsageKindBlockDocument, ProjectID: e.projectID, EntityID: "outer-block", Label: "外层块"},
			{Kind: blockcontract.UsageKindContentTemplate, ProjectID: e.projectID, EntityID: "tpl-1", Label: "商品详情模板"},
		}, nil
	})
	b := e.createBlock(t, "内层块", "block")

	err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: b.ID})
	if !errors.Is(err, blockcontract.ErrBlockInUse) {
		t.Fatalf("期望 ErrBlockInUse，实际 %v", err)
	}
	var inUse *blockcontract.BlockInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("拒绝错误必须携带引用明细（*BlockInUseError），实际 %T: %v", err, err)
	}
	if len(inUse.Usages) != 2 {
		t.Fatalf("引用明细条数应为 2，实际 %d：%#v", len(inUse.Usages), inUse.Usages)
	}
	// 错误本体只交业务 key；引用明细由 BlockedItems 交给失败的 HTML 出口逐条取词，
	// 内部枚举（block_document）与位置不再拼进 Error()。
	if msg := err.Error(); msg != blockcontract.ErrBlockInUse.Error() {
		t.Fatalf("错误本体只应是业务 key，实际 %q", msg)
	}
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatal("拒绝错误必须实现 BlockedError，失败片段才能列出具体引用")
	}
	items := blocked.BlockedItems()
	if len(items) != 2 {
		t.Fatalf("引用条目应为 2 条，实际 %d：%+v", len(items), items)
	}
	if items[0].Name != "外层块" || items[0].ReasonKey != "MsgBlockUsageBlockDocument" {
		t.Fatalf("首条应带位置与类别词条，实际 %+v", items[0])
	}
	if items[1].Name != "商品详情模板" || items[1].ReasonKey != "MsgBlockUsageContentTemplate" {
		t.Fatalf("次条应带位置与类别词条，实际 %+v", items[1])
	}
	current, readErr := e.svc.Detail(ctx, &blockdto.DetailReq{ProjectID: e.projectID, ID: b.ID})
	var original, persisted any
	if err := json.Unmarshal(b.Document, &original); err != nil {
		t.Fatal(err)
	}
	if current != nil {
		if err := json.Unmarshal(current.Document, &persisted); err != nil {
			t.Fatal(err)
		}
	}
	if readErr != nil || current.Version != b.Version || !reflect.DeepEqual(original, persisted) {
		t.Fatalf("拒绝不得改变块: %+v %v", current, readErr)
	}

}

func TestBlockDeleteCheckFailureStillRefuses(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.SetReferenceUsageChecker(func(context.Context, string) ([]blockcontract.BlockUsage, error) {
		return nil, errors.New("反查失败")
	})
	b := e.createBlock(t, "查不出引用的块", "block")
	err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: b.ID})
	if !errors.Is(err, blockcontract.ErrBlockInUse) {
		t.Fatalf("检查失败必须按被引用处理（否则一次读取失败就放行了删除），实际 %v", err)
	}
}

func TestBlockDeleteWithoutCheckerStillRefuses(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.SetReferenceChecker(nil)
	b := e.createBlock(t, "未装配检查器的块", "block")
	if err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: b.ID}); !errors.Is(err, blockcontract.ErrBlockInUse) {
		t.Fatalf("检查器缺失时必须拒绝删除，实际 %v", err)
	}
}
