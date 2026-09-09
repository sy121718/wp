package pageservice

// page_dependency.go — 依赖记录写入与精确 fan-out（docs/03-pipeline.md §8，PIPE-3）。
//
// 两个方向：
//   写入：构建成功后把本次产物的依赖集合落进 page_dependencies（§8.2「按 Artifact
//         可重建的查询投影」）；
//   反查：依赖源变更时按 (kind,key) 找出真正受影响的页面并标记 stale（§8.2 失效查询），
//         可选自动重建（§8.3：默认只产生 staged Artifact，已发布的页面才回写线上）。
//
// 与既有 MarkStaleFor*（主题/块/i18n 的**全站或按主题**标记）的关系：
// 那是「来源自身无法精确表达影响面」时的保守标记，保持原样不退化；
// 本文件是「产物声明过依赖」时的精确路径，两者互补。

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	plugincontract "go_wp/internal/module/plugin/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// maxAutoRebuildPages 单次依赖失效触发的自动重建上限。
//
// 为什么需要上限：自动重建发生在内容写入的请求内（PIPE-2 构建队列尚未落地），
// 无界重建会让一次内容保存耗时随站点规模线性增长。超限的页面保持 stale，
// 由后台「构建待重建页面」或下次内容变更继续收敛。
const maxAutoRebuildPages = 20

// SourceType 实现 pipeline.DependencyTarget：本服务是手工 Page 来源。
func (s *Service) SourceType() string { return pipeline.SourceTypePage }

// MarkStaleByDependency 实现 pipeline.DependencyTarget：按依赖源精确标记。
func (s *Service) MarkStaleByDependency(ctx context.Context, kind, key string) ([]string, error) {
	return s.model.MarkStaleByDependency(ctx, kind, key, time.Now().UTC())
}

// RebuildStale 实现 pipeline.StaleRebuilder：重建受影响的页面。
//
// 策略（§8.3）：每个受影响页面按站点启用语言逐个构建（只产生 staged Artifact）；
// 若该语言**此前已发布**，构建成功后自动发布，保证线上与内容一致——
// 这是「CMS 变更自动发布」的落地口径：从未发布过的页面不会被自动上线。
//
// 单个页面失败不阻断其余页面（记日志后继续），返回值为 nil：
// 调用方是内容写入的后置副作用，失败已由 stale 标记兜底。
func (s *Service) RebuildStale(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > maxAutoRebuildPages {
		logger.Scene("dependency").With("affected", len(ids)).With("limit", maxAutoRebuildPages).
			Warn("自动重建超出单次上限，剩余页面保持 stale 等待后续触发")
		ids = ids[:maxAutoRebuildPages]
	}
	rebuilt, published := 0, 0
	for _, id := range ids {
		page, err := s.model.GetByID(ctx, id)
		if err != nil {
			logger.Scene("dependency").With("page_id", id).Warn("自动重建跳过：页面不存在或已删除")
			continue
		}
		for _, lang := range s.enabledLangsOf(ctx, page.ProjectID) {
			if ctx.Err() != nil {
				return nil
			}
			if _, berr := s.Build(ctx, &pagedto.BuildReq{ID: id, Lang: lang}); berr != nil {
				logger.Scene("dependency").With("page_id", id).With("lang", lang).
					Error(berr, "依赖失效后的自动重建失败（页面保持 stale）")
				continue
			}
			rebuilt++
			// 仅「此前已发布」的语言自动回写线上（未发布页面不自动上线）。
			path, perr := s.publishedPathOf(ctx, page, lang)
			if perr != nil || path == "" {
				continue
			}
			if _, perr = s.Publish(ctx, &pagedto.PublishReq{ID: id, Lang: lang}); perr != nil {
				logger.Scene("dependency").With("page_id", id).With("lang", lang).
					Error(perr, "自动重建后的自动发布失败（产物已暂存，保持 stale）")
				continue
			}
			published++
		}
	}
	if rebuilt > 0 {
		logger.Scene("dependency").With("rebuilt", rebuilt).With("published", published).
			Info("依赖失效后的自动重建完成")
	}
	return nil
}

// persistDependencies 把本次产物的依赖集合写入 page_dependencies。
//
// 失败只记日志：依赖记录是失效追踪的投影，不是构建输入，不阻断发布主链。
// revision 为 null 的 runtime 依赖同样落库（Manifest 声明），但失效查询会跳过。
func (s *Service) persistDependencies(ctx context.Context, pageID, artifactID string, deps []pipeline.Dependency) {
	if strings.TrimSpace(pageID) == "" || strings.TrimSpace(artifactID) == "" {
		return
	}
	now := time.Now().UTC()
	rows := make([]pagemodel.DependencyEntity, 0, len(deps))
	seen := map[[2]string]bool{}
	for _, d := range deps {
		kind, key := strings.TrimSpace(d.Kind), strings.TrimSpace(d.Key)
		if kind == "" || key == "" {
			continue
		}
		// Manifest 已按 (kind,key) 去重排序，此处再兜一层：依赖表主键是
		// (artifact_id, kind, key)，重复插入会整批失败。
		if seen[[2]string{kind, key}] {
			continue
		}
		seen[[2]string{kind, key}] = true
		row := pagemodel.DependencyEntity{
			PageID: pageID, ArtifactID: artifactID,
			DependencyKind: kind, DependencyKey: key, LastChecked: now,
		}
		if rev := strings.TrimSpace(d.Revision); rev != "" {
			r := rev
			row.Revision = &r
		}
		rows = append(rows, row)
	}
	if err := s.model.ReplaceDependencies(ctx, artifactID, rows); err != nil {
		logger.Scene("dependency").With("page_id", pageID).With("artifact_id", artifactID).
			Error(err, "依赖记录写入失败（已降级，不影响构建结果）")
	}
}

