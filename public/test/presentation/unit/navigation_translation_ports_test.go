package unit

// navigation_translation_ports_test.go — 导航译文真实写链的装配夹具（presentation 侧消费者）。
//
// 旧形态：本包曾用 navigationhttp.SetNavigationTranslationWriter(handle) 这类**包级 singleton**
// 注入一个页面句柄，并由 handle.SetMenuInvalidator(navSvc) 借道页面句柄派发失效。
// 那套已随译文 MVC 全部删除（internal/module/navigation/inbound/http 里 grep 为零）。
//
// 现在唯一的写入口是 POST /api/navigation/update 的 action="translations" 分支，
// 写服务经 Handle.SetTranslationWriteService(contract.TranslationWriteService) 注入；
// 「标手工页面 / 标自动发布实例 / 入队重建」由 TranslationWriter 在同一事务里经三个端口完成。
//
// 本夹具按生产装配的形状构造那个 TranslationWriter，**三个端口全部用真实实现**：
//   - page：pageservice.Service.MarkMenuStaleTx（依赖键由 pipeline.MenuKey 构造，
//     命中口径在 page model，测试不复制一份）；
//   - presentation：presentationservice.Service.MarkInstanceMenuStaleTx（用本包 fixture 的实例）；
//   - build：buildservice.Service.EnqueueMenuStaleTx（迁移 698 的序列表）。
//
// 为什么不用探针：本包要验的是「保存导航译文后，绑该位置的自动发布实例被标 stale、
// 没绑的纹丝不动」。探针只能证明端口被调用过，证明不了依赖键拼对、命中口径选对 ——
// 而后者才是这条链唯一的线上失败方式（错键 = 0 行命中且不报错）。

import (
	"context"
	"testing"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

// wirePresentationTranslationWriter 构造真实写链并注入句柄。
//
// 注入目标是**本次用例自己的** handle（生产装配同一处：SetTranslationWriteService）。
// 没有全局状态，因此用例之间不会互相污染 —— 旧 singleton 那套 t.Cleanup 竞争也一并消失。
//
// presentation 端口用 f.pres：本包要验的实例标记必须落在**同一条发布链**上，
// 另造一个 Service 会让用例验的不是它自己的服务。
func wirePresentationTranslationWriter(t *testing.T, f *presFixture, handle *navigationhttp.Handle,
	navSvc *navigationservice.Service, projects projectcontract.ProjectService) *navigationservice.TranslationWriter {
	t.Helper()
	if f == nil || f.db == nil {
		t.Fatal("wirePresentationTranslationWriter 需要已装配的 fixture（PG 不可用时用例应已 skip）")
	}
	if handle == nil || navSvc == nil {
		t.Fatal("wirePresentationTranslationWriter 需要真实句柄与真实导航服务")
	}
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(f.db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(f.db), projects)
	pages := pageservice.NewService(pagemodel.NewPageModel(f.db), artifacts, f.routes, projects, blocks,
		nil, nil, navSvc, nil)
	queue := buildservice.NewService(buildmodel.NewModel(f.db))

	navModel := navigationmodel.NewModel(f.db)
	writer := navigationservice.NewTranslationWriter(navigationservice.TranslationWriterDeps{
		Rows:    navigationservice.NewNavigationRowsTx(navModel),
		Sources: navSvc,
		Projects: navigationservice.ProjectListerFunc(func(ctx context.Context) ([]navigationservice.ProjectEntry, error) {
			list, err := projects.List(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]navigationservice.ProjectEntry, 0, len(list))
			for _, p := range list {
				out = append(out, navigationservice.ProjectEntry{ID: p.ID})
			}
			return out, nil
		}),
		Content:   i18n.NewContentWriter(f.db),
		Pages:     pages,
		Instances: f.pres,
		Queue:     queue,
		Tx:        navModel,
	})
	handle.SetTranslationWriteService(writer)
	return writer
}

// 编译期断言：真实实现必须满足 navigation 契约声明的端口。
//
// 生产侧在 assembly_publish.go 用匿名接口 + panic 做 fail-fast；这里前移到编译期 ——
// 「某个模块没实现端口」在编译阶段就断，而不是等用例跑到保存分支才拿 500。
var (
	_ navigationcontract.PresentationStalePort   = (*presentationservice.Service)(nil)
	_ navigationcontract.TranslationWriteService = (*navigationservice.TranslationWriter)(nil)
)
