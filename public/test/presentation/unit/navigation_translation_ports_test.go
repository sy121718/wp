package unit

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

var (
	_ navigationcontract.PresentationStalePort   = (*presentationservice.Service)(nil)
	_ navigationcontract.TranslationWriteService = (*navigationservice.TranslationWriter)(nil)
)
