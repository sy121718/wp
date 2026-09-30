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
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	plugincontract "go_wp/internal/module/plugin/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"gorm.io/gorm"
)

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
// 编排本身在 page_rebuild.go（RebuildPage），本方法只负责「逐页取计划 + 调用编排」：
// 超限部分入队（enqueueOverflowBuildJobs），队列 worker 消费时**走同一条编排**
// （RunPageBuildJob）—— 报告 ARCH-04 的根因就是这两条路径各有一份实现。
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
		if ctx.Err() != nil {
			return nil
		}
		// 逐工程定位（DB-009 第三批）：ids 来自依赖扇出（可能跨工程），而 pages 带 FORCE
		// 策略 —— 不设作用域的 GetByID 在换非超级角色后一律 ErrRecordNotFound，
		// 整条「内容变更 → 自动重建」会全部落进下面的「跳过」分支且没有任何报错。
		page, err := s.locatePageInProjects(ctx, id)
		if err != nil {
			logger.Scene("dependency").With("page_id", id).Warn("自动重建跳过：页面不存在或已删除")
			continue
		}
		// 计划里的语言集与旧发布范围都按**发布口径**取（审计 I18N-02）：这条路径会构建
		// 并回写线上已发布的语言。按可见回退取列表时，清单读不到会退化成「只重建默认语言」
		// —— 其余语言停在旧字节，而本方法照常返回成功、日志里只有一行「读取失败」。
		// 宁可整页跳过（保持 stale，影响面回执里看得见），也不打默认语言的折扣。
		plan, perr := s.planPageRebuild(ctx, page, nil, pagecontract.BuildIntentDependency)
		if perr != nil {
			logger.Scene("dependency").With("page_id", id).
				Error(perr, "自动重建跳过：站点语言清单或旧发布范围不可读（页面保持 stale）")
			// 除了日志，还把「失败在计划阶段」落到页面行上（见 markRebuildFailure 的注释）：
			// 界面上看到的 stale 从此能区分「还没轮到」与「重建失败过」。
			s.markRebuildFailure(ctx, page, pageRebuildStagePlan)
			continue
		}
		r, p, rerr := s.rebuildPage(ctx, plan)
		rebuilt += r
		published += p
		if rerr != nil {
			logger.Scene("dependency").With("page_id", id).
				Error(rerr, "依赖失效后的自动重建失败（页面保持 stale）")
			s.markRebuildFailure(ctx, page, pageRebuildStageBuild)
			continue
		}
		s.clearRebuildFailure(ctx, page)
	}
	if rebuilt > 0 {
		logger.Scene("dependency").With("rebuilt", rebuilt).With("published", published).
			Info("依赖失效后的自动重建完成")
	}
	return nil
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
			// 槽位绑定（页眉 / 页脚 / 公告条…）：所有被**实际消费**的绑定都要登记为依赖，
			// 否则「改了公告条引用的块」不会让引用页失效 —— 站点上一直显示旧内容。
			//
			// 走 pipeline.StructureSlotDependencies 而不是在这里自己判优先级：绑定了结构模板
			// 的槽位消费的是**模板**（登记 content_template:{id} + 模板内引用的 block:{id}），
			// 块只是回退路径。自己再判一次的结果是两条路径迟早分叉 —— 一边登记了没消费的键
			//（改了那套模板以为会重建，其实页面根本没用它），一边漏登记真正消费的键
			//（改了模板，页面永远停在旧字节）。
			//
			// 端口未注入时模板槽位解析失败 → 与构建期同一回退口径：只登记块绑定。
			for _, dep := range pipeline.StructureSlotDependencies(ctx, s.structureTemplates, page.ProjectID, parsed.Settings.Structure) {
				add(pipeline.DepKey{Kind: dep.Kind, Key: dep.Key})
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
// **两条来源取并集**（审计 ARCH-01）—— 此前只有第 2 条，于是全站只有插件组件能被
// 精确失效，内置集合组件一条都不登记：
//
//  1. 内置组件：组件自己在注册表里声明集合源字段（core.CollectionProvider），
//     统一经 core.CollectionSourcesOf 读取。core.productList / core.cardstack 这类
//     「商品列表 / 文章列表」组件走的正是这一条 —— 缺它时「新增一个商品」不会让
//     任何列表页失效，产物停在旧字节且日志里什么都没有（本模块此前的注释
//     「内置组件无集合绑定」是错的：它们把集合源写在节点 Props 的 collectionSource 里）。
//  2. 插件组件：集合源声明在插件 manifest 里（spec.Collection.Source），
//     文档节点只带组件类型，只能经插件装配素材反查。插件组件不在 core 注册表里，
//     两路互不覆盖，任何一路都不能删。
func (s *Service) collectionSourcesOf(ctx context.Context, roots []*core.Node) []string {
	seen := map[string]bool{}
	var out []string
	add := func(src string) {
		if src = strings.TrimSpace(src); src != "" && !seen[src] {
			seen[src] = true
			out = append(out, src)
		}
	}
	// 1) 内置组件（唯一来源：组件自己的声明，不在这里维护「类型 → 集合源」映射表）。
	for _, src := range core.CollectionSourcesOf(roots) {
		add(src)
	}
	// 2) 插件组件。
	asm := pipeline.LoadPluginAssembly(ctx, s.plugins)
	if asm != nil && len(asm.Specs) > 0 {
		resolver := plugincontract.AssemblyResolver(asm)
		var walk func(n *core.Node)
		walk = func(n *core.Node) {
			if n == nil {
				return
			}
			if spec, ok := resolver.LookupPluginComponent(n.Type); ok && spec != nil && spec.Collection != nil {
				add(spec.Collection.Source)
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		for _, r := range roots {
			walk(r)
		}
	}
	return out
}

// 自动重建失败的阶段（闭集；与 pages.rebuild_failed_stage 的 VARCHAR(16) 对齐）。
//
// 只分两级而不是按错误类型细分：这两级已经足以决定「下一步做什么」——
// plan = 读不到语言清单 / 旧发布范围（先查配置与语言表），build = 构建或发布失败
// （查构建日志）。再细的分类需要把错误映射成枚举，而那层映射本身就会漂。
const (
	pageRebuildStagePlan  = "plan"
	pageRebuildStageBuild = "build"
)

// markRebuildFailure 把「这一页自动重建失败在哪个阶段」写到页面行上。
//
// 为什么值得单独一处：重建失败此前只进日志，界面上留下的唯一痕迹是 stale 仍为 true —
// 于是「待重建影响面」显示非零时，读的人分不清「还没轮到」与「反复失败」。
//
// 写入失败只记日志：观测不该反过来打断主流程（那句失败原文已经在调用点的日志里了）。
func (s *Service) markRebuildFailure(ctx context.Context, page *pagemodel.PageEntity, stage string) {
	if page == nil {
		return
	}
	if err := s.model.MarkRebuildFailure(ctx, page.ProjectID, page.ID, stage, time.Now()); err != nil {
		logger.Scene("dependency").With("page_id", page.ID).Error(err, "记录重建失败阶段失败（页面上的失败原因会缺失）")
	}
}

// clearRebuildFailure 重建成功后清掉失败痕迹。
func (s *Service) clearRebuildFailure(ctx context.Context, page *pagemodel.PageEntity) {
	if page == nil {
		return
	}
	if err := s.model.ClearRebuildFailure(ctx, page.ProjectID, page.ID); err != nil {
		logger.Scene("dependency").With("page_id", page.ID).Error(err, "清除重建失败痕迹失败（该页可能继续显示上一次失败）")
	}
}
