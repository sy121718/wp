package pageservice

// 装配编译（方案 C，021_blocks.sql）：内核 CompileFn 注入。
// 页面文档 settings.structure 快照了主题的页眉/页脚块绑定，
// 构建时在此拉取块文档分别编译，HTML/CSS 拼接进页面产物——
// 访问面保持纯静态（无运行时拼接），块内容变更通过 stale 传播触发重建。
// 页面文档内的 core.globalref 节点经 BlockResolver 同样内联展开。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// errCompileFailed 标记编译阶段失败。compileDocument 以 %w 包裹，
// 调用方经 errors.Is 区分「编译失败」与「组件模板加载/文档渲染失败」——
// 预览需要据此分类 422（编译失败）与 500（其余内部错误），构建路径仅关心 err != nil。
var errCompileFailed = errors.New("页面编译失败")

// assembleCompile 装配感知编译：页眉块 + 页面主体 + 页脚块。
// 内容引用面只存 URL 快照，构建期零解析（不查媒体库）。
// 无绑定无引用时输出与默认编译字节一致（hash 兼容历史产物）；
// 块文档缺失/非法降级为空片段，不阻塞构建主链。
// 解析失败回退默认编译；解析成功则与预览共用 compileDocument 装配管线。
func (s *Service) assembleCompile(ctx context.Context, in pipeline.BuildInput) ([]byte, error) {
	page, err := builder.ParsePage(in.DocJSON)
	if err != nil {
		logger.Scene("build").With("err", err).Warn("页面文档解析失败，回退默认编译")
		return pipeline.DefaultCompile(ctx, in)
	}
	projectID, currentPath := s.pageContextOf(ctx, in.PageID, in.Lang)
	// 语言来自构建输入（内核按 PageRecord.Lang 注入，见 pipeline.BuildInput）；
	// 访问路径仍取页面记录的逻辑路径，前缀在 compileDocument 内单点计算。
	// 依赖线索记录器（审计 VIS-006）：构建路径传入，编译期记录消费过的系统页面槽位。
	html, err := s.compileDocument(ctx, page, projectID, currentPath, in.Lang, in.Usage, true)
	if err != nil {
		if errors.Is(err, errCompileFailed) {
			logger.Scene("build").Error(err, "页面编译失败")
		}
		return nil, err
	}
	s.syncMediaRefs(ctx, in.PageID, currentPath, html)
	return html, nil
}

// syncMediaRefs 构建期写入媒体引用缓存（02-B 第 4 能力，docs/02-B §2 引用保护）。
//
// 时机：**构建期**，不是每次编辑——引用关系是产物事实（文档里写的 URL 未必都进产物，
// 条件渲染/块内联/CMS 集合展开后只有编译结果才权威），且构建期天然幂等
// （同一文档重复构建写入同一集合，差集为空零写入）。
//
// 失败一律降级：引用缓存是保护性元数据，不是构建输入，不阻断发布主链。
// 标题参数用页面逻辑路径（pages 表无标题列，标题在文档内），仅用于删除拦截提示。
func (s *Service) syncMediaRefs(ctx context.Context, pageID, pagePath string, html []byte) {
	if s.media == nil || strings.TrimSpace(pageID) == "" || len(html) == 0 {
		return
	}
	if _, err := s.media.SyncReferencesFromHTML(ctx, "page", pageID, pagePath, string(html)); err != nil {
		logger.Scene("build").With("page_id", pageID).Warn("媒体引用缓存同步失败（已降级，不阻断构建）")
	}
}

