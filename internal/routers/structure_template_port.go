package routers

// structure_template_port.go — 结构模板解析端口的装配侧适配器。
//
// page 模块需要一个「按工程作用域取某套结构模板的当前版本文档」的能力
//（pipeline.StructureTemplatePort），而提供方是 contenttemplate 的对外契约。
// page 与 contenttemplate 之间不互相 import 具体实现：这里放一层最小适配，
// 把契约收窄成消费者真正要的那一件事（一份文档）。
//
// presentation 侧不需要这层：它本来就持有 contenttemplate 契约，直接实现了同一个端口
//（见 presentation/service/presentation_blocks.go 的 ResolveStructureDocument）。

import (
	"context"
	"errors"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// structureTemplatePortAdapter 把 contenttemplate 契约适配为 pipeline.StructureTemplatePort。
type structureTemplatePortAdapter struct {
	svc contenttemplatecontract.ContentTemplateService
}

// ResolveStructureDocument 按工程作用域取结构模板当前版本文档。
//
// 三个失败分支（契约缺失 / 模板不存在或文档非法 / 文档为空）对构建期的含义完全一致：
// 这套模板不可用 —— 由 pipeline.BuildStructureSlots 回退到该槽位的块绑定。
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

// contentTemplateInvalidator 内容模板的失效扇出 + **影响面回执**。
//
// 为什么不让 contenttemplate 直接持有 *pipeline.Fanout：Fanout.Invalidate 丢弃了
// InvalidateKeys 的返回值 —— 于是「这次模板换代到底让哪些页面 / 实例变 stale」
// 在日志里完全看不到，只剩下一句「模板已更新」。影响面是这套能力的基本回执：
// 运营改的是全站页眉，必须能看到它波及了多少张页面。
type contentTemplateInvalidator struct {
	fanout *pipeline.Fanout
}

// invalidatorSampleLimit 回执里每个来源最多列几条样本 id（可定位即可，不刷日志）。
const invalidatorSampleLimit = 5

// Invalidate 实现 contenttemplate.DependencyInvalidator：扇出 + 记影响面回执。
func (a contentTemplateInvalidator) Invalidate(ctx context.Context, kind, key string) {
	if a.fanout == nil {
		return
	}
	affected := a.fanout.InvalidateKeys(ctx, pipeline.DepKey{Kind: kind, Key: key})
	if len(affected) == 0 {
		// 没有任何产物声明该依赖：不算异常（模板可能还没被任何页面绑定），
		// 但值得留一条痕迹 —— 否则「改了模板但无人受影响」与「依赖没登记上」
		// 在日志里长得一模一样。
		logger.Scene("contenttemplate").With("kind", kind).With("key", key).
			Info("模板变更未命中任何产物（可能尚无页面 / 实例引用它）")
		return
	}
	entry := logger.Scene("contenttemplate").With("kind", kind).With("key", key)
	for _, st := range []string{pipeline.SourceTypePage, pipeline.SourceTypePresentation} {
		ids := affected[st]
		if len(ids) == 0 {
			continue
		}
		entry = entry.With(st+"_count", len(ids)).With(st+"_sample", sampleIDs(ids))
	}
	entry.Info("模板变更的失效影响面（已标记待重建）")
}

// sampleIDs 取前 invalidatorSampleLimit 条 id 拼成一行（超出部分用 … 提示）。
func sampleIDs(ids []string) string {
	if len(ids) <= invalidatorSampleLimit {
		return strings.Join(ids, ",")
	}
	return strings.Join(ids[:invalidatorSampleLimit], ",") + ",…"
}
