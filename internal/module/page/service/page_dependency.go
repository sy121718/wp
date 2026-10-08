package pageservice

// 两个方向：
//   写入：构建成功后把本次产物的依赖集合落进 page_dependencies（§8.2「按 Artifact
//         可重建的查询投影」）；
//   反查：依赖源变更时按 (kind,key) 找出真正受影响的页面并标记 stale（§8.2 失效查询），
//         可选自动重建（§8.3：默认只产生 staged Artifact，已发布的页面才回写线上）。
//
// 与既有 MarkStaleFor*（主题/块/i18n 的**全站或按主题**标记）的关系：
// 那是「来源自身无法精确表达影响面」时的保守标记，保持原样不退化；
// 本文件是「产物声明过依赖」时的精确路径，两者互补。

// 与 page_dependency.go 的分工：那个文件是写侧（落依赖行 + 按依赖键把命中的页面标记 stale），
// 本文件只读 —— 回答「谁声明过这条依赖」，全程没有一条 UPDATE，尤其不动 pages.stale。
//
// 为什么要把「只读反查」做成独立入口而不是顺手调 MarkStaleByDependency：
//   · 语义相反：写路径返回「这次被标记的页面」，反查要的是「现在声明着这条依赖的页面」；
//     在只读调用点上复用写路径，等于看一眼影响面就把全站相关页面标成待重建；
//   · 契约层因此也分成两个形状（pagecontract.PageDependencyLookup 与 PageService 上的
//     MarkStaleByDependency）。
//
// 本文件只做三件事：入参归一化、调 model、把行映射成契约投影并**按 id 去重排序**。
// 去重不是装饰：同一页面的活跃与暂存产物可能都声明了这条依赖（两个 artifact_id 各一行），
// 不去重时同一个页面会在影响面清单里出现两次 —— 而清单长度正是运营判断「影响几处」的依据。

// 背景：本模块一批「整站标记／精确标记」入口的形状都是「枚举工程 → 逐工程独立作用域执行
// → 合并命中集合」（为什么要逐工程见 page_scope.go）。合并这一段此前在每个入口里各写一遍
// 内联循环；本次给整站标记（主题 / 块 / 词条）接影响面回执时若不收口，它就会变成第四、
// 第五份 —— 去重抄漏一处，日志里的「本次影响 N 个页面」就会比实际多。
//
// 本文件只做聚合：命中集合由 model 的 RETURNING id 给出，聚合结果交给

// 与 model.ListStale 的分工：model 管「一个工程内按参数取一段」，
// 本文件管「跨工程合并 + 全局排序 + 截断 + 计数」。跨工程只能逐工程各设一次
// 作用域（pages 在迁移 215 里带 FORCE 策略，作用域是单值会话变量，合并多工程
// 到一个查询只能靠放宽谓词，那等于取消隔离 —— 见 page_scope.go 的论证）。
//
// 两个消费者：
//   1. 后台只读展示（/admin/pages 的待重建区块）：ListStalePages —— 全站清单，
//      按标记时间倒序，「最近这次改动影响的」排在最前面；
//   2. 写侧的回执与结构化日志：StaleImpactOfIDs —— 把扇出返回的 id 集合
//      （跨工程）翻译成「N 个页面 + 最多 K 条标题 / 路径」的人类可读摘要。
//
// 一致性：两个消费者共用同一份取数口径（同一组 model 方法、同一份可读标识规则、
// 同一组 limit 归一化常量），不在各自的调用点上另抄一遍。