// compileDocument 装配编译已解析的页面文档为完整 HTML 字节：
// 组件模板 Set 选择（embed / CompositeSet）、BlockResolver/PluginResolver/
// CollectionResolver/ThemeSettings 注入、Compile、页眉/页脚块内联、RenderDocument。
// 解析由调用方负责（构建路径 ParsePage + 降级；预览路径 json.Unmarshal + 空文档检查）。
// 编译失败以 %w 包裹 errCompileFailed，其余失败原样返回。
// projectID 为本次编译的站点工程 ID（页面文档不携带，由调用方按页面记录注入）；
// 供导航等站点级资源解析使用，为空时绑定菜单位置的导航节点在编译期显式报错。
// currentPath 为页面逻辑访问路径，用于导航「当前项」高亮（空 = 不标记）；
// 多语言开启前缀时，此处统一转换为带前缀路径后再比对（与导航项 URL 同源）。
// lang 为本次构建语言（空 = 站点默认语言）：驱动组件文案取词（构建期冻结快照）
// 与导航项 URL 前缀，是「同一文档每个语言一份独立产物」的语言维度。
// withPublishScope 控制语言切换器是否按「访问面是否已发布」过滤：
// 构建与发布路径传 true（审计 I18N-021：不过滤会把用户送到 404）；
// **预览传 false** —— 预览是编辑期行为，作者在看「这份文档会长什么样」，
// 与「哪些语言已经发布过」无关。按发布面过滤会让刚加的语言在预览里凭空消失，
// 作者只会以为切换器坏了。
func (s *Service) compileDocument(ctx context.Context, page *builder.Page, projectID, currentPath, lang string, usage core.UsageRecorder, withPublishScope bool) ([]byte, error) {
	// 组件模板 Set + 插件装配（EDT-003 共用 pipeline.ComponentSetWithPlugins）。
	asm := pipeline.LoadPluginAssembly(ctx, s.plugins)
	set, pluginOpts, err := pipeline.ComponentSetWithPlugins(asm)
	if err != nil {
		return nil, err
	}
	resolver := newBlockResolverAdapter(s, ctx, projectID)
	// 构建语言与取词函数：WithLanguage 决定 RenderContext.Lang；
	// WithTranslator 注入「构建开始时刻冻结」的词条快照——构建中途刷新 i18n 缓存
	// 不影响本次产物字节（确定性构建不变量，docs/06-D §2.3/§12）。
	opts := []builder.CompileOption{
		builder.WithContext(ctx), builder.WithBlockResolver(resolver), builder.WithComponentSet(set),
	}
	opts = append(opts, pluginOpts...)
	opts = append(opts, pipeline.LocaleCompileOptions(lang)...)
	opts = append(opts, pipeline.ClientAssetOptions()...)
	if s.content != nil {
		opts = append(opts, builder.WithCollectionResolver(s.content))
	}
	// 商品数据源（issue #35）：商品专用组件直连受限接口取数据。
	if s.productDS != nil {
		opts = append(opts, builder.WithProductDataSource(s.productDS))
	}
	// 站点级装配（EDT-003）：导航 / 槽位 / 高亮 / hreflang / srcset —— 与 presentation 共用 pipeline.SiteCompileOptions。
	var mediaProbe func(context.Context, string) []int
	if s.media != nil {
		mediaProbe = s.media.ProbeImageVariants
	}
	siteOpts, serr := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
		Project: s.project, Navigation: s.navigation, SitePages: s, MediaProbe: mediaProbe,
		// 语言切换器的发布状态查询（审计 I18N-021）：已登记但未发布的语言不进切换器 ——
		// 那不是「暂时没有内容」，而是一个必然 404 的链接。
		//
		// 判定来源是**访问面本身**（active 目录的符号链接），与访客看到的完全一致；
		// 查数据库的路由表只会得出「已登记 = 可见」，那正是这条 finding 的成因。
		// 每页每语言一次 lstat，成本可忽略。
		RoutePublished: publishScope(withPublishScope, func(accessPath string) bool {
			state, ierr := s.publication.Inspect(accessPath)
			return ierr == nil && state != nil && state.Kind != "none"
		}),
	}, pipeline.SiteCompileParams{
		Ctx: ctx, ProjectID: projectID, Lang: lang, LogicalPath: currentPath,
		CurrentPath: pipeline.HighlightPath(ctx, s.project, projectID, lang, currentPath),
	})
	if serr != nil {
		return nil, fmt.Errorf("%w: %v", errCompileFailed, serr)
	}
	opts = append(opts, siteOpts...)
	// 主题快照注入：settings.theme（保存时合入的 ThemeSettings 快照）→ 编译进产物。
	if page.Settings.Theme != nil {
		opts = append(opts, builder.WithThemeSettings(page.Settings.Theme))
	}
	// 内容翻译（多语言 P5b，docs/06-D §7.7）：作者在编辑器里填写的文本（按钮文字/
	// 标题/alt/图注/富文本）按组件 Translatable 白名单替换。每页每语言**构造一次**
	// 取词器——先收集候选（本页 AST + 页眉/页脚块 + core.globalref 内联块，见
	// collectContentCandidates）→ ShouldTranslateContent 过滤 → **一次**批量 SQL 取回
	// 译文，组件渲染期零查库（§7.7「零查库」）。默认语言与单语言站点跳过（产物即原文）。
	var contentTranslator *i18n.ContentTranslator
	var contentCandidates int
	opts, contentTranslator, contentCandidates = pipeline.AppendContentTranslation(
		opts, ctx, s.project, projectID, lang, page, resolver.ResolveBlockRoot, s.newContentTranslator)
	opts = append(opts, pipeline.AnalyticsCompileOptions(ctx, s.project, projectID)...)
	// 结构槽位（审计 VIS-001）：页眉 / 页脚的绑定展开成 root 首尾的槽位节点，
	// 与页面主体走**同一次编译** —— 不再由装配层把块单独编译后拼字符串。
	//
	// 拼字符串的问题不在字节，而在「块不在 AST 里」：翻译候选、失效依赖、
	// workbench 画布、main 地标判定各要一份特判，漏一处就是「页眉改了但页面没重建」。
	// 展开之后，槽位节点的语义与作者手动插入的 core.globalref 完全一致。
	opts = append(opts, structureSlotOptions(page.Settings.Structure)...)
	// 依赖线索：只记录**真实消费**的槽位（预览路径传 nil，不记录）。
	if usage != nil {
		opts = append(opts, builder.WithUsageRecorder(usage))
	}
	compiled, err := builder.Compile(page, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCompileFailed, err)
	}
	// L3 构建期缺失告警（决策 F14 第三层）：统计本页未命中译文数并记日志，
	// **不阻断构建**（缺译文已在取词器内回退原文，产物照常产出）。
	// 位置在块内联之后：页眉/页脚与 globalref 内联块的缺失同样计入（取词器为同一实例）。
	if contentTranslator != nil {
		pipeline.LogContentTranslationMisses(lang, contentCandidates, contentTranslator.Misses())
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
}

