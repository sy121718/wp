package unit

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"reflect"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
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
	msg := err.Error()
	for _, want := range []string{string(blockcontract.UsageKindBlockDocument), "外层块",
		string(blockcontract.UsageKindContentTemplate), "商品详情模板"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("拒绝文案应能定位到 %q，实际 %q", want, msg)
		}
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
