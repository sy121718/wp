package routers

import (
	"context"
	"errors"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/pipeline"
	"testing"
)

type blockPageTarget struct {
	pagecontract.PageService
	hit string
}

func (p *blockPageTarget) MarkStaleForBlock(_ context.Context, id string) error {
	p.hit = id
	return nil
}

type blockProjectTarget struct{ projectcontract.ProjectService }

func (p blockProjectTarget) ListThemesByBlockID(context.Context, string) ([]projectdto.ThemeResp, error) {
	return nil, nil
}

type blockPresentationTarget struct {
	kind, key string
	err       error
}

func (p *blockPresentationTarget) MarkStaleByDependency(_ context.Context, kind, key string) ([]string, error) {
	p.kind, p.key = kind, key
	return nil, p.err
}
func TestBlockChangesReachBothPublishingSources(t *testing.T) {
	pages := &blockPageTarget{}
	instances := &blockPresentationTarget{}
	propagate := BlockStalePropagator(pages, blockProjectTarget{}, instances)
	if err := propagate(t.Context(), "shared-block"); err != nil {
		t.Fatal(err)
	}
	want := pipeline.BlockKey("shared-block")
	if pages.hit != "shared-block" || instances.kind != want.Kind || instances.key != want.Key {
		t.Fatalf("块变更必须同时标记两种发布来源：%+v %+v", pages, instances)
	}
	instances.err = errors.New("依赖写入失败")
	if err := propagate(t.Context(), "shared-block"); !errors.Is(err, instances.err) {
		t.Fatalf("不得吞掉自动实例标记错误：%v", err)
	}
}
