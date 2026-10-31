package routers

import (
	"context"
	"errors"
	"testing"
	"time"

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/pipeline"
)

type blockPageTarget struct {
	pagecontract.PageService
	hit string

	rebuilt     []string
	rebuildDone chan struct{}
}

func (p *blockPageTarget) MarkStaleForBlock(_ context.Context, id string) ([]string, error) {
	p.hit = id

	return []string{"page-1", "page-2"}, nil
}

func (p *blockPageTarget) RebuildStale(_ context.Context, ids []string) error {
	p.rebuilt = append(p.rebuilt, ids...)
	p.rebuildDone <- struct{}{}
	return nil
}

type blockProjectTarget struct{ projectcontract.ProjectService }

func (p blockProjectTarget) ListThemesByBlockID(context.Context, string) ([]projectdto.ThemeResp, error) {
	return nil, nil
}

type blockPresentationTarget struct {
	kind, key   string
	err         error
	rebuilt     []string
	rebuildDone chan struct{}
}

func (p *blockPresentationTarget) MarkStaleByDependency(_ context.Context, kind, key string) ([]string, error) {
	p.kind, p.key = kind, key
	if p.err != nil {
		return nil, p.err
	}
	return []string{"inst-1"}, nil
}

func (p *blockPresentationTarget) RebuildStale(_ context.Context, ids []string) error {
	p.rebuilt = append(p.rebuilt, ids...)
	p.rebuildDone <- struct{}{}
	return nil
}

func TestBlockChangesReachBothPublishingSources(t *testing.T) {
	pages := &blockPageTarget{rebuildDone: make(chan struct{}, 4)}
	instances := &blockPresentationTarget{rebuildDone: make(chan struct{}, 4)}
	propagate := BlockStalePropagator(pages, blockProjectTarget{}, instances, instances)
	if err := propagate(t.Context(), "shared-block"); err != nil {
		t.Fatal(err)
	}
	want := pipeline.BlockKey("shared-block")
	if pages.hit != "shared-block" || instances.kind != want.Kind || instances.key != want.Key {
		t.Fatalf("块变更必须同时标记两种发布来源：%+v %+v", pages, instances)
	}

	waitFor(t, pages.rebuildDone)
	waitFor(t, instances.rebuildDone)
	if len(pages.rebuilt) == 0 {
		t.Error("块变更后未自动重建页面（标记与重建分家了）")
	}
	if len(instances.rebuilt) == 0 {
		t.Error("块变更后未自动重建自动发布实例")
	}

	instances.err = errors.New("依赖写入失败")
	if err := propagate(t.Context(), "shared-block"); !errors.Is(err, instances.err) {
		t.Fatalf("不得吞掉自动实例标记错误：%v", err)
	}
}

func waitFor(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("等待自动重建超时")
	}
}

type blockRefProjectTarget struct {
	projectcontract.ProjectService
	themes []projectdto.ThemeResp
	err    error
}

func (p *blockRefProjectTarget) ListThemesByBlockID(context.Context, string) ([]projectdto.ThemeResp, error) {
	return p.themes, p.err
}

type blockRefPageTarget struct {
	pagecontract.PageService
	usages []blockcontract.BlockUsage
	err    error
}

func (p *blockRefPageTarget) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return p.usages, p.err
}

type blockRefTemplateTarget struct {
	contenttemplatecontract.ContentTemplateService
	usages []blockcontract.BlockUsage
	err    error
}

func (p *blockRefTemplateTarget) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return p.usages, p.err
}

type blockRefPresentationTarget struct {
	presentationcontract.PresentationService
	usages []blockcontract.BlockUsage
	err    error
}

func (p *blockRefPresentationTarget) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return p.usages, p.err
}

type blockRefBlockTarget struct {
	blockcontract.BlockService
	usages []blockcontract.BlockUsage
	err    error
}

func (p *blockRefBlockTarget) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return p.usages, p.err
}

func oneRef(kind blockcontract.BlockUsageKind, id, label string) []blockcontract.BlockUsage {
	return []blockcontract.BlockUsage{{Kind: kind, EntityID: id, Label: label}}
}

