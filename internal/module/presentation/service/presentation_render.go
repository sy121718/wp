package presentationservice

// 缺口：手工 Page 的构建在 builder.Compile 内注入 canonical / OG / Twitter / JSON-LD，
// 而自动发布实例此前只把模板 AST 编译成字节 —— 实体上的 seoTitle / seoDescription
// 没有任何构建期消费者，商品 / 文章详情页产物里既没有 canonical 也没有结构化数据，
// og:type 也永远只能是默认的 website。
//
// 这里在**唯一注入点**（renderHTML，发布与预览共用同一份渲染）把实体字段与实例线上
// 路径喂进 page.Settings.SEO，随后由 builder 既有的 BuildSEOHead 统一产出（不自己拼
// meta：canonical / OG / Twitter / JSON-LD 的转义、`og:type` 跟随 schemaType、面包屑
// 等规则只有一份）。
//
// 三条取舍：
//
//  1. **优先级：实体字段（非空）> 模板 settings.seo 已有值**。实体字段是「这一篇 /
//     这一个商品」的 SEO 事实，模板是这一**类**页面的默认值 —— 前者更具体。
//     但实体字段为空时**一律不写**：模板里人工填好的标题 / 描述不会因为实体没填
//     而被清空（与 internal/seo 的 ScoreArticle 取法同向：seoTitle 缺了才回落 title）。
//
//  2. **canonical 是唯一的例外：实例的线上路径（urlPath）覆盖模板值**。同一套模板
//     被多个实体复用，模板里手填的 canonical 必然只对其中一个正确；而实例绑定的
//     URL 是系统权威事实（与产物 Manifest.CanonicalPath 同源）。预览不激活 URL
//     （urlPath 传空）→ 保持模板原值不动，通常就是没有 canonical。
//
//  3. **确定性**：只取实体字段与实例路径，不引入时间戳等非确定值 ——
//     同一输入（模板 + 实体 + URL）产生相同字节（项目不变量）。
//
// 注意（产物字节变化）：这让**所有重新构建**的详情页产物多出 canonical / JSON-LD，
// 已发布但内容未变的实例不会自动重建 —— 要重新发布（或经依赖失效触发重建）
// 才会带上 SEO 头。

// 发布顺序（审计 AR2-003 收口）：
//
//	构建（无访问面副作用）→ 登记账本产物（一个事务：快照 + 各语言产物行 + 依赖）
//	→ 逐语言「登记 pending 回执 → 激活访问面 → 登记路由 → 写语言账本 → 结案」
//	→ 全部语言成功后推进实例指针并清 stale。
//
// 旧实现是「先在一个事务里提交全部语言的 publication 行与实例指针，事务提交之后
// 才逐个激活文件」：任一语言激活失败，数据库已经显示所有语言都已发布，而线上只有
// 前面几种语言的文件 —— 后台、sitemap 与语言切换器都会报出不可访问的 URL。
// 现在语言账本行（presentation_publications）是「该语言 URL 确实可访问」的唯一声明，
// 只在激活与路由登记都成功之后才写；指针只在整批成功后才前进。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/presentation/enums"
	"go_wp/internal/module/presentation/model"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/pipeline"
	seoutil "go_wp/internal/seo"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"
)

// resolveTemplate 解析本次构建使用的模板版本（issue #14）。
//
// templateID 非空 = 显式指定某套命名模板，并校验实体类型一致（不允许拿商品模板
// 去渲染文章）；为空 = 按实体类型取默认模板（既有行为）。
func (s *Service) resolveTemplate(ctx context.Context, projectID, entityType, templateID string) (tpl *contenttemplatecontract.ResolvedTemplate, err error) {
	if id := strings.TrimSpace(templateID); id != "" {
		// 类型判定必须**先于**文档解析。
		//
		// ResolveTemplateByID 会顺带按模板自己的类型校验文档：一份 article 模板里的
		// product 字段绑定在 article 数据源下必然越界，于是「类型抄错了」会被报成
		// 「字段绑定越界」，把人引向改模板内容而不是换模板 —— 而文档内容本身没错。
		if s.templateTypeMismatch(ctx, projectID, id, entityType) {
			return nil, errors.New(presentationenums.ErrTemplateTypeMismatch)
		}
		// 带工程作用域的解析：content_templates 带 FORCE 策略，
		// 不设 app.project_id 的读取在非超级角色下会「模板不存在」。
		tpl, err = s.templates.ResolveTemplateByIDScoped(ctx, projectID, id)
		if err != nil {
			// 原样透出：类型已确认相符，此时失败只剩「模板没了」或「文档校验没过」，
			// 后者的原文（哪个字段、越界在哪）正是排查需要的。
			return nil, err
		}
		return tpl, nil
	}
	tpl, err = s.templates.ResolveTemplateScoped(ctx, projectID, entityType)
	if err != nil {
		// 默认模板同样透出原因：该类型下确实没有模板与「模板存在但文档越界」
		// 是两件事，压成一句会让运营拿着「没有可用模板」去建一个新模板，
		// 而问题其实在新模板也会踩的字段绑定上。
		return nil, err
	}
	return tpl, nil
}

// templateTypeMismatch 判断显式指定的模板是否属于另一种内容类型。
//
// 只看模板行本身（Get），**刻意不走 ResolveTemplateByID** —— 后者会连带做文档
// 校验，而跨类型场景下文档几乎必然校验失败（字段绑定按另一种数据源解释），
// 于是判定结果为「文档有问题」而不是「模板不对」。模板不存在时返回 false：
// 那是另一种失败，交给后续的解析去报。
func (s *Service) templateTypeMismatch(ctx context.Context, projectID, templateID, entityType string) bool {
	if s.templates == nil {
		return false
	}
	tpl, err := s.templates.GetScoped(ctx, projectID, templateID)
	if err != nil || tpl == nil {
		return false
	}
	return tpl.EntityType != entityType
}

// resolveBoundTemplate 解析实例重建要用的模板：实例绑定的模板是权威。
//
// 未显式指定时**不**回落「同类型最新模板」——否则切换模板后一次内容更新
// 就会把产物换回别的模板（验收 4 的反面）。绑定模板已被删除或类型不符时
// 回落类型默认模板并记日志：重建优先于报错，产物仍能自愈。
func (s *Service) resolveBoundTemplate(ctx context.Context, inst *presentationmodel.InstanceEntity,
	explicitID string) (*contenttemplatecontract.ResolvedTemplate, error) {
	if strings.TrimSpace(explicitID) != "" {
		return s.resolveTemplate(ctx, inst.ProjectID, inst.EntityType, explicitID)
	}
	if id := strings.TrimSpace(inst.TemplateID); id != "" {
		tpl, err := s.templates.ResolveTemplateByIDScoped(ctx, inst.ProjectID, id)
		if err == nil && tpl.EntityType == inst.EntityType {
			return tpl, nil
		}
		logger.Scene("build").With("presentation_id", inst.ID).With("template_id", id).
			Warn("实例绑定的模板不可用，本次重建回落到该类型的默认模板")
	}
	return s.resolveTemplate(ctx, inst.ProjectID, inst.EntityType, "")
}

