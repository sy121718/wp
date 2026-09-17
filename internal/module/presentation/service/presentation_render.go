package presentationservice

// presentation_render.go — 渲染与模板解析（构建、产物生成、路径激活、语言与工程解析）。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"

	"go_wp/pkg/logger"
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

// buildArtifact 编译模板 AST（经 entity resolver）→ 产物落盘（**不激活**）。
//
// 激活由调用方在实例落库成功后单独执行（见 activate）：先激活后落库时，
// 一旦落库失败，线上已渲染出新实体内容却没有任何恢复入口。
//
// targetLangs 是本次批次准备上线的语言集合（SEO-026），逐字透传给 renderHTML 的
// hreflang 判定 —— 调用方必须传它逐语言结案用的那一份，不要在中间重新推导。
func (s *Service) buildArtifact(ctx context.Context, entityType, entityID, urlPath, projectID, lang string,
	targetLangs []string, tpl *contenttemplatecontract.ResolvedTemplate) (built builtArtifact, err error) {
	html, err := s.renderHTML(ctx, entityType, entityID, urlPath, projectID, lang, targetLangs, tpl)
	if err != nil {
		return built, err
	}
	sourceHash := pipeline.SHA256(tpl.Document)
	artifact, err := pipeline.NewArtifact(html, &pipeline.Manifest{
		ManifestSchemaVersion:     1,
		PageDocumentSchemaVersion: 1,
		SourceID:                  entityID,
		SourceType:                pipeline.SourceTypePresentation,
		CanonicalPath:             urlPath,
		SourceHash:                sourceHash,
		BuildInputHash:            sourceHash,
		Lang:                      strings.TrimSpace(lang),
		Dependencies:              presentationDependencies(entityType, entityID, tpl.TemplateID),
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
func (s *Service) renderHTML(ctx context.Context, entityType, entityID, urlPath, projectID, lang string,
	targetLangs []string, tpl *contenttemplatecontract.ResolvedTemplate) (html []byte, err error) {
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
	compileOpts := []builder.CompileOption{
		builder.WithContext(buildCtx),
		builder.WithComponentSet(set),
		builder.WithContentResolver(resolver),
		builder.WithBlockResolver(blockAdapter),
	}
	compileOpts = append(compileOpts, pluginOpts...)
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
	var contentTranslator *i18n.ContentTranslator
	var contentCandidates int
	compileOpts, contentTranslator, contentCandidates = pipeline.AppendContentTranslation(
		compileOpts, ctx, s.project, projectID, lang, page, blockAdapter.ResolveBlockRoot, s.newContentTranslator)
	// 集合源注入（issue #9）：模板里的集合类组件按白名单展开商品等集合数据。
	if s.collection != nil {
		compileOpts = append(compileOpts, builder.WithCollectionResolver(s.collection))
	}
	// 商品数据源（issue #35）：与 page 路径同一注入方式。
	if s.productDS != nil {
		compileOpts = append(compileOpts, builder.WithProductDataSource(s.productDS))
	}
	compileOpts = append(compileOpts, pipeline.AnalyticsCompileOptions(ctx, s.project, projectID)...)
	// 结构槽位（审计 VIS-001）：页眉 / 页脚与模板主体走同一次编译，不再拼字符串。
	// 与手工页面路径同一口径 —— 两条路径各写一份装配逻辑，正是本条目要消除的重复。
	compileOpts = append(compileOpts, structureSlotOptions(page.Settings.Structure)...)
	compiled, err := builder.Compile(page, compileOpts...)
	if err != nil {
		return nil, err
	}
	if contentTranslator != nil {
		pipeline.LogContentTranslationMisses(lang, contentCandidates, contentTranslator.Misses())
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
//     当前无来源模块触发该键，登记用于审计与后续接入）。
func presentationDependencies(entityType, entityID, templateID string) []pipeline.Dependency {
	dep := pipeline.DirectContentKey(entityType, entityID)
	out := []pipeline.Dependency{{Kind: dep.Kind, Key: dep.Key}}
	if strings.TrimSpace(templateID) != "" {
		out = append(out, pipeline.Dependency{
			Kind: pipeline.DepKindContentTemplate,
			Key:  "content_template:" + templateID,
		})
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
func (s *Service) resolveProjectID(ctx context.Context, explicit string) (string, error) {
	if id := strings.TrimSpace(explicit); id != "" {
		if s.project != nil {
			ok, err := s.project.Exists(ctx, id)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", errors.New(presentationenums.ErrProjectNotFound)
			}
		}
		return id, nil
	}
	if s.project == nil {
		return "", errors.New(presentationenums.ErrProjectRequired)
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