func TestBlockReferenceCheckerMergesAllSourceRefs(t *testing.T) {
	projects := &blockRefProjectTarget{themes: []projectdto.ThemeResp{{ID: "theme-1", Name: "默认主题"}}}
	pages := &blockRefPageTarget{usages: oneRef(blockcontract.UsageKindPageDocument, "page-1", "/index")}
	templates := &blockRefTemplateTarget{usages: oneRef(blockcontract.UsageKindContentTemplate, "tpl-1", "商品详情")}
	presents := &blockRefPresentationTarget{usages: oneRef(blockcontract.UsageKindPresentationInstance, "inst-1", "/product/a")}
	blocks := &blockRefBlockTarget{usages: oneRef(blockcontract.UsageKindBlockDocument, "blk-outer", "外层块")}

	checker := BlockReferenceChecker(blocks, pages, projects, presents, templates)
	got, err := checker(t.Context(), "shared-block")
	if err != nil {
		t.Fatalf("引用合并不应报错：%v", err)
	}
	want := map[blockcontract.BlockUsageKind]string{
		blockcontract.UsageKindThemeSlot:            "theme-1",
		blockcontract.UsageKindPageDocument:         "page-1",
		blockcontract.UsageKindContentTemplate:      "tpl-1",
		blockcontract.UsageKindPresentationInstance: "inst-1",
		blockcontract.UsageKindBlockDocument:        "blk-outer",
	}
	if len(got) != len(want) {
		t.Fatalf("应合并 5 条来源各一条引用，实际 %d 条：%#v", len(got), got)
	}
	for _, u := range got {
		wantID, ok := want[u.Kind]
		if !ok {
			t.Fatalf("出现了未预期的引用类别 %q：%#v", u.Kind, got)
		}
		if u.EntityID != wantID {
			t.Fatalf("类别 %q 的实体应是 %q，实际 %q", u.Kind, wantID, u.EntityID)
		}
	}
}

func TestBlockReferenceCheckerPropagatesSourceError(t *testing.T) {
	srcErr := errors.New("页面引用反查失败")
	projects := &blockRefProjectTarget{}
	pages := &blockRefPageTarget{err: srcErr}
	checker := BlockReferenceChecker(&blockRefBlockTarget{}, pages, projects,
		&blockRefPresentationTarget{}, &blockRefTemplateTarget{})
	if _, err := checker(t.Context(), "shared-block"); !errors.Is(err, srcErr) {
		t.Fatalf("来源查询失败必须向上传播，实际 %v", err)
	}

	themeErr := errors.New("主题反查失败")
	checker2 := BlockReferenceChecker(&blockRefBlockTarget{}, &blockRefPageTarget{},
		&blockRefProjectTarget{err: themeErr}, &blockRefPresentationTarget{}, &blockRefTemplateTarget{})
	if _, err := checker2(t.Context(), "shared-block"); !errors.Is(err, themeErr) {
		t.Fatalf("主题来源失败必须向上传播，实际 %v", err)
	}
}

func TestBlockReferenceCheckerKeepsOtherSourcesWhenOneMissing(t *testing.T) {
	projects := &blockRefProjectTarget{themes: []projectdto.ThemeResp{{ID: "theme-1", Name: "默认主题"}}}
	pages := &blockRefPageTarget{usages: oneRef(blockcontract.UsageKindPageDocument, "page-1", "/index")}
	blocks := &blockRefBlockTarget{usages: oneRef(blockcontract.UsageKindBlockDocument, "blk-outer", "外层块")}

	checker := BlockReferenceChecker(blocks, pages, projects, nil, nil)
	got, err := checker(t.Context(), "shared-block")
	if err != nil {
		t.Fatalf("契约缺失不应让整条判据失败：%v", err)
	}
	if len(got) != 3 {
		t.Fatalf("缺席的契约只应影响它那一类，期望 3 条（主题 + 页面 + 块），实际 %d：%#v", len(got), got)
	}
}