//（mediacontract.StaleMarker，见 media/contract/media_service.go 的「索要的端口」）。
//
// 为什么由 page 实现：引用集（谁引用了这张图）在 media 的表里，而「页面怎么算待重建」
// 在 pages 表里 —— 跨模块表访问是禁止的（AGENTS.md §表隔离），所以 media 只声明端口。
//
// 为什么放在 service 同包而不是 outbound/media/：实现用的是本模块自己的 model，
// 签名直接对得上、没有任何形状翻译（CLAUDE.md：这类「顺手满足对方端口」留在 service
// 同包 + 编译期断言）。多包一层反而要把 model 暴露出去。
//
// 与本模块其它标记入口的关系：MarkStaleForTheme / MarkStaleByDependency 按**依赖键**
// 命中，本入口按**显式 id 集合**命中 —— 后者与 MarkStaleByRegistryVersion 同形
// （调用方已经算出精确集合，不做任何全站标记）。

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/module/media/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/model"
	"go_wp/internal/module/plugin/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
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
	if err := s.requireArtifactPage(ctx, pageID, artifactID); err != nil {
		logger.Scene("dependency").With("page_id", pageID).With("artifact_id", artifactID).
			Error(err, "依赖记录的产物归属校验失败（已降级，不影响构建结果）")
		return
	}
	if err := s.model.ReplaceDependencies(ctx, projectID, pageID, artifactID, rows); err != nil {
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
	if err := s.requireArtifactPage(ctx, pageID, artifactID); err != nil {
		return err
	}
	return s.model.ReplaceDependenciesTx(ctx, tx, projectID, pageID, artifactID, rows)
}