// withInstanceDocument 实例级文档覆盖（迁移 281，docs/04-C-instance-override.md）：
// 实例带 override_document 时以它为编译底稿（binding 照常解析，实体数据照常刷新），
// 否则原样返回模板 —— 既有行为零回归。
//
// 复制 ResolvedTemplate 只换 Document：TemplateID/VersionID 等身份字段保持模板侧
// 真值，下游的模板切换判定与快照 source_template_version_id 语义不变。
// 显式切换模板（换底稿 = 放弃自定义）的调用方**不得**包这层，见 Rebuild 的 switching 分支。
func withInstanceDocument(inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate) *contenttemplatecontract.ResolvedTemplate {
	if inst != nil && len(inst.OverrideDocument) > 0 {
		override := *tpl
		override.Document = inst.OverrideDocument
		return &override
	}
	return tpl
}

// buildArtifact 编译模板 AST（经 entity resolver）→ 产物落盘（**不激活**）。
//
// 激活由调用方在实例落库成功后单独执行（见 activate）：先激活后落库时，
// 一旦落库失败，线上已渲染出新实体内容却没有任何恢复入口。
//
// targetLangs 是本次批次准备上线的语言集合（SEO-026），逐字透传给 renderHTML 的
// hreflang 判定 —— 调用方必须传它逐语言结案用的那一份，不要在中间重新推导。
func (s *Service) buildArtifact(ctx context.Context, entityType, entityID, urlPath, projectID, lang string,
	targetLangs []string, tpl *contenttemplatecontract.ResolvedTemplate) (built builtArtifact, err error) {
	// 编译期依赖线索收集器（审计遗留缺口）：模板里的 core.nav 绑定菜单位置时，
	// 渲染期经 RenderContext.UseMenu 记录本次真正消费了哪个位置，构建后写进依赖表。
	// 不收集的表现是「改了导航，自动发布详情页永远停在旧字节」且没有任何报错。
	usage := &pipeline.CompileUsage{}
	// 降级归因收集器（审计 ARCH-05，与手工页面路径同一口径）：编译期记录「被容忍的
	// 降级」，构建成功后写进产物 Manifest.Diagnostics；失败路径根本产不出 Manifest。
	diags := builder.NewDegradeCollector()
	// 发布模式：显式绑定但拿不到的结构模板让本次构建失败（手工 Page 侧同一条判据）。
	html, err := s.renderHTML(ctx, entityType, entityID, urlPath, projectID, lang, targetLangs, tpl, usage,
		builder.CompileModePublish, diags)
	if err != nil {
		return built, err
	}
	// 结构槽位依赖：页眉 / 页脚绑定的结构模板（content_template:{id}）与模板文档内
	// 引用的块（block:{id}）。缺它 = 改了模板/块，引用页永远停在旧字节。
	// 判据与构建期解析同一函数（pipeline.BuildStructureSlots / StructureSlotDependencies），
	// 两处不可能分叉；回退到块绑定的槽位不会被登记成 content_template 依赖。
	var slotDeps []pipeline.Dependency
	// 集合源（审计 ARCH-01）：模板文档里声明的集合组件消费了哪个集合源。
	// 与手工页面路径**共用同一份判定**（core.CollectionSourcesOf，组件自己声明字段名），
	// 不在这里另做一次静态扫描 —— 两套口径迟早分叉，而分叉的表现正是「改了集合内容，
	// 某一类产物不重建」这种静默失效。
	var collectionSources []string
	if pageDoc, perr := builder.ParsePage(tpl.Document); perr == nil {
		slotDeps = pipeline.StructureSlotDependencies(ctx, s, projectID, pageDoc.Settings.Structure)
		collectionSources = core.CollectionSourcesOf(pageDoc.Root)
	}
	sourceHash := pipeline.SHA256(tpl.Document)
	// 多语言依赖（page 侧同一口径，见 page_lang.go §buildDependencies）：
	//   · i18n:site    组件固定文案（sys_i18n）—— 构建期取词注入 HTML 字节，恒登记；
	//   · i18n:content 内容译文（sys_translation）—— 仅当本次确有可翻译候选时登记。
	// 少登记的表现是「改了译文/词条，商品页永远是旧字节」且日志里什么都没有。
	deps := presentationDependencies(entityType, entityID, tpl.TemplateID, projectID, collectionSources, usage)
	deps = append(deps, pipeline.I18NDependency(i18n.Revision()))
	if usage != nil && usage.ContentTranslation {
		deps = append(deps, pipeline.I18NContentDependency(i18n.ContentRevisionForProject(ctx, projectID)))
	}
	deps = append(deps, slotDeps...)
	artifact, err := pipeline.NewArtifact(html, &pipeline.Manifest{
		ManifestSchemaVersion:     1,
		PageDocumentSchemaVersion: 1,
		SourceID:                  entityID,
		SourceType:                pipeline.SourceTypePresentation,
		CanonicalPath:             urlPath,
		SourceHash:                sourceHash,
		BuildInputHash:            sourceHash,
		Lang:                      strings.TrimSpace(lang),
		Dependencies:              deps,
		Diagnostics:               diags.Items(),
	})
	if err != nil {
		return built, err
	}
	loc, err := s.store.PutArtifact(artifact)
	if err != nil {
		return built, err
	}
	return builtArtifact{Hash: artifact.Hash, Loc: loc, Manifest: artifact.Manifest}, nil
}

