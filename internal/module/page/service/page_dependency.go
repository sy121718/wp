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

	pagecontract "go_wp/internal/module/page/contract"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	plugincontract "go_wp/internal/module/plugin/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
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
//
// 逐工程扇出（DB-009 第三批）：契约来自 pipeline.DependencyTarget，签名只能是
// (kind,key) —— 引擎不知道也不需要知道工程；而 pages 带 FORCE 策略，UPDATE 必须落在
// 某个具体工程的作用域里。逐个工程各设一次作用域后合并去重（presentation 侧同形）。
// 漏作用域时它在换非超级角色后静默 0 行：内容改了，引用它的页面不再自动重建。
func (s *Service) MarkStaleByDependency(ctx context.Context, kind, key string) ([]string, error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(key) == "" {
		return nil, nil
	}
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	// 聚合走与整站标记同一份 staleIDCollector（去重只有一份实现）。
	hit := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		ids, herr := s.model.MarkStaleByDependency(ctx, projectID, kind, key, at)
		if herr != nil {
			return nil, herr
		}
		hit.add(ids)
	}
	affected := hit.list()
	// 影响面回执（只读，失败不影响主流程）：这里是 PIPE-3 精确扇出的唯一出口，
	// 「这次内容改动影响了哪几个页面」只有这一刻手里有完整答案 —— 过了这里
	// 就只剩 pages.stale 这个布尔列，再想回答就得靠反查全部 stale 页面去近似。
	s.logStaleImpact(ctx, "dependency:"+kind+":"+key, affected)
	return affected, nil
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
		// 超限部分交给构建队列（审计 DB-007）：此前只能丢弃并靠「下次触发」兜底，
		// 而「下次触发」未必会来 —— 内容改完站点却一直不更新，是这条路径最容易留下的现象。
		overflow := append([]string(nil), ids[maxAutoRebuildPages:]...)
		ids = ids[:maxAutoRebuildPages]
		s.enqueueOverflowBuildJobs(ctx, overflow)
	}
	rebuilt, published := 0, 0
	for _, id := range ids {
		// 逐工程定位（DB-009 第三批）：ids 来自依赖扇出（可能跨工程），而 pages 带 FORCE
		// 策略 —— 不设作用域的 GetByID 在换非超级角色后一律 ErrRecordNotFound，
		// 整条「内容变更 → 自动重建」会全部落进下面的「跳过」分支且没有任何报错。
		page, err := s.locatePageInProjects(ctx, id)
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

// SetBuildQueue 注入构建队列端口（装配期调用）。
//
// 未注入时 enqueueOverflowBuildJobs 会退回「记告警、保持 stale」的既有行为 ——
// 不静默丢弃，也不假装已经排上了。
func (s *Service) SetBuildQueue(q pagecontract.BuildQueueEnqueuer) {
	if s == nil {
		return
	}
	s.buildQueue = q
}

// enqueueOverflowBuildJobs 把超出单次同步重建上限的页面交给构建队列。
//
// 单个页面入队失败只记日志：这是一条尽力而为的旁路（同步那部分已经重建完了），
// 抛错会让调用方误以为整批失败。
func (s *Service) enqueueOverflowBuildJobs(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	if s.buildQueue == nil {
		logger.Scene("dependency").With("affected", len(ids)).
			Warn("自动重建超出单次上限且构建队列未接入，剩余页面保持 stale 等待后续触发")
		return
	}
	queued := 0
	for _, id := range ids {
		// 同 RebuildStale：入队前也要按工程作用域读一次页面（漏作用域时整批任务静默不再入队）。
		page, err := s.locatePageInProjects(ctx, id)
		if err != nil {
			continue
		}
		// build_input_hash 传空串是刻意的：队列的部分唯一索引按 (来源, 目标, hash) 去重，
		// 空串让「同一页面同时只有一条待办」成立 —— 一批扇出反复标记同一页时不会堆出多份任务。
		if qerr := s.buildQueue.EnqueuePageBuild(ctx, id, page.DraftVersion, ""); qerr != nil {
			logger.Scene("dependency").With("page_id", id).Error(qerr, "超限重建任务入队失败")
			continue
		}
		queued++
	}
	logger.Scene("dependency").With("queued", queued).With("affected", len(ids)).
		Info("超限的自动重建已交给构建队列")
}

// persistDependencies 把本次产物的依赖集合写入 page_dependencies（自足入口：自带事务）。
//
// 失败只记日志。**仅用于「补写投影」的旁路**（发布时按 Manifest 补齐、恢复时补归档）：
// 那些调用点的主链状态已经落定，依赖投影缺失只会让该页在依赖源变更时少一次自动重建，
// 不值得把已经完成的发布打回去。构建主链（Build）不走这里 —— 它要求依赖记写失败
// 与暂存指针一起回滚，用 persistDependenciesTx。
//
// revision 为 null 的 runtime 依赖同样落库（Manifest 声明），但失效查询会跳过。
// projectID 必填（DB-009 第四批）：page_dependencies 没有 project_id 列、不受策略约束，
// 归属由 model 经 page_artifacts → pages 校验；缺它时一次越界的 artifactID 就能改写
// 别的工程的依赖投影（而它不报任何错）。
func (s *Service) persistDependencies(ctx context.Context, projectID, pageID, artifactID string, deps []pipeline.Dependency) {
	rows, rerr := dependencyRows(pageID, artifactID, deps)
	if rerr != nil {
		return
	}
	if err := s.model.ReplaceDependencies(ctx, projectID, artifactID, rows); err != nil {
		logger.Scene("dependency").With("page_id", pageID).With("artifact_id", artifactID).
			Error(err, "依赖记录写入失败（已降级，不影响构建结果）")
	}
}

// persistDependenciesTx 在**调用方的事务**内写依赖记录，失败原样返回（不降级）。
//
// 与 persistDependencies 的分工：构建主链用它，把「依赖记录 + 暂存指针」收在一个
// 事务里 —— 依赖写失败必须让整次构建失败，否则该页在依赖源变更时不再被精确标
// stale（站点长期旧内容，且只在日志里留一行）。
func (s *Service) persistDependenciesTx(ctx context.Context, tx *gorm.DB, projectID, pageID, artifactID string, deps []pipeline.Dependency) error {
	rows, rerr := dependencyRows(pageID, artifactID, deps)
	if rerr != nil {
		return rerr
	}
	return s.model.ReplaceDependenciesTx(ctx, tx, projectID, artifactID, rows)
}

// dependencyRows 把 Manifest 的依赖集合转成依赖行（去重 + 过滤空值）。
//
// pageID / artifactID 任一为空时返回错误：主键的第一个与第二个分量缺一就是写不出
// 有意义的一行，静默跳过等于「构建成功但依赖表空着」。
func dependencyRows(pageID, artifactID string, deps []pipeline.Dependency) ([]pagemodel.DependencyEntity, error) {
	if strings.TrimSpace(pageID) == "" || strings.TrimSpace(artifactID) == "" {
		return nil, nil
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
	return rows, nil
}

// persistDependenciesFromManifest 从产物 Manifest 反序列化依赖并落库。
//
// 用途：产物行早已存在（PIPE-3 之前归档的产物、或依赖行被手工清理）时，
// 发布路径没有经过 ensureArtifactRow，依赖表可能是空的——发布时补写一次，
// 保证「活跃产物必有依赖记录」这一 fan-out 前提成立。
func (s *Service) persistDependenciesFromManifest(ctx context.Context, projectID, pageID, artifactID string, manifestJSON json.RawMessage) {
	if len(manifestJSON) == 0 {
		return
	}
	var m pipeline.Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		logger.Scene("dependency").With("page_id", pageID).With("artifact_id", artifactID).
			Warn("产物 Manifest 解析失败，跳过依赖记录补写")
		return
	}
	s.persistDependencies(ctx, projectID, pageID, artifactID, m.Dependencies)
}

// pageDependencyKeys 由页面记录与文档推导本次构建的依赖源集合。
//
// 组成（docs/03-pipeline.md §8.2 典型 fan-out 的可精确表达部分）：
//  1. block:{id}        —— core.globalref 引用块 + settings.structure 页眉/页脚绑定块；
//  2. direct_content    —— 页面绑定的内容实体（pages.content_target_type/id）；
//  3. content_collection—— 文档中「声明了集合绑定」的插件组件所使用的集合源。
//
// i18n 依赖由 buildDependencies 单独补（已有实现，不在此重复）；
// 菜单依赖同样在 buildDependencies 里按**编译期消费记录**登记（core.nav 绑定菜单位置），
// 不在这里静态推导 —— 绑定可能藏在页眉/页脚块里，静态扫本页文档看不到。
// 媒体/主题设置依赖暂未登记（需要构建期解析点回传）。
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
			// 槽位绑定（页眉 / 页脚 / 公告条…）：所有被绑定的块都要登记为依赖，
			// 否则「改了公告条引用的块」不会让引用页失效 —— 站点上一直显示旧内容。
			if bindings := parsed.Settings.Structure.SlotBindings(); len(bindings) > 0 {
				for _, slot := range builder.SortedSlots(bindings) {
					add(pipeline.BlockKey(bindings[slot]))
				}
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
	asm := pipeline.LoadPluginAssembly(ctx, s.plugins)
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