// blockResolverAdapter 适配 block 契约为 builder 的 BlockResolver
// （core.globalref 构建期展开引用块内容）。
// 缓存为单次编译内块解析缓存：同一块被引用多次时只查一次库，且**候选收集与
// 渲染展开共用同一份缓存**（内容翻译的块内候选不会额外产生一次块查询）。
type blockResolverAdapter struct {
	s *Service
	// projectID 与 ctx 一起构成块查询的 scope：block.Detail 把工程归属当作必填，
	// 缺它只会拿到「参数缺失」，而这一层是降级不报错的（构建继续、产物少一截）。
	projectID string
	ctx       context.Context
	cache     map[string]*builder.Page
	errs      map[string]error
}

// newBlockResolverAdapter 构造单次编译的块解析适配器（缓存随编译实例存活）。
func newBlockResolverAdapter(s *Service, ctx context.Context, projectID string) *blockResolverAdapter {
	return &blockResolverAdapter{
		s: s, ctx: ctx, projectID: projectID,
		cache: map[string]*builder.Page{}, errs: map[string]error{},
	}
}

// ResolveBlockRoot 按块 ID 返回块文档 root 节点。
// 防御（docs/02-D §5/§9）：reuse_mode=template 的块是「一次性复制」语义，
// 不允许经 core.globalref 引用展开——正常流程下副本已在插入时并入页面文档，
// 此处命中说明引用被绕过编辑器写入，构建期即报错暴露而非静默按引用渲染。
func (a *blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	page, err := a.blockPage(blockID)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

// blockPage 解析块文档为 builder.Page（带缓存；失败结果同样缓存，避免重复查库）。
func (a *blockResolverAdapter) blockPage(blockID string) (*builder.Page, error) {
	if a.cache != nil {
		if page, ok := a.cache[blockID]; ok {
			return page, nil
		}
	}
	if a.errs != nil {
		if err, ok := a.errs[blockID]; ok {
			return nil, err
		}
	}
	fail := func(err error) (*builder.Page, error) {
		if a.errs != nil {
			a.errs[blockID] = err
		}
		return nil, err
	}
	if a.s == nil || a.s.blocks == nil {
		return fail(fmt.Errorf("全局块 %s 不可用", blockID))
	}
	block, err := a.s.blocks.Detail(a.ctx, &blockcontract.DetailReq{ProjectID: a.projectID, ID: blockID})
	if err != nil || block == nil {
		return fail(fmt.Errorf("全局块 %s 不可用", blockID))
	}
	if block.ReuseMode == "template" {
		return fail(fmt.Errorf("全局块 %s 为一次性复制片段，不能被引用展开", blockID))
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return fail(err)
	}
	if a.cache != nil {
		a.cache[blockID] = page
	}
	return page, nil
}

// publishScope 按开关返回发布状态查询：false 时返回 nil，
// 语言切换器就退化成「全部语言都列出」（I18N-021 接入前的行为），这正是预览要的。
func publishScope(enabled bool, fn func(string) bool) func(string) bool {
	if !enabled {
		return nil
	}
	return fn
}
