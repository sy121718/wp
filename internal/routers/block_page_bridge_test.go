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
	// rebuilt 记录自动重建收到的 id（异步执行，用 rebuildDone 同步断言）。
	rebuilt     []string
	rebuildDone chan struct{}
}

func (p *blockPageTarget) MarkStaleForBlock(_ context.Context, id string) ([]string, error) {
	p.hit = id
	// 回一件命中 id：传播器拿它触发自动重建（本次改动的核心 —— 标记与重建不再分家）。
	return []string{"page-1", "page-2"}, nil
}

// RebuildStale 记录重建调用（自动重建是异步的，断言前需要等待，见下面的 waitFor）。
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

	// 标记之后必须**自动重建**（异步）：只标记不重建正是「改了页眉块、线上一直不变」的来源
	// —— 那段时间里没有任何人工入口能触发它。
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

// waitFor 异步重建完成（带超时，避免实现坏了变成永久挂起）。
func waitFor(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("等待自动重建超时")
	}
}

// —— 删除保护的引用合并（审计 ARCH-02）——

// blockRefProjectTarget 只实现 ListThemesByBlockID 的 project 契约桩。
type blockRefProjectTarget struct {
	projectcontract.ProjectService
	themes []projectdto.ThemeResp
	err    error
}

func (p *blockRefProjectTarget) ListThemesByBlockID(context.Context, string) ([]projectdto.ThemeResp, error) {
	return p.themes, p.err
}

// blockRefPageTarget 只实现 ListBlockSourceRefs 的 page 契约桩。
type blockRefPageTarget struct {
	pagecontract.PageService
	usages []blockcontract.BlockUsage
	err    error
}

func (p *blockRefPageTarget) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return p.usages, p.err
}

// blockRefTemplateTarget 只实现 ListBlockSourceRefs 的 contenttemplate 契约桩。
type blockRefTemplateTarget struct {
	contenttemplatecontract.ContentTemplateService
	usages []blockcontract.BlockUsage
	err    error
}

func (p *blockRefTemplateTarget) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return p.usages, p.err
}

// blockRefPresentationTarget 只实现 ListBlockSourceRefs 的 presentation 契约桩。
type blockRefPresentationTarget struct {
	presentationcontract.PresentationService
	usages []blockcontract.BlockUsage
	err    error
}

func (p *blockRefPresentationTarget) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return p.usages, p.err
}

// blockRefBlockTarget 只实现 ListBlockSourceRefs 的 block 契约桩。
type blockRefBlockTarget struct {
	blockcontract.BlockService
	usages []blockcontract.BlockUsage
	err    error
}

func (p *blockRefBlockTarget) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return p.usages, p.err
}

// oneRef 构造一条引用，Kind 与 Label 足以在断言里区分来源。
func oneRef(kind blockcontract.BlockUsageKind, id, label string) []blockcontract.BlockUsage {
	return []blockcontract.BlockUsage{{Kind: kind, EntityID: id, Label: label}}
}

// TestBlockReferenceCheckerMergesAllSourceRefs 五条来源必须全部参与判据。
//
// 这条用例就是 ARCH-02 的回归护栏：修复前判据只有「主题 + 页面」，引用只存在于
// 嵌套块 / 未发布模板 / 独立实例时删除一路成功，站点在下一次构建才暴露缺失。
// 谁把某一条来源从 BlockReferenceChecker 里去掉，这里立刻少一类。
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

// TestBlockReferenceCheckerPropagatesSourceError 任一来源失败必须整体失败。
//
// service 层对「检查失败」按被引用处理（宁拒勿删）；把失败吞掉变成空集合，
// 才是真正危险的形态 —— 它会让一次读取失败表现为「没有引用」。
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

// TestBlockReferenceCheckerKeepsOtherSourcesWhenOneMissing 契约缺失只影响它自己那一类。
//
// 为什么不是「缺一个就整体失败」：装配层是唯一接线点，缺失由这里的 warn 日志记账；
// 而其它来源仍然有效 —— 让整条判据停摆，等于把「模板引用查不了」升级成「所有引用都查不了」。
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