// renderHTML 把模板 AST 经实体 resolver 编译为最终 HTML 字节（**不落盘、不落库、不激活**）。
//
// 发布（buildArtifact）与预览（PreviewInstance）共用这一份渲染，保证「预览看到的就是
// 发布出来的」；区别只在于发布还要把字节包成 Artifact 落盘并推进指针。
//
// urlPath 是该实例的线上路径（预览传空）：它是 SEO 头 canonical 的来源，
// 见 presentation_seo.go —— 唯一注入点的第二半（渲染函数本身不认识 SEO）。
// lang 为空时取站点默认语言；非默认语言时接入模板内作者文案翻译（ContentTranslator）。
//
// targetLangs 是本次批次准备上线的语言集合（SEO-026，预览传 nil）：hreflang 互指
// 按它生成，而不是回头读「某个语言是否已结案」—— 批次是「先构建全部语言、再逐语言
// 结案」，账本行在构建时还不存在，读它会让首发布产出的互指全部落空。
//
// mode 为本次编译的用途（审计 ARCH-05）：发布传 CompileModePublish（显式绑定但拿不到
// 的结构模板直接失败），预览传 CompileModePreview（降级为带归因的占位）。
// diags 为降级归因收集器（预览传 nil）：发布期收集的容忍降级写进产物 Manifest。
func (s *Service) renderHTML(ctx context.Context, entityType, entityID, urlPath, projectID, lang string,
	targetLangs []string, tpl *contenttemplatecontract.ResolvedTemplate, usage *pipeline.CompileUsage,
	mode builder.CompileMode, diags *builder.DegradeCollector) (html []byte, err error) {
	page, err := builder.ParsePage(tpl.Document)
	if err != nil {
		return nil, err
	}
	if s.registry == nil {
		return nil, errors.New(presentationenums.ErrRegistryMissing)
	}
	// 字段绑定的数据源白名单校验（不变量 4）：模板保存时已校验一次，这里再校一次
	// 是为了兜住「模板保存后才新增/改名数据源」与直连 service 的调用路径。
	if err = builder.ValidateFieldRefs(page, entityType, s.registry); err != nil {
		return nil, err
	}
	// 构建语言：实体字段与模板内文案按它取译文（语境 实体.字段名 / 组件 Translatable）。
	if strings.TrimSpace(lang) == "" {
		lang = s.resolveLang(ctx, projectID)
	}
	// 工程 id 必须进上下文（DB-009 第四批）：下面一行 ResolverFor 在 core.Compile **之前**
	// 调用，而 WithBuildProjectID 只在 Compile 内部补 —— 不在这里显式带上，构建期实体字段源
	// 拿不到工程 id，换非超级角色后按工程隔离的读取会 fail closed（字段渲染成空）。
	buildCtx := core.WithBuildProjectID(core.WithBuildLang(ctx, lang), projectID)
	logicalPath := pipeline.LogicalPathOf(ctx, s.project, projectID, urlPath)
	highlightPath := pipeline.HighlightPath(ctx, s.project, projectID, lang, logicalPath)
	if urlPath == "" {
		highlightPath = ""
	}
	resolver, err := s.registry.ResolverFor(buildCtx, entityType, entityID)
	if err != nil {
		return nil, err
	}
	// 实体字段驱动的 SEO 头（见 presentation_seo.go）：把 seoTitle / seoDescription 等
	// 喂进 page.Settings.SEO，随后的 builder.Compile 由既有的 BuildSEOHead 统一产出
	// canonical / OG / Twitter / JSON-LD。放在 Compile 之前是唯一有效的时机 ——
	// SEO 头在 compile 内一次性生成，之后没有回填入口。
	if err = applyEntitySEO(page, entityType, urlPath, s.registry.FieldWhitelist(entityType), resolver); err != nil {
		return nil, err
	}
	asm := pipeline.LoadPluginAssembly(ctx, s.plugins)
	set, pluginOpts, err := pipeline.ComponentSetWithPlugins(asm)
	if err != nil {
		return nil, err
	}
	// projectID 必须一起传：块查询以工程归属做越权防护，缺它只会拿到「参数缺失」，
	// 表现是页眉/页脚在这一层静默消失（降级不报错，产物只是少一截）。
	blockAdapter := newBlockResolverAdapter(s.blocks, buildCtx, projectID)
	// 归档上下文（审计 EDT-004）：**归档模板**渲染的是「实例实体下面的内容列表」，
	// 列表组件（core.productList 的 filterFromArchive）据此把筛选值落到实例实体上。
	//
	// 判据取**已解析模板的角色**，而不是调用方传参或实例行的角色：模板才是"这一页讲什么"
	// 的定义处（同一份模板既能给实例用也能给预览用，两条路径都必须拿到同样的上下文）。
	// 缺它的表现是"归档页列的是全站商品"——页面打得开、不报错，没人会发现。
	archiveOpts := []builder.CompileOption{}
	if tpl != nil && strings.TrimSpace(tpl.TemplateRole) == contenttemplatecontract.TemplateRoleArchive {
		archiveOpts = append(archiveOpts, builder.WithArchiveEntity(entityType, entityID))
	}
	compileOpts := []builder.CompileOption{
		builder.WithContext(buildCtx),
		builder.WithComponentSet(set),
		builder.WithContentResolver(resolver),
		builder.WithBlockResolver(blockAdapter),
		// 引用失败策略（审计 ARCH-05）：发布期显式绑定但拿不到的块 / 模板直接失败。
		builder.WithCompileMode(mode),
	}
	if diags != nil {
		compileOpts = append(compileOpts, builder.WithDegradeCollector(diags))
	}
	compileOpts = append(compileOpts, pluginOpts...)
	compileOpts = append(compileOpts, archiveOpts...)
	compileOpts = append(compileOpts, pipeline.LocaleCompileOptions(lang)...)
	// hreflang 互指的判定依据是**本批次准备上线哪些语言**（SEO-026），不是「某个
	// 访问路径是否已发布」：后者要等逐语言结案才成立，首发布构建时它必然为假，
	// 于是互指全部落空、产物缺 hreflang，重建一次（账本已写全）才补上。
	//
	// 因此发布路径不再传 RoutePublished —— 传了反而会被它盖住（见 SiteCompileParams.TargetLangs）：
	// TargetLangs 非空时 published 不参与判定。预览（targetLangs 为空）沿用访问面口径：
	// 预览没有「本次要上线谁」这件事，它面向的是线上现状 + 这份草稿内容。
	siteOpts, serr := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
		Project: s.project, Navigation: s.navigation, SitePages: s.sitePages, MediaProbe: s.mediaProbe,
		RoutePublished: func(accessPath string) bool {
			state, ierr := s.publication.Inspect(accessPath)
			return ierr == nil && state != nil && state.Kind != "none"
		},
	}, pipeline.SiteCompileParams{
		Ctx: ctx, ProjectID: projectID, Lang: lang,
		LogicalPath: logicalPath, CurrentPath: highlightPath,
		TargetLangs: targetLangs,
	})
	if serr != nil {
		return nil, serr
	}
	compileOpts = append(compileOpts, siteOpts...)
	if page.Settings.Theme != nil {
		compileOpts = append(compileOpts, builder.WithThemeSettings(page.Settings.Theme))
	}
	compileOpts = append(compileOpts, pipeline.ClientAssetOptions()...)
	// 依赖线索收集器只在发布路径注入；预览传 nil（预览不落依赖表，收集了也无处可写）。
	if usage != nil {
		compileOpts = append(compileOpts, builder.WithUsageRecorder(usage))
	}
	// 结构槽位（审计 VIS-001）：页眉 / 页脚与模板主体走同一次编译，不再拼字符串。
	// 结构模板优先、块绑定回退（pipeline.BuildStructureSlots，与手工页面路径**同一份实现**）。
	// **回退只在预览成立**（审计 ARCH-05）：发布路径下显式绑定了模板却拿不到（不存在 /
	// 跨工程 / 文档非法）直接返回错误，由调用方让本次构建整体失败 —— 自动发布实例同样
	// 不该把缺了页眉的详情页推上线。没绑定时按显式设计处理。
	//
	// 必须在取词器构造**之前**算出槽位：结构模板文档不在块表里，它的可翻译文本要经
	// 叠加了解析器的 slotResolver 才能进候选集合（否则模板里的文案永远不翻译且无人报错）。
	slotRes, slotErr := pipeline.BuildStructureSlots(ctx, pipeline.StructureSlotInput{
		Port: s, ProjectID: projectID, Structure: page.Settings.Structure,
		Inner: blockAdapter, Mode: mode, Diagnostics: diags,
	})
	if slotErr != nil {
		// 结构绑定拿不到 = 产物必然缺一截：整体失败（不落盘、不落库、不激活），
		// 线上保持原样。预览路径不会走到这里（预览模式按降级处理）。
		return nil, slotErr
	}
	slotList, slotResolver := slotRes.Slots, slotRes.Resolver
	var contentTranslator *i18n.ContentTranslator
	var contentCandidates int
	compileOpts, contentTranslator, contentCandidates = pipeline.AppendContentTranslation(
		compileOpts, ctx, s.project, projectID, lang, page, slotResolver.ResolveBlockRoot, s.newContentTranslator)
	// 集合源注入（issue #9）：模板里的集合类组件按白名单展开商品等集合数据。
	if s.collection != nil {
		compileOpts = append(compileOpts, builder.WithCollectionResolver(s.collection))
	}
	// 商品数据源（issue #35）：与 page 路径同一注入方式。
	if s.productDS != nil {
		compileOpts = append(compileOpts, builder.WithProductDataSource(s.productDS))
	}
	// 结算表单的国家下拉（core.checkoutForm）：与 page 路径同一注入方式与同一条判据
	//（取到空清单不注入，让「表单里有国家字段」的构建显式失败）。
	if s.checkoutCountries != nil {
		if countries := s.checkoutCountries(ctx, lang); len(countries) > 0 {
			compileOpts = append(compileOpts, builder.WithCheckoutCountries(countries))
		}
	}
	compileOpts = append(compileOpts, pipeline.AnalyticsCompileOptions(ctx, s.project, projectID)...)
	if len(slotList) > 0 {
		compileOpts = append(compileOpts, builder.WithBlockResolver(slotResolver))
		compileOpts = append(compileOpts, builder.WithStructureSlots(slotList...))
	}
	compiled, err := builder.Compile(page, compileOpts...)
	if err != nil {
		return nil, err
	}
	if contentTranslator != nil {
		pipeline.LogContentTranslationMisses(lang, contentCandidates, contentTranslator.Misses())
	}
	// 本次构建有可翻译候选 = 产物字节受 sys_translation 影响（补齐/修改译文都会变），
	// 供 buildArtifact 登记 i18n:content 依赖。预览 usage 为 nil，不登记。
	if usage != nil && contentCandidates > 0 {
		usage.UseContentTranslation()
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
}

