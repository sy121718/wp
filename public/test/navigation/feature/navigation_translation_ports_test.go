package feature

// navigation_translation_ports_test.go — 旧 public 测试消费者的**唯一迁移点**。
//
// 背景：导航译文的写路径此前靠包级 singleton 注入一个页面句柄
// （navigationhttp.SetNavigationTranslationWriter + handle.SetContentWriter /
// SetPageMarker / SetMenuInvalidator）。那套形态已随译文 MVC 清退：
//
//   - 写入口只剩一个 —— POST /api/navigation/update 的 action="translations" 分支，
//     句柄是 SetupNavigationRoutes 返回的那个 *Handle，写服务经
//     Handle.SetTranslationWriteService(contract.TranslationWriteService) 注入；
//   - 「标页面 / 标实例 / 入队」三处失效不再借页面句柄转手，而是装配层把 page /
//     presentation / build 的**同事务**能力收窄成三个端口，交给 TranslationWriter；
//   - 包级句柄与三个 setter 都已删除（internal/module/navigation/inbound/http 里 grep 为零）。
//
// 本文件因此不再模拟那套注入，而是**按生产装配的形状**构造同一个 TranslationWriter：
// 三个端口都是真实实现（page 的 MarkMenuStaleTx / presentation 的 MarkInstanceMenuStaleTx /
// build 的 EnqueueMenuStaleTx），事务边界与行读取来自同一 navigation model，
// 译文存储是测试库句柄的 i18n.ContentWriter。
//
// 为什么必须用真实端口而不是探针：这批用例要验的正是「保存译文后，绑该菜单的页面与
// 实例真的被标 stale、任务真的入队」。探针只能证明「端口被调用了」，证明不了
// 「依赖键没拼错、命中口径选对了」—— 而后者才是这条链在线上唯一的失败方式。
//
// 表结构一律来自生产迁移（support.NewMigratedPGTestDB），不手抄 CREATE TABLE。

import (
	"context"
	"testing"

	"gorm.io/gorm"

	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pageservice "go_wp/internal/module/page/service"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

// navigationTranslationWriteHandle 一次装配得到的写链（字段与生产装配一一对应）。
type navigationTranslationWriteHandle struct {
	// Handle 真实 HTTP 句柄：写服务已注入（等价 assembly 的 SetTranslationWriteService）。
	Handle *navigationhttp.Handle
	// Writer 写服务本体（与 Handle 上挂的是同一个实例）。
	Writer *navigationservice.TranslationWriter
	// Queue 真实 build 队列服务（断言任务真的入队，而不是「端口被调用过」）。
	Queue *buildservice.Service
}

// wireNavigationTranslationWriter 按生产链路构造写服务并注入句柄。
//
// 与 internal/routers/assembly_navigation_translation.go 的 newNavigationTranslationWriter
// 逐字段同构：差别只有「各模块由测试库句柄构造」这一层装配细节。
// **不在测试里另造一套端口**：探针只能证明「被调用」，证明不了依赖键与命中口径。
//
// 注意：本函数注入到**返回的那个 *Handle**，不碰任何包级句柄（那套已删除）。
// 每个用例用它自己的句柄，用例之间因此不会互相污染 —— 旧 singleton 的 t.Cleanup 竞争
// 也随之消失。
func wireNavigationTranslationWriter(t *testing.T, db *gorm.DB, handle *navigationhttp.Handle,
	navSvc *navigationservice.Service, projects projectcontract.ProjectService) *navigationTranslationWriteHandle {
	t.Helper()
	if db == nil {
		t.Fatal("wireNavigationTranslationWriter 需要真实迁移库")
	}
	if handle == nil {
		t.Fatal("wireNavigationTranslationWriter 需要 SetupNavigationRoutes 返回的真实句柄")
	}
	if navSvc == nil {
		t.Fatal("wireNavigationTranslationWriter 需要真实导航服务（事务内来源索引在它身上）")
	}

	// 端口取值走**同一个构造点**（navigation_write_ports_fixture_test.go 的 navWritePortsFor）：
	// 生产装配里译文写链与菜单 CRUD 共用同一批适配器，测试里若各装一套，
	// 「译文保存标了页面、菜单保存没标」这种口径漂移就测不出来。
	ports := navWritePortsFor(db, projects)
	navModel := navigationmodel.NewModel(db)
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
		Content:   i18n.NewContentWriter(db),
		Pages:     ports.Pages,
		Instances: ports.Instances,
		Queue:     ports.Queue,
		Tx:        navModel,
	})
	handle.SetTranslationWriteService(writer)
	return &navigationTranslationWriteHandle{Handle: handle, Writer: writer, Queue: ports.BuildSvc}
}

// —— 端口形状断言 ——
//
// 生产侧在 assembly_publish.go 用匿名接口 + panic 做 fail-fast；这里用编译期断言，
// 让「某个模块没实现端口」在**编译**阶段暴露，而不是等到某条用例跑到保存分支才 500。
var (
	_ navigationcontract.PageStalePort           = (*pageservice.Service)(nil)
	_ navigationcontract.PresentationStalePort   = (*presentationservice.Service)(nil)
	_ navigationcontract.MenuStaleEnqueuePort    = (*buildservice.Service)(nil)
	_ navigationcontract.TranslationWriteService = (*navigationservice.TranslationWriter)(nil)
)

// menuStaleJobs 该工程当前的 menu_stale 任务条数（真实 build_jobs 表，迁移 698）。
//
// 判据口径写清：按 `source_type = 'menu_stale'` 且 `project_id` 过滤，只数**未完成**的行
// （pending / running / failed 都算「已入队但尚未成功」）。计数在**保存之后**读，
// 事务已提交，因此能证明「入队与译文同进同出」—— 探针计数证明不了这一点：
// 事务回滚后探针照样记过账。
func menuStaleJobs(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM build_jobs
		WHERE source_type = ? AND project_id = ? AND status <> ?`,
		buildmodel.SourceTypeMenuStale, projectID, buildmodel.StatusSucceeded).Scan(&n).Error; err != nil {
		t.Fatalf("统计 menu_stale 任务失败: %v", err)
	}
	return n
}

// pageStaleCount 该工程被标记为待重建的手工页面数（stale = true）。
//
// 这是**真实列**的读回，不是端口调用计数：译文保存后页面确实处于「待重建」，
// 才说明 page 侧的命中口径（依赖键 menu:{project}:{kind}）拼对了。
// 拼错时端口照样被调用（甚至返回 0 行也不报错），只有读回 pages.stale 才看得见。
func pageStaleCount(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM pages WHERE project_id = ? AND stale = true`,
		projectID).Scan(&n).Error; err != nil {
		t.Fatalf("统计待重建页面失败: %v", err)
	}
	return n
}
