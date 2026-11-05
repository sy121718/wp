package routers

import (
	"context"
	"errors"
	publish "go_wp/pkg/publish"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	"go_wp/pkg/logger"
)

type structureTemplatePortAdapter struct {
	svc contenttemplatecontract.ContentTemplateService
}

func (a structureTemplatePortAdapter) ResolveStructureDocument(ctx context.Context, projectID, templateID string) ([]byte, error) {
	if a.svc == nil {
		return nil, errors.New("contenttemplate 契约未装配")
	}
	tpl, err := a.svc.ResolveTemplateByIDScoped(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	if tpl == nil || len(tpl.Document) == 0 {
		return nil, errors.New("结构模板无可用文档")
	}
	return tpl.Document, nil
}

type contentTemplateInvalidator struct {
	fanout *publish.Fanout
}

const invalidatorSampleLimit = 5

func (a contentTemplateInvalidator) Invalidate(ctx context.Context, kind, key string) {
	if a.fanout == nil {
		return
	}
	affected := a.fanout.InvalidateKeys(ctx, publish.DepKey{Kind: kind, Key: key})
	if len(affected) == 0 {

		logger.Scene("contenttemplate").With("kind", kind).With("key", key).
			Info("模板变更未命中任何产物（可能尚无页面 / 实例引用它）")
		return
	}
	entry := logger.Scene("contenttemplate").With("kind", kind).With("key", key)
	for _, st := range []string{publish.SourceTypePage, publish.SourceTypePresentation} {
		ids := affected[st]
		if len(ids) == 0 {
			continue
		}
		entry = entry.With(st+"_count", len(ids)).With(st+"_sample", sampleIDs(ids))
	}
	entry.Info("模板变更的失效影响面（已标记待重建）")
}

func sampleIDs(ids []string) string {
	if len(ids) <= invalidatorSampleLimit {
		return strings.Join(ids, ",")
	}
	return strings.Join(ids[:invalidatorSampleLimit], ",") + ",…"
}