// activate 把本次产物激活到线上 URL（覆盖 active 符号链接）。
// 调用时机：实例 / 快照 / 产物行 / 指针全部落库成功之后。
func (s *Service) activate(urlPath string, built builtArtifact) error {
	if err := s.publication.Activate(urlPath, built.Loc); err != nil {
		return fmt.Errorf("激活 %s 失败: %w", urlPath, err)
	}
	return nil
}

// presentationDependencies 本次产物的依赖源集合。
//
//   - direct_content:{type}:{id}    —— 内容实体字段变化（PIPE-3 自动重建的触发键，
//     与 content 模块 notifyContentChanged 声明的键逐字一致）；
//   - content_template:{templateID} —— 模板版本变化（kind 见 pipeline.DepKindContentTemplate；
//     当前无来源模块触发该键，登记用于审计与后续接入）；
//   - menu:{projectID}:{kind} —— 模板里的 core.nav 绑定了该菜单位置（编译期消费记录，
//     键构造见 pipeline.MenuKey；与手工页面路径用同一个构造函数，两侧必须一致）。
//
// projectID 为空（极罕见：调用方未解析出工程）时不登记导航依赖：菜单键必须带工程，
// 猜一个工程 ID 会把失效范围指到别的站点上。
func presentationDependencies(entityType, entityID, templateID, projectID string,
	collectionSources []string, usage *pipeline.CompileUsage) []pipeline.Dependency {
	dep := pipeline.DirectContentKey(entityType, entityID)
	out := []pipeline.Dependency{{Kind: dep.Kind, Key: dep.Key}}
	// 集合依赖（审计 ARCH-01）：模板文档里声明的集合源（content:product 等）的**成员与
	// 成员可见字段**变化会让本实例的字节变化 —— 分类归档页（EDT-004）就是这一类：
	// 它的 direct_content 键只认自己那个分类（product_category:{id}），商品增删改
	// 全都不命中，于是「新建一个商品，归档页不更新」且没有任何报错。
	// 键的构造与 page 侧、与 content 模块的发射端用同一个构造函数，逐字一致。
	for _, src := range collectionSources {
		if src = strings.TrimSpace(src); src == "" {
			continue
		}
		out = append(out, pipeline.Dependency{
			Kind: pipeline.DepKindContentCollection,
			Key:  "collection:" + src,
		})
	}
	if strings.TrimSpace(templateID) != "" {
		out = append(out, pipeline.Dependency{
			Kind: pipeline.DepKindContentTemplate,
			Key:  "content_template:" + templateID,
		})
	}
	if pid := strings.TrimSpace(projectID); pid != "" {
		for _, kind := range usage.MenuList() {
			k := pipeline.MenuKey(pid, kind)
			out = append(out, pipeline.Dependency{Kind: k.Kind, Key: k.Key})
		}
		// 按**具体菜单项**引用：键 navigation:{itemID}（与 page 侧同一构造函数）。
		for _, navID := range usage.NavigationList() {
			k := pipeline.NavigationKey(navID)
			out = append(out, pipeline.Dependency{Kind: k.Kind, Key: k.Key})
		}
		// 渲染期展开的块（菜单悬浮面板）：块 id 不在文档里，只有 UseBlock 记录的这一份。
		for _, blockID := range usage.BlockList() {
			k := pipeline.BlockKey(blockID)
			out = append(out, pipeline.Dependency{Kind: k.Kind, Key: k.Key})
		}
	}
	return out
}

// resolveLang 预览或未显式传 lang 时的回退：工程默认语言（语言清单 is_default）。
// 正式发布经 publishAllLangs 逐语言传入 lang，不依赖本函数。
func (s *Service) resolveLang(ctx context.Context, projectID string) string {
	if s.project == nil || strings.TrimSpace(projectID) == "" {
		return ""
	}
	lang, err := s.project.DefaultLocale(ctx, projectID)
	if err != nil {
		logger.Scene("build").With("project_id", projectID).
			Warn("构建语言解析失败，本次构建按原文输出: " + err.Error())
		return ""
	}
	return strings.TrimSpace(lang)
}