// requireArtifactPage 校验「产物行确实挂在这张页面上」。
//
// 这一步原来在 model 的 requireArtifactOwned 里，与「页面属于本工程」合成一条 SQL，
// 于是同时读了 page_artifacts（artifact 模块的表）与 pages。现在两半分开：产物行 → 页面
// 问 artifact 契约，页面 → 工程留在 model —— 每个模块只读自己的表。
//
// 两次读之间产物行理论上可能被删，但依赖表本来就只记 artifact_id，那只让该行指向一个
// 不存在的产物（GC 与构建期都会发现），不会造成越权写；反过来把它并进调用方事务则需要
// 契约接口接收外部 tx，而那会让「谁的表谁负责」重新糊掉。
func (s *Service) requireArtifactPage(ctx context.Context, pageID, artifactID string) error {
	if s.pageArtifacts == nil {
		return errors.New("page: artifact 契约未注入，无法校验产物归属")
	}
	got, err := s.pageArtifacts.PageArtifactPageID(ctx, artifactID)
	if err != nil {
		return err
	}
	if got == "" || got != pageID {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// pageArtifactHashes 经契约取「认领中的产物 hash」清单（契约未注入时返回错误而非 panic）。
func (s *Service) pageArtifactHashes(ctx context.Context) ([]string, error) {
	if s.pageArtifacts == nil {
		return nil, errors.New("page: artifact 契约未注入，孤儿对账退化为只按本模块清单判定")
	}
	return s.pageArtifacts.ListPageArtifactHashes(ctx)
}

// pageArtifactHashByID 经契约按产物行 id 取 hash（不存在时返回空串）。
func (s *Service) pageArtifactHashByID(ctx context.Context, id string) (string, error) {
	if s.pageArtifacts == nil {
		return "", errors.New("page: artifact 契约未注入")
	}
	return s.pageArtifacts.PageArtifactHashByID(ctx, id)
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

// FindPagesByDependency 实现 pagecontract.PageDependencyLookup：按依赖键只读反查页面。
//
// 命中口径（与写路径 MarkStaleByDependency 逐字一致）由 model 负责：该页面的活跃或暂存
// 产物在 page_dependencies 里声明了这条依赖。本层不重新解释它 —— 两个口径分叉的表现是
// 「按依赖标记的页面」与「按依赖反查的页面」不是同一批，而删除保护正是按后者放行 / 拦截。
//
// 失败即失败（不降级成空集合）：反查读不到与「没有引用」是两件事 —— 降级会让删除保护
// 把一次读取失败当成「没人引用」然后放行删模板。
func (s *Service) FindPagesByDependency(ctx context.Context, projectID, dependencyKind, dependencyKey string) (
	refs []pagecontract.DependencyPageRef, err error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	pid, kind, key, err := normalizeDependencyLookup(projectID, dependencyKind, dependencyKey)
	if err != nil {
		return nil, err
	}
	if kind == "" || key == "" {
		// 空依赖键不是错误，但也不去查：拿空键查等于把「依赖键缺失」变成
		// 「全站页面都引用了它」（model 侧同样早退）。
		return nil, nil
	}
	rows, lerr := s.model.ListPagesByDependency(ctx, pid, kind, key)
	if lerr != nil {
		return nil, lerr
	}
	return dependencyRefsOf(rows), nil
}

// normalizeDependencyLookup 归一化反查入参（纯函数，便于单测）。
//
// projectID 必填：pages 带 FORCE 策略，**空工程 id 一律拒绝而不是退化成「不限工程」**——
// 后者会把别的工程的页面混进这次删除保护的影响面里，表现为「拦了一个不相干的页面」，
// 而拦截型错误的代价是运营去解绑一个根本无关的页面。
//
// kind / key 归一化后可能为空：这是**合法**的（返回空集合），由调用方决定是早退还是查询。
func normalizeDependencyLookup(projectID, dependencyKind, dependencyKey string) (pid, kind, key string, err error) {
	pid = strings.TrimSpace(projectID)
	if pid == "" {
		return "", "", "", ErrProjectRequired
	}
	// 依赖键原样 TrimSpace：page_dependencies 的写入侧（dependencyRows）也是 TrimSpace 后落库，
	// 两侧不一致时带空白的键永远查不到任何页面（表现为「明明有引用，反查却是空的」）。
	return pid, strings.TrimSpace(dependencyKind), strings.TrimSpace(dependencyKey), nil
}

// dependencyRefsOf 行 → 契约投影：按页面 id 去重、跳过空白 id、按 id 排序（纯函数，便于单测）。
//
// 去重的合并规则：同一页面出现多行时取**首次出现的标题**（同一页面的标题来自同一条
// draft_document，各行必然相同）。
//
// 排序是必需的：SQL 不排序（次序口径只留一份，见 model 的注释），调用方（影响面清单 /
// 日志与错误明细）拿到的是稳定次序 —— 不排的话同一个工程两次渲染的行序可能不同。
func dependencyRefsOf(rows []pagemodel.DependencyRefRow) []pagecontract.DependencyPageRef {
	if len(rows) == 0 {
		return nil
	}
	out := make([]pagecontract.DependencyPageRef, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		id := strings.TrimSpace(rows[i].ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, pagecontract.DependencyPageRef{
			ID: id, Title: strings.TrimSpace(rows[i].Title),
		})
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// staleIDCollector 累积逐工程扇出命中的页面 id：去重、保持首次出现次序、跳过空白 id。
//
// 为什么去重是防御性的而不是必需的：pages.id 是主键，每个工程的 UPDATE 都带 project_id
// 条件，同一个 id 不可能被两个工程同时命中。留着它是因为「只有一个实现」才是真正要守的
// 东西 —— 它同时是「本次影响多少个页面」这个数字的唯一来源（写侧聚合与日志归一化都走它）。
//
// 零值可用（seen 懒初始化），便于在循环外直接声明。
type staleIDCollector struct {
	seen map[string]bool
	ids  []string
}

// add 合并一次扇出的命中集合（空集合是常态，直接返回）。
//
// id 按 TrimSpace 后的值收录，与 StaleImpactOfIDs 的归一化口径一致：两处口径不同的表现是
// 「日志里的总数」与「摘要里的总数」对不上，而这两个数都只在日志里看得见。
func (c *staleIDCollector) add(hit []string) {
	for _, id := range hit {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if c.seen == nil {
			c.seen = make(map[string]bool, len(hit))
		}
		if c.seen[id] {
			continue
		}
		c.seen[id] = true
		c.ids = append(c.ids, id)
	}
}

// list 返回累积的 id 集合。一个都没有时返回 nil —— 调用方按 len 判断即可：
// 这里刻意不返回「空但非 nil」的切片去逼调用方区分两者，那区分没有语义。
func (c *staleIDCollector) list() []string { return c.ids }

const (
	// staleImpactSampleLimit 写侧日志 / 回执里最多列出的页面数（前 K 个）。
	staleImpactSampleLimit = 5
	// staleImpactIDChunkSize 影响面反查时单条 IN 查询最多带多少个 id。
	//
	// 整站标记（主题 / 块 / 词条）一次就能返回成千上万个 id：500 远低于 PostgreSQL 的
	// 参数上限 65535，同时把单条 SQL 的 IN 元素规模压在一个 planner 处理起来很便宜的
	// 量级（分块的正确性与取舍见 StaleImpactOfIDs 的注释）。
	staleImpactIDChunkSize = 500
)

// ListStalePages 只读反查：列出待重建页面（ProjectID 为空 = 全部工程）。
//
// 失败即失败（不降级成空清单）：「影响面 0」与「读不到影响面」是两件事，
// 混在一起会让运营把一次读取失败当成「没有待重建页面」然后放心地不看。
// 调用方（页面 handler）自己决定怎么显示这个失败 —— 它有不降级的权限，
// 只读面没有替它撒谎的权限。
func (s *Service) ListStalePages(ctx context.Context, req *pagedto.StalePageListReq) (res *pagedto.StalePageListResp, err error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	if req == nil {
		req = &pagedto.StalePageListReq{}
	}
	orderBy, oerr := pagemodel.NormalizeStaleOrder(req.OrderBy)
	if oerr != nil {
		return nil, ErrInvalidParam
	}
	limit := pagemodel.NormalizeStaleListLimit(req.Limit)

	projectIDs, names, err := s.staleProjectScope(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	parts := make([][]pagemodel.StalePageRow, 0, len(projectIDs))
	total := 0
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// 每工程取满 limit 条：全局前 limit 条一定落在「各工程前 limit 条」的并集里，
		// 所以逐工程截断后再全局排序不会丢行 —— 而一次读全站会把整张表拉进内存。
		rows, lerr := s.model.ListStale(ctx, projectID, limit, orderBy, req.Descending)
		if lerr != nil {
			return nil, lerr
		}
		n, cerr := s.model.CountStale(ctx, projectID)
		if cerr != nil {
			return nil, cerr
		}
		total += int(n)
		parts = append(parts, rows)
	}

	merged := mergeStaleRows(parts, limit, orderBy, req.Descending)
	pages := make([]pagedto.StalePageResp, 0, len(merged))
	for i := range merged {
		pages = append(pages, staleRowToDTO(merged[i], names))
	}
	return &pagedto.StalePageListResp{
		Pages:     pages,
		Total:     total,
		Limit:     limit,
		Truncated: total > len(pages),
	}, nil
}

// StaleImpactOfIDs 把**一次写操作**的受影响页面 id 集合翻译成影响面摘要
// （总数 + 最多 staleImpactSampleLimit 条标题 / 路径），供写侧回执与结构化日志共用。
//
// 为什么放在 service 而不是让每个写侧自己查：标题的取数口径（文档 SEO 段）、
// 可读标识的回落规则、跨工程的作用域拆解都只有一份，写侧复用同一份实现；
// 每个写侧各写一遍的结果，是这个页面在不同日志行里显示成三个名字。
//
// 返回 nil 表示「摘要不可用」（工程清单读不到 / 查询失败 / 空集合）——
// **不是错误**：这是观测，不是业务结果。内容已经写成功了，一次只读查询失败
// 绝不能反向影响写入结论；调用方按「没有摘要」继续即可。
func (s *Service) StaleImpactOfIDs(ctx context.Context, ids []string) *pagedto.StaleImpactSummary {
	if s == nil || s.model == nil || len(ids) == 0 {
		return nil
	}
	projectIDs, names, err := s.staleProjectScope(ctx, "")
	if err != nil {
		return nil
	}
	limit := staleImpactSampleLimit
	parts := make([][]pagemodel.StalePageRow, 0, len(projectIDs))
	seen := map[string]bool{}
	uniq := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return nil
	}
	// 分块 IN 查询（整站标记接影响面回执时新增的规模处理）：
	//
	// 整站标记一次就能返回成千上万个 id，而这里是一条 `WHERE id IN (…)` —— PG 的参数
	// 上限是 65535，且单个 IN 的元素越多，planner 为它构造的表达式与内存占用增长越快。
	// 每块最多 staleImpactIDChunkSize 个，块内取前 limit 条，最后与其它块一起全局归并。
	//
	// 为什么选「分块」而不是「超过阈值就降级成只记条数 + 按标记时间取样本」：
	//   · 分块后的样本仍然**精确属于本次 id 集合**；降级法给出的样本是按时间近似的，
	//     读者无法区分「这次改动影响的页」与「同一时刻被别的改动标记的页」——
	//     而整站标记本身正是「一次改动标记全站」，样本里混进别处的概率不低；
	//   · 降级法要新增一条「按标记时间取样本」的取数口径，那等于给「样本从哪来」
	//     留下两个答案（本包刻意只保留一份：id 集合反查 + 同一份排序 / 截断）。
	// 代价：查询条数从 1 条变成 ceil(N/500) 条（每块都是主键索引 + LIMIT K）。整站标记
	// 本身就要逐工程 UPDATE 一次全站，这点只读开销属于同一量级，且只发生在罕见的
	// 文案 / 主题变更路径上；失败语义不变（任一块读失败即整体放弃，见下）。
	//
	// 归并的正确性：全局前 K 条一定落在「各块前 K 条」的并集里（标准 top-K 归并性质），
	// 所以分块不会让样本变样。
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return nil
		}
		for _, chunk := range chunkIDs(uniq, staleImpactIDChunkSize) {
			rows, lerr := s.model.ListBriefsByIDs(ctx, projectID, chunk, limit, pagemodel.StaleOrderUpdateTime, true)
			if lerr != nil {
				// 单个工程（或其中一块）读失败即整体放弃：给出一份少了某个工程、
				// 或某一块的摘要，比不给摘要更容易误导（读者会以为那就是全部）。
				return nil
			}
			parts = append(parts, rows)
		}
	}
	merged := mergeStaleRows(parts, limit, pagemodel.StaleOrderUpdateTime, true)
	pages := make([]pagedto.StalePageResp, 0, len(merged))
	for i := range merged {
		pages = append(pages, staleRowToDTO(merged[i], names))
	}
	return &pagedto.StaleImpactSummary{
		Total:     len(uniq),
		Pages:     pages,
		Limit:     limit,
		Truncated: len(uniq) > len(pages),
	}
}

// chunkIDs 把 id 集合切成每块最多 size 个（纯函数，便于单测）。
//
// 分块本身不会报错：切错了只会让样本少几行 —— 而「影响面样本少了几行」正是这一批
// 要消灭的那种静默偏差，所以它单独被测。
//
// 保持原顺序（块内 = 输入次序，块 = 输入次序）：样本的最终次序由 mergeStaleRows 按
// 标记时间重排，与分块次序无关；保序只是为了「同一个 id 集合每次都得到同一组 SQL」。
//
// size <= 0（调用方给了不合法的值）时退回单块，**不静默丢 id**：丢 id 在本函数的语义里
// 等于「这些页面不存在」，那是最不该出现的失败形态。
func chunkIDs(ids []string, size int) [][]string {
	if len(ids) == 0 {
		return nil
	}
	if size <= 0 || len(ids) <= size {
		return [][]string{ids}
	}
	out := make([][]string, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		out = append(out, ids[start:end])
	}
	return out
}

// staleImpactLogPlan 一次影响面日志的「写什么」（纯数据，便于单测）。
type staleImpactLogPlan struct {
	// Total 本次受影响的页面数（去重后的 id 条数）。
	Total int
	// NoSample 摘要不可用：这一次只能记条数。
	NoSample bool
	// Sample 人类可读样本（标题优先、回落路径）；NoSample 时为空。
	Sample string
	// SampleShown 样本条数。
	SampleShown int
	// Truncated 样本被截断（总数大于样本条数）。
	Truncated bool
	// Message 日志正文（唯一一份文案）。
	Message string
}

// planStaleImpactLog 决定这次记什么（纯函数，便于单测）。
//
// 总数以调用方给的 affected 为准，而不是 impact.Total：affected 是**已经发生的事实**
// （这一次扇出真正返回了多少个不同页面），而 summary 是一次只读反查的产物 ——
// 反查少了几行不该把日志里的总数也改小，那会让「日志说 3、实际标了 8」这种偏差
// 永远查不出来。两者的口径由调用方保证一致（同一个归一化后的 id 集合）。
func planStaleImpactLog(affected int, impact *pagedto.StaleImpactSummary) staleImpactLogPlan {
	plan := staleImpactLogPlan{Total: affected}
	if impact == nil {
		plan.NoSample = true
		plan.Message = "依赖失效：本次影响 " + strconv.Itoa(affected) + " 个页面（影响面摘要不可用，仅记条数）"
		return plan
	}
	plan.Sample = formatStaleImpactSample(impact.Pages)
	plan.SampleShown = len(impact.Pages)
	plan.Truncated = impact.Truncated
	plan.Message = "依赖失效：本次影响 " + strconv.Itoa(affected) +
		" 个页面（前 " + strconv.Itoa(plan.SampleShown) + " 个：标题 / 路径）"
	return plan
}

// logStaleImpact 记一条「本次影响 N 个页面（最多列前 K 个：标题 / 路径）」的结构化日志。
//
// 通道复用现有的 logger（scene=dependency），不新造回执通道；文案只有这一份
// （planStaleImpactLog）。摘要取不到时降级为「只记条数」；写侧主流程（返回的 ids / error）
// 完全不受影响 —— 这是观测，不是业务结果。
//
// 边界（写清是为了让后来者知道这条日志能当什么证据、不能当什么）：
//   - 空集合（或全是空白 id）**不记**：那不是「影响面为 0 个页面」，是「这次什么都没被
//     标记」；每次整站标记都留一行「影响 0 个页面」只会把 dependency 场景淹掉；
//   - 调用方因 ctx 取消提前中断扇出时，这里的 affected 是**已经标记掉的那部分**，
//     不是「本应标记的全部」—— 日志只陈述已发生的事实，不替调用方承诺完整范围；
//   - 样本是按**本次 id 集合**反查出来的（不是按「谁刚变成 stale」猜的），所以它不会
//     混进别的改动标记的页面；整站规模下的取数代价与取舍见 StaleImpactOfIDs 的注释。
func (s *Service) logStaleImpact(ctx context.Context, reason string, ids []string) {
	if s == nil {
		return
	}
	// 归一化复用逐工程聚合那一份实现（staleIDCollector）：口径分叉的表现是
	// 「日志里的总数」与「摘要里的总数」对不上，而两处都只在日志里看得见。
	c := &staleIDCollector{}
	c.add(ids)
	uniq := c.list()
	if len(uniq) == 0 {
		return
	}
	plan := planStaleImpactLog(len(uniq), s.StaleImpactOfIDs(ctx, uniq))
	entry := logger.Scene("dependency").
		With("reason", reason).
		With("affected", plan.Total).
		With("sample_limit", staleImpactSampleLimit)
	if plan.NoSample {
		// 摘要不可用：至少留下条数与场景，不让「谁受影响了」整体消失。
		entry.Info(plan.Message)
		return
	}
	entry.With("truncated", plan.Truncated).With("sample", plan.Sample).Info(plan.Message)
}

// formatStaleImpactSample 把影响面样本压成一行（标题优先、回落路径）。
//
// 纯函数便于单测。标题缺失时**只给路径**，不编「（无标题）」这类占位 ——
// 日志里那句话不是给人看的界面文案，多一个常量不会让任何东西更清楚。
func formatStaleImpactSample(pages []pagedto.StalePageResp) string {
	parts := make([]string, 0, len(pages))
	for i := range pages {
		label := strings.TrimSpace(pages[i].Title)
		path := strings.TrimSpace(pages[i].Path)
		switch {
		case label == "":
			label = path
		case path != "" && path != label:
			label = label + " (" + path + ")"
		}
		if label != "" {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, "; ")
}

// staleProjectScope 返回本次反查要覆盖的工程清单与工程名映射。
//
// projectID 非空 = 只查该工程（调用方自带作用域，不再按工程表扇出）；
// 空 = 全部工程（逐工程各设一次作用域，见 page_scope.go）。
func (s *Service) staleProjectScope(ctx context.Context, projectID string) (ids []string, names map[string]string, err error) {
	if pid := strings.TrimSpace(projectID); pid != "" {
		return []string{pid}, s.staleProjectNames(ctx), nil
	}
	ids, err = s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, nil, err
	}
	return ids, s.staleProjectNames(ctx), nil
}

// staleProjectNames 工程 id → 名称。
//
// 读不到时返回空 map（不是错误）：工程名只是展示加成，缺了它清单仍然可用
// （路径本身就是页面唯一稳定的标识）；但反过来，让一次只读反查因为
// 「拿不到工程名」而整体失败，是把装饰当成了数据。
func (s *Service) staleProjectNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s == nil || s.project == nil {
		return out
	}
	list, err := s.project.List(ctx)
	if err != nil {
		logger.Scene("page").Error(err, "读取工程列表失败，待重建清单不显示工程名")
		return out
	}
	for i := range list {
		out[list[i].ID] = list[i].Name
	}
	return out
}

// mergeStaleRows 把各工程已排序的行合并为全局有序的前 limit 条（纯函数，便于单测）。
//
// 各工程片段内部已有序，但全局次序需要重排：A 工程第 3 条可能比 B 工程第 1 条更近。
// 比较口径与 model 的 ORDER BY 相同（staleRowLess 的注释里写了为什么必须一致）。
func mergeStaleRows(parts [][]pagemodel.StalePageRow, limit int, orderBy string, descending bool) []pagemodel.StalePageRow {
	total := 0
	for i := range parts {
		total += len(parts[i])
	}
	all := make([]pagemodel.StalePageRow, 0, total)
	for i := range parts {
		all = append(all, parts[i]...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return staleRowLess(all[i], all[j], orderBy, descending)
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

// staleRowLess 行的全局排序比较：与 model 的 ORDER BY 同一口径（列 + 方向 + id 兜底）。
//
// 为什么必须与 SQL 一致：逐工程查询按 SQL 排序取前 limit 条，全局重排按这个函数 ——
// 两者不一致时「每工程的前 limit 条」与「全局前 limit 条」对不上，清单会漏行，
// 而且漏得没有规律（取决于哪个工程先被遍历）。已知的一处偏差：PG 的默认 collation
// 与 Go 的字节序在非 ASCII 文本上可能给出不同次序，页面路径以 ASCII 为主，影响限于
// 同长度前缀的边界情形 —— 记录在此，不假装没有。
func staleRowLess(a, b pagemodel.StalePageRow, orderBy string, descending bool) bool {
	var less, equal bool
	switch orderBy {
	case pagemodel.StaleOrderDraftPath:
		less, equal = a.DraftPath < b.DraftPath, a.DraftPath == b.DraftPath
	case pagemodel.StaleOrderTitle:
		less, equal = a.Title < b.Title, a.Title == b.Title
	case pagemodel.StaleOrderID:
		less, equal = a.ID < b.ID, a.ID == b.ID
	default: // pagemodel.StaleOrderUpdateTime
		less, equal = a.UpdatedAt.Before(b.UpdatedAt), a.UpdatedAt.Equal(b.UpdatedAt)
	}
	if equal {
		// 与 SQL 的 ", id ASC" 对齐：同值行的次序由主键兜底，避免两次读取次序不同。
		return a.ID < b.ID
	}
	if descending {
		return !less
	}
	return less
}

// staleRowToDTO 行 → 只读投影（填入工程名与可读标识）。
func staleRowToDTO(row pagemodel.StalePageRow, names map[string]string) pagedto.StalePageResp {
	return pagedto.StalePageResp{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		ProjectName: names[strings.TrimSpace(row.ProjectID)],
		Title:       strings.TrimSpace(row.Title),
		Path:        staleDisplayPath(row),
		Published:   stalePublished(row),
		Stale:       row.Stale,
		UpdatedAt:   utils.NewJSONTime(row.UpdatedAt),
		// 失败痕迹按原样带出，不做「多久算旧」的判断：判据属于展示层
		// （时间旧不旧要看的人自己权衡），读侧只如实给出事实。
		RebuildFailedAt:    failedAtDTO(row.RebuildFailedAt),
		RebuildFailedStage: strings.TrimSpace(derefString(row.RebuildFailedStage)),
	}
}

// failedAtDTO 只把**有值**的失败时刻转成 DTO（nil 进 nil 出，避免 JSON 里出现零值时间）。
func failedAtDTO(at *time.Time) *utils.JSONTime {
	if at == nil || at.IsZero() {
		return nil
	}
	v := utils.NewJSONTime(*at)
	return &v
}

// derefString 取字符串指针的值（nil 与空串同义：这一列要么有值、要么是 NULL）。
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// staleDisplayPath 页面的可读标识：已上线路径优先，其次草稿路径，都没有时退回 id。
//
// 与 content 的 stalePagePath / block 的 blockStalePagePath 是同一口径（三处显示同一
// 个页面时必须同名）；那两处在各自的 handler 里、不在本次改动范围内，因此这里是
// page 侧的那一份 —— 三份口径的一致性由注释与 hand-off 记录，不静默分叉。
//
// 不编造路径：一个不存在的 URL 比一串 id 更误导人。
func staleDisplayPath(row pagemodel.StalePageRow) string {
	if row.ActivePath != nil && strings.TrimSpace(*row.ActivePath) != "" {
		return strings.TrimSpace(*row.ActivePath)
	}
	if p := strings.TrimSpace(row.DraftPath); p != "" {
		return p
	}
	return row.ID
}

// stalePublished 是否已上线（有活跃路径）。
func stalePublished(row pagemodel.StalePageRow) bool {
	return row.ActivePath != nil && strings.TrimSpace(*row.ActivePath) != ""
}

var _ mediacontract.StaleMarker = (*Service)(nil)

// RefKinds 本实现认领的引用方类型：媒体引用集里 kind = page 的那些。
func (s *Service) RefKinds() []string { return []string{mediacontract.RefKindPage} }

// MarkStaleByMediaRefs 把「产物里引用了这张图」的页面标记为待重建。
//
// 语义要点：
//   - 只处理 kind = page 的项（media 已按 RefKinds() 过滤，这里再判一次是为了
//     实现自身可独立成立 —— 端口语义不该依赖调用方守规矩）；
//   - 逐工程独立作用域执行（DB-009）：pages 带 FORCE 策略，未设 app.project_id 的
//     UPDATE 在非超级角色下静默 0 行 —— 表现是「换图后页面不更新」且日志无异常，
//     与端口未接入完全同形。非本工程的 id 由 project_id 条件 + 策略双重拦下；
//   - 返回 RETURNING id 回读集合（真正命中的页面），不是入参回显：入参里可能混着
//     已删除 / 不存在的 id，回显会让换图日志的影响面虚高；
//   - 失败即失败：任一个工程的事务失败就整体返回错误，不吞（media 侧据此把
//     「换图成功但引用方不会更新」透出给操作者）。
func (s *Service) MarkStaleByMediaRefs(ctx context.Context, refs []mediacontract.MediaRef) (marked []string, err error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	ids := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for i := range refs {
		if strings.TrimSpace(refs[i].Kind) != mediacontract.RefKindPage {
			continue
		}
		id := strings.TrimSpace(refs[i].ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now()
	collector := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return collector.list(), ctx.Err()
		}
		hit, merr := s.model.MarkStaleByIDs(ctx, projectID, ids, at)
		if merr != nil {
			return nil, merr
		}
		collector.add(hit)
	}
	// 影响面留痕走本模块既有那份实现（page_stale_overview.go）：理由与其它标记入口
	// 一致 —— 同一个 id 集合在任何标注里都该显示同一个名字。
	s.logStaleImpact(ctx, "media_replace", collector.list())
	return collector.list(), nil
}
