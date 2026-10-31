package feature

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

type navigationTranslationWriteHandle struct {
	Handle *navigationhttp.Handle

	Writer *navigationservice.TranslationWriter

	Queue *buildservice.Service
}

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

var (
	_ navigationcontract.PageStalePort           = (*pageservice.Service)(nil)
	_ navigationcontract.PresentationStalePort   = (*presentationservice.Service)(nil)
	_ navigationcontract.MenuStaleEnqueuePort    = (*buildservice.Service)(nil)
	_ navigationcontract.TranslationWriteService = (*navigationservice.TranslationWriter)(nil)
)

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

func pageStaleCount(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM pages WHERE project_id = ? AND stale = true`,
		projectID).Scan(&n).Error; err != nil {
		t.Fatalf("统计待重建页面失败: %v", err)
	}
	return n
}