// resolveProjectID 解析实例所属工程：显式传入优先（校验存在），
// 否则经 project 契约取唯一工程；无工程或多工程时要求显式指定。
//
// 归属校验的边界（评审 H1-b 的取证结论，改这里之前先读完这段）：
//   - 应用层**没有**「这个工程属于谁」这个事实：projects 表只有 id/name/settings/时间戳
//     （public/migrations/init_builder_schema.sql），没有所有者列；会话里也没有「当前工程」
//     （全仓 active_project / currentProject 零命中）；Casbin 策略的 obj 是 API 路径、没有工程维度。
//     所以这里能校验的只有「工程存在」—— 凭空写一段 owner 判断只会得到一个假的授权检查。
//   - membership 模块不是这里的答案：public/migrations/462_membership.sql 的三张表是面向**顾客**的
//     会员体系（membership_tiers 等级 / membership_entitlements 权益 / membership_assignments
//     把 user_id 分配到等级），描述「谁买了什么」，不描述「后台账号能操作哪个工程」。
//   - 「谁能调写面」由 Casbin 权限点承担：POST /workbench/instance/save 挂
//     /api/presentation/rebuild（builtin.CasbinMiddlewareForPath），页面写端点复用 API 权限点。
//   - 「写入只能落在当前作用域的工程行」由 RLS 在数据库层承担：presentation_instances 在
//     迁移 215 的隔离清单内，model 的 *Tx 变体内部调 rls.ScopeTx（护栏见
//     public/test/rls/rls_presentation_scope_test.go 的「事务变体必须自己带上作用域」）。
//   - 因此契约缺失时**不能放行**：原写法是「project != nil 才校验」，缺契约时任意 projectID
//     都跳过存在性校验直达写入 —— 装配缺口不该变成授权缺口，这里一律 fail closed。
func (s *Service) resolveProjectID(ctx context.Context, explicit string) (string, error) {
	if s.project == nil {
		return "", errors.New(presentationenums.ErrProjectRequired)
	}
	if id := strings.TrimSpace(explicit); id != "" {
		ok, err := s.project.Exists(ctx, id)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errors.New(presentationenums.ErrProjectNotFound)
		}
		return id, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(presentationenums.ErrProjectRequired)
	}
	return list[0].ID, nil
}

func seoCanonicalPath(path string) string {
	return seoutil.CanonicalPublicPath(path)
}

// builder 页面设置校验的字段上限（internal/builder/settings.go 的 validateSettings）：
// title ≤ 200 字节、description ≤ 500 字节，超限直接让 Compile 失败。
//
// 失败的代价很大而收益为零：上层只拿到 ErrBuildFailed，要知道"是 SEO 标题太长"
// 得翻到设置校验那一层；而当事人（填标题的运营）看到的是"发布失败"。
// 所以这里按上限**截断**：SEO 标题与描述本来就是会被搜索结果截断的东西
// （Google 大约 60 字符就开始视觉截断），少几个字可以接受，整页发不出去不可以接受。
// 截断不是静默的 —— 每次都会记一条 warn（带实体类型、字段、上限与实际字节数）。
const (
	seoTitleLimitBytes       = 200
	seoDescriptionLimitBytes = 500
)

// 自动发布实例的两种实体类型标识。
//
// 商品取商品模块契约的常量（类型标识的唯一来源）；文章没有导出常量（内容模块的
// 白名单以字面量 article 为键），这里按该键逐字取值。
const (
	entityTypeArticle = "article"
	entityTypeProduct = productcontract.EntityTypeProduct
)

// seoTitleCandidates / seoDescriptionCandidates 实体字段的候选链（顺序即回落顺序）。
//
// 覆盖两类详情页：
//   - 文章：seoTitle → title；seoDescription → excerpt（字段白名单见 content 模块契约）；
//   - 商品：白名单里没有 seoTitle / seoDescription（只有分类与品牌有），
//     标题回落 name、描述回落 description。
//
// 落在白名单外的候选会被直接跳过（见 pickSEOField）：解析器对白名单外的字段
// 是**报错**而不是返回空串，「文章没有 name 字段」不是错误，只是这个候选不适用。
var (
	seoTitleCandidates       = []string{"seoTitle", "title", "name"}
	seoDescriptionCandidates = []string{"seoDescription", "excerpt", "description"}
)

// schemaTypeOf 实体类型 → 结构化数据类型（builder schemaTypeMap 的取值域）。
//
// 只映射两类详情页；未知类型返回空串 = 不写这个键，模板已填的 schemaType 原样保留
// （builder 对空值回落 WebPage，与「没有这个功能」时的产物一致）。
func schemaTypeOf(entityType string) string {
	switch entityType {
	case entityTypeArticle:
		return entityTypeArticle
	case entityTypeProduct:
		return entityTypeProduct
	}
	return ""
}

// entityImageCandidates 各实体类型的头图字段候选（顺序即回落顺序）。
//
// 只列**确实存在且语义就是头图**的字段：
//   - 文章：featuredImage（内容字段白名单里的封面）；
//   - 商品：defaultImage 是主图，images 是图集（取首张）。
//
// 候选外的字段一律不看 —— 猜一个字段名只会得到一张错的分享图。
var entityImageCandidates = map[string][]string{
	entityTypeArticle: {"featuredImage"},
	entityTypeProduct: {"defaultImage", "images"},
}

// pickEntityImage 取实体头图（取不到返回空串，不报错）。
//
// 图集字段（images）在实体里是 JSON 数组字符串，这里取首张 —— 与前台商品卡同口径
// （都取图集第一张当主图），避免「卡片与分享图不是同一张」。
func pickEntityImage(entityType string, writable []string, resolver core.ContentResolver) string {
	if resolver == nil {
		return ""
	}
	for _, field := range entityImageCandidates[entityType] {
		if !fieldWritable(writable, field) {
			continue
		}
		v, err := resolver.ResolveString(entityType + "." + field)
		if err != nil {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "[") {
			var list []string
			if jerr := json.Unmarshal([]byte(v), &list); jerr == nil && len(list) > 0 {
				v = strings.TrimSpace(list[0])
			}
		}
		if v != "" {
			return v
		}
	}
	return ""
}

// applyEntitySEO 把实体字段与实例线上路径写进页面 SEO 设置（Compile 之前调用）。
//
// writable 是该实体类型的字段白名单（取自实体类型注册表 —— 白名单的唯一来源，
// 这里不另维护一份）；resolver 是绑定该实体的构建期解析器，读到的值已按构建语言
// 取过译文（语境 实体类型.字段名），因此 SEO 头与页面正文的语言一致。
func applyEntitySEO(page *builder.Page, entityType, urlPath string,
	writable []string, resolver core.ContentResolver) error {
	title, err := pickSEOField(entityType, writable, resolver, seoTitleCandidates, seoPlainText)
	if err != nil {
		return err
	}
	description, err := pickSEOField(entityType, writable, resolver, seoDescriptionCandidates, seoDescriptionText)
	if err != nil {
		return err
	}
	// 空值不覆盖：只写取到的实体字段（取舍 1）。超限按上限截断（见 clampSEOField）。
	if title != "" {
		page.Settings.SEO.Title = clampSEOField(entityType, "seoTitle", title, seoTitleLimitBytes)
	}
	if description != "" {
		page.Settings.SEO.Description = clampSEOField(entityType, "seoDescription", description, seoDescriptionLimitBytes)
	}
	if st := schemaTypeOf(entityType); st != "" {
		page.Settings.SEO.SchemaType = st
	}
	if entityType == entityTypeProduct {
		if offer := productOfferLD(entityType, writable, resolver); offer != nil {
			page.Settings.SEO.ProductOffer = offer
		}
	}
	// 头图 → og:image（社交分享卡片与 JSON-LD 的 image）。
	//
	// 此前只填了 title / description / schemaType，**没填图** —— 详情页的产物里
	// 一条 og:image 都没有，分享到任何平台都是无图卡片（twitter:card 也退回 summary）。
	// 取不到就不写：宁可没有图，也不要一张猜出来的图。
	if img := pickEntityImage(entityType, writable, resolver); img != "" {
		// 先归一到媒体自己的对外地址（upload.StorageURL），再交给 BuildSEOHead。
		// 顺序要紧：实体里存的可能是相对路径 /storage/x.jpg，直接交给 BuildSEOHead 的
		// absoluteURL 会**套上站点基址的前缀** —— 基址是 /site 时拼成
		// /site/storage/x.jpg，而媒体其实在站点根的 /storage 下（实测踩过）。
		page.Settings.SEO.OGImage = upload.StorageURL(img)
	}
	// 线上路径覆盖模板 canonical（取舍 2）；预览（urlPath 空）不动模板原值。
	if path := strings.TrimSpace(urlPath); path != "" {
		page.Settings.SEO.Canonical = seoCanonicalPath(path)
	}
	return nil
}