// persistDependenciesFromManifest 从产物 Manifest 反序列化依赖并落库。
//
// 用途：产物行早已存在（PIPE-3 之前归档的产物、或依赖行被手工清理）时，
// 发布路径没有经过 ensureArtifactRow，依赖表可能是空的——发布时补写一次，
// 保证「活跃产物必有依赖记录」这一 fan-out 前提成立。
func (s *Service) persistDependenciesFromManifest(ctx context.Context, pageID, artifactID string, manifestJSON json.RawMessage) {
	if len(manifestJSON) == 0 {
		return
	}
	var m pipeline.Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		logger.Scene("dependency").With("page_id", pageID).With("artifact_id", artifactID).
			Warn("产物 Manifest 解析失败，跳过依赖记录补写")
		return
	}
	s.persistDependencies(ctx, pageID, artifactID, m.Dependencies)
}

// pageDependencyKeys 由页面记录与文档推导本次构建的依赖源集合。
//
// 组成（docs/03-pipeline.md §8.2 典型 fan-out 的可精确表达部分）：
//  1. block:{id}        —— core.globalref 引用块 + settings.structure 页眉/页脚绑定块；
//  2. direct_content    —— 页面绑定的内容实体（pages.content_target_type/id）；
//  3. content_collection—— 文档中「声明了集合绑定」的插件组件所使用的集合源。
//
// i18n 依赖由 buildDependencies 单独补（已有实现，不在此重复）。
// 菜单/媒体/主题设置依赖暂未登记（见本轮遗留项：需要构建期解析点回传）。
func (s *Service) pageDependencyKeys(ctx context.Context, page *pagemodel.PageEntity) []pipeline.Dependency {
	if page == nil {
		return nil
	}
	keys := map[[2]string]string{}
	add := func(k pipeline.DepKey) {
		keys[[2]string{k.Kind, k.Key}] = k.Key
	}
	// 1. 内容实体绑定（page/article/product/category 等 kind）。
	if t := strings.TrimSpace(page.ContentTargetType); t != "" && t != "none" {
		if page.ContentTargetID != nil {
			if id := strings.TrimSpace(*page.ContentTargetID); id != "" {
				add(pipeline.DirectContentKey(t, id))
			}
		}
	}
	// 2. 文档内块引用 + 页眉/页脚绑定 + 集合源。
	doc := page.DraftDocument
	if len(doc) > 0 {
		if parsed, err := builder.ParsePage(doc); err == nil {
			for _, id := range builder.ReferencedBlockIDs(parsed.Root) {
				add(pipeline.BlockKey(id))
			}
			if id := strings.TrimSpace(parsed.Settings.Structure.HeaderBlockID); id != "" {
				add(pipeline.BlockKey(id))
			}
			if id := strings.TrimSpace(parsed.Settings.Structure.FooterBlockID); id != "" {
				add(pipeline.BlockKey(id))
			}
			for _, src := range s.collectionSourcesOf(ctx, parsed.Root) {
				add(pipeline.DepKey{Kind: pipeline.DepKindContentCollection, Key: "collection:" + src})
			}
		}
	}
	out := make([]pipeline.Dependency, 0, len(keys))
	for key := range keys {
		out = append(out, pipeline.Dependency{Kind: key[0], Key: key[1]})
	}
	return out
}

// collectionSourcesOf 收集文档中集合组件实际使用的集合源（如 content:product）。
//
// 集合源声明在插件组件规格里（spec.Collection.Source），文档节点只带组件类型，
// 因此必须经插件装配素材反查；未启用插件时返回空（内置组件无集合绑定）。
func (s *Service) collectionSourcesOf(ctx context.Context, roots []*core.Node) []string {
	asm := s.enabledAssembly(ctx)
	if asm == nil || len(asm.Specs) == 0 {
		return nil
	}
	resolver := plugincontract.AssemblyResolver(asm)
	seen := map[string]bool{}
	var out []string
	var walk func(n *core.Node)
	walk = func(n *core.Node) {
		if n == nil {
			return
		}
		if spec, ok := resolver.LookupPluginComponent(n.Type); ok && spec != nil && spec.Collection != nil {
			if src := strings.TrimSpace(spec.Collection.Source); src != "" && !seen[src] {
				seen[src] = true
				out = append(out, src)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return out
}