// productOfferLD 商品 JSON-LD 扩展：构建期静态 Offer/评分（不含实时库存，SEO-005）。
func productOfferLD(entityType string, writable []string, resolver core.ContentResolver) *builder.ProductOfferLD {
	read := func(field string) string {
		if !fieldWritable(writable, field) {
			return ""
		}
		v, err := resolver.ResolveString(entityType + "." + field)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(v)
	}
	price := read("price")
	if price == "" {
		return nil
	}
	offer := &builder.ProductOfferLD{
		SKU:   read("sku"),
		Price: price,
		// 币种取**全局默认**（进程内缓存值，不查库）：这一层是展示 / 结构化数据，
		// 接上不会动任何金额口径 —— 购物车与订单的金额仍固定人民币（那里的常量
		// 不能跟着本值走，否则会造出「配置设成 USD、订单落 USD，金额却按人民币算」
		// 的新不一致，见 pkg/i18n.RuntimeValues.DefaultCurrency 的注释）。
		PriceCurrency: i18n.GetDefaultCurrency(),
		Availability:  "InStock",
	}
	if variants := read("variants"); variants == "" || variants == "[]" {
		offer.Availability = "OutOfStock"
	}
	if rc := read("ratingCount"); rc != "" {
		if n, err := strconv.Atoi(rc); err == nil && n > 0 {
			offer.RatingCount = n
			if rv := read("rating"); rv != "" {
				if f, err := strconv.ParseFloat(rv, 64); err == nil && f > 0 {
					offer.RatingValue = f
				}
			}
		}
	}
	return offer
}

// clampSEOField 按字节上限截断，并回退到完整 UTF-8 边界（不切碎多字节字符）。
//
// 回退后可能变空（只有超长多字节串才会，实际到不了）—— 那时返回空串，
// 调用方据此不写这个键、保留模板原值：半个字的 SEO 标题比没有更糟。
func clampSEOField(entityType, field, value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := value[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	logger.Scene("build").
		With("entityType", entityType).
		With("field", field).
		With("limitBytes", limit).
		With("actualBytes", len(value)).
		Warn("实体 SEO 字段超过页面设置上限，已按上限截断")
	return strings.TrimSpace(cut)
}

// pickSEOField 按候选链取第一个非空字段值（全空返回空串）。
//
// normalize 既决定判空口径，也决定最终写进 SEO 的字节 —— 「富文本里只剩标签」
// 这种值因此会被当成空（不把 <p></p> 写进 meta description）。
func pickSEOField(entityType string, writable []string, resolver core.ContentResolver,
	candidates []string, normalize func(string) string) (string, error) {
	for _, name := range candidates {
		if !fieldWritable(writable, name) {
			continue
		}
		raw, err := resolver.ResolveString(entityType + "." + name)
		if err != nil {
			// 白名单内的字段读不到是真实故障（注册表与解析器口径不一致 / 实体数据损坏）：
			// 返回错误让构建显式失败，而不是静默产出一个没有 SEO 的详情页。
			return "", fmt.Errorf("读取实体 SEO 字段 %s.%s 失败: %w", entityType, name, err)
		}
		if v := normalize(raw); v != "" {
			return v, nil
		}
	}
	return "", nil
}

// fieldWritable 字段是否在该实体类型的白名单内。
func fieldWritable(writable []string, name string) bool {
	for _, f := range writable {
		if f == name {
			return true
		}
	}
	return false
}

// blockBoundaryRe 块级标签的边界（闭合标签与换行标签）。
//
// core.StripRichTags 只取文本节点、不在块之间插分隔，段落会粘成「透气四季可穿」；
// meta 描述与 og:title 要的是可读的单行文本，所以先把边界换成空格再交给它清洗
// （仍然只有一份清洗口径，不另写一套去标签逻辑）。
// 注意 br 的写法要容得下 <br> / <br/> / <br />：漏了带空格那种（HTML 里很常见），
// 换行就会被当成"没有边界"，前后两段文字直接粘在一起。
// 块边界集合要跟着富文本白名单走：白名单新增块级元素（summary/details/thead/tbody/tfoot/
// caption，见 core/richtext.go）而这里不补，去标签后「标题」与「正文」会**粘成一串**
// （<summary>标题</summary><p>正文</p> → 标题正文），meta 描述与 JSON-LD 都会带上这种噪声。
var blockBoundaryRe = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|td|th|blockquote|summary|details|thead|tbody|tfoot|caption|dt|dd)>|<br\s*/?>`)

// seoPlainText 实体字段值 → 可进 <head> 的纯文本。
//
// 商品描述（product.description）是富文本 HTML（见商品模块的 descriptionHTML），
// 直接写进 meta 会被转义成 "&lt;p&gt;…" 这样的可见噪声，所以含标记时先去标签。
// 纯文本字段（标题 / 摘要）不含标记，原样返回。
func seoPlainText(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || !core.HasRichMarkup(v) {
		return v
	}
	v = blockBoundaryRe.ReplaceAllString(v, " ")
	return strings.TrimSpace(core.StripRichTags(v))
}

// seoDescriptionText 描述的进一步归一：富文本去标签后会留下换行与连续空格，
// 折叠成单空格（meta 描述与 JSON-LD 都是单行文本，产物字节也因此稳定）。
func seoDescriptionText(v string) string {
	return strings.Join(strings.Fields(seoPlainText(v)), " ")
}

type langBuildResult struct {
	lang       string
	accessPath string
	built      builtArtifact
	artifactID string
}

// instanceLogicalPath 实例 url_path 列存逻辑路径；若历史数据带语言前缀则剥掉。
func (s *Service) instanceLogicalPath(ctx context.Context, inst *presentationmodel.InstanceEntity) string {
	return pipeline.LogicalPathOf(ctx, s.project, inst.ProjectID, inst.URLPath)
}

// normalizeLogicalPath 创建/改 URL 时把输入路径归一化为逻辑路径。
func (s *Service) normalizeLogicalPath(ctx context.Context, projectID, raw string) (string, error) {
	normalized, err := pipeline.NormalizeURL(raw)
	if err != nil {
		return "", err
	}
	return pipeline.LogicalPathOf(ctx, s.project, projectID, normalized), nil
}

// ensureLogicalPathFree 预检逻辑路径下全部语言访问路径均未被占用。
func (s *Service) ensureLogicalPathFree(ctx context.Context, projectID, logicalPath, excludeInstanceID string) error {
	entries, err := pipeline.SiteRouteEntries(ctx, s.project, projectID, logicalPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := s.ensurePathFree(ctx, projectID, e.Path, excludeInstanceID); err != nil {
			return err
		}
	}
	// 逻辑路径本身也不得与其他实例冲突（UNIQUE project_id + url_path）。
	if _, err := s.m.FindInstanceByPath(ctx, projectID, logicalPath, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", logicalPath).Warn("详情页逻辑路径预检被拒绝：已被其他展示实例占用")
		return errors.New(presentationenums.ErrPathOccupied)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

// publishLangsOf 发布口径的站点启用语言：语言清单读不到即返回错误（审计 I18N-02）。
//
// 与 page 模块同名方法同义：本模块「会改动访问面 / 会写发布事实」的两处
// （publishAllLangs 的整批发布循环、batchConverged 的收敛判定）都走它 ——
// 前者降级会只发布默认语言并推进指针，后者降级会把「每种语言都有账本行」的检查
// 缩成只查默认语言那一行（假收敛）。两处的失败处置不同（一个中止整批、一个本轮
// 不处置），但**取数口径必须是同一条**，否则判定依据会随调用点漂移。
func (s *Service) publishLangsOf(ctx context.Context, projectID string) ([]string, error) {
	return pipeline.ResolveSiteLangs(ctx, s.project, projectID, pipeline.LangFallbackForbidden)
}

// publishAllLangs 按站点启用语言构建、逐语言结案，整批成功后才推进实例指针。
//
// 三段式（PERF-01；实例锁到底保护什么见 presentation_publish_plan.go 的文件头）：
//
//	① 锁内冻结本次发布的不可变输入（实例行 + 语言清单 + 编译底稿）
//	② 锁外逐语言编译与产物落盘 —— 最慢的一段，不再占着实例锁
//	③ 锁内校验实例状态未变 → 发布计划落库 → 逐语言结案 → 推进实例指针
//
// ② 期间别的发布会话可以先进锁提交（锁已让出）。此时 ③ 的指纹校验会判定本次编译
// 基于过期的实例状态，丢弃结果并重新冻结重试 —— 而不是把旧输入写回去（L4）。
func (s *Service) publishAllLangs(ctx context.Context, inst *presentationmodel.InstanceEntity,
	intent publishIntent) (primaryArtifactID string, err error) {
	// 分阶段耗时（PERF-01 验收）：成功与失败都发一条 Info（失败时带原因），
	// 于是「锁等待还剩多少 / 编译占多少 / 有没有被冲突重试拖长」在生产上可见。
	metrics := publishSessionMetrics{startedAt: time.Now()}
	defer func() { s.logPublishSession(inst, metrics, err) }()

	var moved error
	for attempt := 1; attempt <= publishAttempts; attempt++ {
		metrics.attempts = attempt
		frozen, freezeTiming, ferr := s.freezePublish(ctx, inst, intent)
		metrics.lockWaitFreeze += freezeTiming.lockWait
		metrics.freeze += freezeTiming.work
		if ferr != nil {
			return "", ferr
		}
		metrics.langs = len(frozen.langs)

		results, compileWork, cerr := s.compileLangs(ctx, inst, frozen)
		metrics.compile += compileWork
		if cerr != nil {
			return "", cerr
		}

		var commitTiming phaseTiming
		primaryArtifactID, commitTiming, moved = s.commitPublish(ctx, inst, frozen, results)
		metrics.lockWaitCommit += commitTiming.lockWait
		metrics.commit += commitTiming.work
		if moved == nil {
			return primaryArtifactID, nil
		}
		if !errors.Is(moved, errPublishStateMoved) {
			return "", moved
		}
		// 冲突：编译结果整体作废（校验在任何写入之前，因此这里是零副作用失败），
		// 重新冻结实例状态再来一次。产物是内容寻址的，重试若产出同样的字节，
		// 落库阶段按 hash 复用同一行（recordArtifactTx），不会堆出版本。
		logger.Scene("build").With("instanceId", inst.ID).With("attempt", attempt).
			Warn("发布会话的实例状态已被并发批次推进，丢弃本次编译结果并重新冻结")
	}
	return "", fmt.Errorf("发布会话连续 %d 次被并发的发布批次推进实例状态，本次中止: %w", publishAttempts, moved)
}

// publishOneLang 单语言结案：登记回执 → 激活访问面 → 登记路由 → 写语言账本 → 结案。
//
// 任一步失败都向上返回，且该语言的 publication 行不会被写入（它只在走到倒数第二步
// 时才写）—— 「数据库声称已发布」与「文件真的在线」之间不允许有窗口。
// 回执保留 pending 时记录的正是「切到了哪一步」，交由启动恢复补齐或结案为未生效。
func (s *Service) publishOneLang(ctx context.Context, inst *presentationmodel.InstanceEntity,
	b *langBuildResult, fromArtifactID string, now time.Time) error {
	// 切换是不可逆的访问面副作用：先有账本才谈得上恢复（登记失败即中止，不切换）。
	receiptID, berr := s.beginPublishReceipt(ctx, inst, b.accessPath, b.lang, fromArtifactID, b.artifactID)
	if berr != nil {
		return fmt.Errorf("登记 %s 的发布回执失败（未切换访问面）: %w", b.accessPath, berr)
	}
	if aerr := s.activate(b.accessPath, b.built); aerr != nil {
		s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
		return aerr
	}
	// 路由登记失败不再只是日志里的 Warn（审计 AR2-004）：访问面已切换、路由账本缺行，
	// 会让占用预检 / 回滚 / 删除 / GC 全部依据错误的路由表决策。回执保持 pending。
	if rerr := s.registerRoute(ctx, inst, b.accessPath, b.artifactID); rerr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
			Error(rerr, "多语言路由登记失败（访问面已激活，回执待恢复）")
		// 回执留在 pending：推一次进程内快通道让收敛立刻重放（主链失败收口，非阻塞）。
		s.NotifyPendingReceipt()
		return fmt.Errorf("登记 %s 的路由占用失败（访问面已激活，回执 %s 待恢复）: %w",
			b.accessPath, receiptID, rerr)
	}
	// 语言账本：走到这里，这个语言的 URL 才真的可访问。
	if merr := s.m.MarkPublishedLang(ctx, presentationmodel.PublicationRecord{
		PresentationID: inst.ID, Lang: b.lang, ActivePath: b.accessPath,
		ArtifactID: b.artifactID, ArtifactHash: b.built.Hash, PublishedAt: now,
	}); merr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
			Error(merr, "多语言语言账本写入失败（访问面与路由已生效，回执待恢复）")
		// 同上：回执留在 pending，快通道让收敛按访问面证据补齐（幂等）。
		s.NotifyPendingReceipt()
		return fmt.Errorf("写入 %s 的发布账本失败（访问面已激活，回执 %s 待恢复）: %w",
			b.accessPath, receiptID, merr)
	}
	// 结案失败只意味着账本没落终结态：访问面、路由与语言账本都已生效，未结案回执会在
	// 下次启动恢复时按同样证据补齐（幂等），因此不把该语言判为失败。
	if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
			Warn("多语言发布回执结案失败（访问面、路由与语言账本已生效，留待恢复补齐）: " + cerr.Error())
		// 状态已一致、回执没收口：推快通道让收敛立刻幂等收尾。
		s.NotifyPendingReceipt()
	}
	return nil
}

// finalizeMultiLangBatch 整批成功后的收尾：推进实例指针（active/staged = 默认语言产物）、
// 清 stale、记发布时间。
//
// 指针落后于访问面（部分语言失败）是安全方向：stale + pending 回执 + 启动恢复会收敛，
// 而且指针指向的旧产物是内容寻址的不可变文件，仍在原处可访问。反方向——指针声称已发布
// 而文件不存在——才会骗到 sitemap 与后台，这里从顺序上排除了它。
func (s *Service) finalizeMultiLangBatch(ctx context.Context, inst *presentationmodel.InstanceEntity,
	snapID, primaryArtifactID string, now time.Time) error {
	if strings.TrimSpace(primaryArtifactID) == "" || strings.TrimSpace(snapID) == "" {
		return errors.New("多语言发布缺少默认语言产物或快照，无法推进实例指针")
	}
	inst.CurrentSnapshotID = &snapID
	inst.StagedSnapshotID = &snapID
	inst.StagedArtifactID = &primaryArtifactID
	inst.ActiveArtifactID = &primaryArtifactID
	inst.Stale = false
	inst.PublishedAt = &now
	inst.UpdatedAt = now
	return s.m.UpdateInstancePointers(ctx, inst)
}

// markBatchUnconverged 批次未收敛时把实例标记为待重建（后台可见的「明确 partial」）。
//
// 语言账本里缺哪些语言是精确的（缺行 = 该语言没上线），stale 则表达「这个实例还有
// 一批没走完」：依赖失效触发的自动重建、构建队列或启动恢复都会把它收敛回全成功。
func (s *Service) markBatchUnconverged(ctx context.Context, inst *presentationmodel.InstanceEntity, cause error) {
	inst.Stale = true
	if _, err := s.m.MarkStale(ctx, inst.ProjectID, []string{inst.ID}, time.Now().UTC()); err != nil {
		logger.Scene("build").With("instanceId", inst.ID).Error(err, "标记实例待重建失败")
	}
	logger.Scene("build").With("instanceId", inst.ID).
		Error(cause, "多语言发布批次未收敛（实例已标记待重建）")
}

// publishedArtifactsByLang 实例当前各语言的活跃产物 id（无记录 / 查询失败时为空表）。
func (s *Service) publishedArtifactsByLang(ctx context.Context, instanceID string) map[string]string {
	out := map[string]string{}
	pubs, err := s.m.ListPublications(ctx, instanceID)
	if err != nil {
		return out
	}
	for _, pub := range pubs {
		if pub.ArtifactID != nil {
			out[pub.Lang] = *pub.ArtifactID
		}
	}
	return out
}

// persistMultiLangArtifacts 一次快照 + 多语言账本产物行 + 模板/路径变更 + 依赖记录。
//
// 这个事务**不声明任何语言已上线**：它只登记「本次构建产出了哪些字节」（产物行）与
// 「构建输入是什么」（快照、依赖）。语言账本行与实例指针由逐语言结案和批次收尾各自
// 写入 —— 把「记账」与「上线」分开，才有 AR2-003 要的那条不变式。
func (s *Service) persistMultiLangArtifacts(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate, results []langBuildResult, now time.Time,
	logicalPath, defaultLang string, mode *instanceModePending) (primaryArtifactID, snapID string, err error) {
	snapID = uuid.NewString()
	snap := &presentationmodel.SnapshotEntity{
		ID: snapID, PresentationInstanceID: inst.ID,
		SourceTemplateVersionID: tpl.VersionID, SourceEntityRevisionID: inst.EntityID,
		Document: tpl.Document, CreatedAt: now,
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateSnapshotTx(tx, snap); cerr != nil {
			return cerr
		}
		// 渲染模式与独立文档（双轨，迁移 282）与快照/产物/指针同事务：
		// 分开写会留下「文档已换、模式没换」的中间态，下一次模板更新就会按
		// template 模式把它重建回模板文档 —— 用户的自定义凭空消失。
		if mode != nil {
			if uerr := s.m.UpdateInstanceModeTx(tx, inst.ProjectID, inst.ID, mode.renderMode, mode.document, now); uerr != nil {
				return uerr
			}
			inst.RenderMode = mode.renderMode
			if mode.renderMode == presentationmodel.RenderModeDocument {
				inst.OverrideDocument = mode.document
			} else {
				inst.OverrideDocument = nil
			}
		}
		if inst.TemplateID != tpl.TemplateID {
			if uerr := s.m.UpdateInstanceTemplateTx(tx, inst.ProjectID, inst.ID, tpl.TemplateID, now); uerr != nil {
				return uerr
			}
			inst.TemplateID = tpl.TemplateID
			// 换底稿 = 放弃独立文档（双轨语义）：产物已按新模板编译，文档列若留着旧自定义，
			// 下次重建又会拿旧文档盖掉新模板 —— 与本次「切换」自相矛盾。
			// mode != nil 时上面已处理（ReapplyPreset 的「换模板 + 回跟随」路径同时给两者）。
			if mode == nil && (presentationmodel.IsDocumentMode(inst.RenderMode) || len(inst.OverrideDocument) > 0) {
				if cerr := s.m.ClearInstanceModeTx(tx, inst.ProjectID, inst.ID, now); cerr != nil {
					return cerr
				}
				inst.RenderMode = presentationmodel.RenderModeTemplate
				inst.OverrideDocument = nil
			}
		}
		if logicalPath != "" && inst.URLPath != logicalPath {
			if uerr := s.m.UpdateInstanceURLTx(tx, inst.ProjectID, inst.ID, logicalPath, now); uerr != nil {
				return uerr
			}
			inst.URLPath = logicalPath
		}
		version, verr := s.m.NextArtifactVersionTx(tx, inst.ID)
		if verr != nil {
			return verr
		}
		for i := range results {
			b := &results[i]
			aid, aerr := s.recordArtifactTx(ctx, tx, inst, snapID, b.built, b.lang, version, now)
			if aerr != nil {
				return aerr
			}
			b.artifactID = aid
			if b.lang == defaultLang {
				primaryArtifactID = aid
			}
		}
		if primaryArtifactID == "" && len(results) > 0 {
			primaryArtifactID = results[0].artifactID
		}
		// 依赖记录挂在默认语言产物上（各语言依赖集合相同）。
		if len(results) > 0 {
			deps := results[0].built.Manifest.Dependencies
			return s.persistDependenciesTx(tx, inst.ID, primaryArtifactID, deps, now)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return primaryArtifactID, snapID, nil
}
