package pageservice

// 装配编译（方案 C，021_blocks.sql）：内核 CompileFn 注入。
// 页面文档 settings.structure 快照了主题的页眉/页脚块绑定，
// 构建时在此拉取块文档分别编译，HTML/CSS 拼接进页面产物——
// 访问面保持纯静态（无运行时拼接），块内容变更通过 stale 传播触发重建。
// 页面文档内的 core.globalref 节点经 BlockResolver 同样内联展开。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
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
	projectID, currentPath := s.pageContextOf(ctx, in.PageID)
	// 语言来自构建输入（内核按 PageRecord.Lang 注入，见 pipeline.BuildInput）；
	// 访问路径仍取页面记录的逻辑路径，前缀在 compileDocument 内单点计算。
	html, err := s.compileDocument(ctx, page, projectID, currentPath, in.Lang)
	if err != nil {
		if errors.Is(err, errCompileFailed) {
			logger.Scene("build").Error(err, "页面编译失败")
		}
		return nil, err
	}
	return html, nil
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
func (s *Service) compileDocument(ctx context.Context, page *builder.Page, projectID, currentPath, lang string) ([]byte, error) {
	// 组件模板 Set：无插件走 embed 单例；有插件走 CompositeSet
	//（内置 embed + 插件命名空间合并，docs/06 §7）。
	asm := s.enabledAssembly(ctx)
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		return nil, err
	}
	if asm != nil && len(asm.PluginFS) > 0 {
		set, err = templates.NewCompositeSet(asm.PluginFS)
		if err != nil {
			return nil, err
		}
	}
	resolver := blockResolverAdapter{s: s, ctx: ctx, cache: make(map[string][]*core.Node)}
	// 构建语言与取词函数：WithLanguage 决定 RenderContext.Lang；
	// WithTranslator 注入「构建开始时刻冻结」的词条快照——构建中途刷新 i18n 缓存
	// 不影响本次产物字节（确定性构建不变量，docs/06-D §2.3/§12）。
	opts := []builder.CompileOption{
		builder.WithContext(ctx), builder.WithBlockResolver(resolver), builder.WithComponentSet(set),
		builder.WithLanguage(lang), builder.WithTranslator(i18n.Snapshot(lang)),
	}
	if asm != nil {
		opts = append(opts, builder.WithPluginResolver(plugincontract.AssemblyResolver(asm)))
	}
	if s.content != nil {
		opts = append(opts, builder.WithCollectionResolver(s.content))
	}
	// 导航注入：core.nav 绑定菜单位置（header/footer）时构建期解析为静态菜单项。
	// 缓存按「工程 + 位置」单次编译内复用（同一页面多个导航节点只查一次库）。
	if s.navigation != nil {
		opts = append(opts, builder.WithNavigationResolver(navigationResolverAdapter{
			svc: s.navigation, ctx: ctx, lang: lang, cache: map[string][]core.NavigationItem{},
		}))
	}
	// 响应式图片：媒体变体存在时输出 srcset/sizes（构建期探测，访客零查询）。
	if s.media != nil {
		opts = append(opts, builder.WithAssetProbe(func(url string) []int {
			return s.media.ProbeImageVariants(ctx, url)
		}))
	}
	// 工程 ID：页面文档不携带，由调用方按页面记录注入（导航等站点级资源取数上下文）。
	// 当前项高亮用「实际访问路径」（多语言开启前缀时与导航项 URL 同带前缀）。
	opts = append(opts, builder.WithProjectID(projectID), builder.WithCurrentPath(highlightPath(lang, currentPath)))
	// 主题快照注入：settings.theme（保存时合入的 ThemeSettings 快照）→ 编译进产物。
	if page.Settings.Theme != nil {
		opts = append(opts, builder.WithThemeSettings(page.Settings.Theme))
	}
	compiled, err := builder.Compile(page, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCompileFailed, err)
	}
	// 页眉/页脚块内联（settings.structure 绑定快照）：与预览/正式构建同源。
	headerHTML, headerCSS := s.compileBlockFragment(ctx, page.Settings.Structure.HeaderBlockID)
	footerHTML, footerCSS := s.compileBlockFragment(ctx, page.Settings.Structure.FooterBlockID)
	// 页眉在主体前、页脚在主体后；三段 CSS 为独立规则集，顺序拼接。
	compiled.HTML = headerHTML + compiled.HTML + footerHTML
	compiled.CSS = headerCSS + compiled.CSS + footerCSS
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
}

// parseStructureBindings 从页面文档读取全局块绑定快照（无该键时返回零值）。
// 供主题合并（mergeActiveTheme）读取页面级页眉/页脚覆盖使用。
func parseStructureBindings(docJSON []byte) (b builder.StructureBindings, err error) {
	var page struct {
		Settings struct {
			Structure builder.StructureBindings `json:"structure"`
		} `json:"settings"`
	}
	if err = json.Unmarshal(docJSON, &page); err != nil {
		return b, err
	}
	return page.Settings.Structure, nil
}

// blockRootResolverAdapter 适配 block 契约为 builder 的 BlockResolver
// （core.globalref 构建期展开引用块内容）。
// cache 为单次编译内块解析缓存：同一块被引用多次时只查一次库。
type blockResolverAdapter struct {
	s     *Service
	ctx   context.Context
	cache map[string][]*core.Node
}

// ResolveBlockRoot 按块 ID 返回块文档 root 节点。
// 防御（docs/02-D §5/§9）：reuse_mode=template 的块是「一次性复制」语义，
// 不允许经 core.globalref 引用展开——正常流程下副本已在插入时并入页面文档，
// 此处命中说明引用被绕过编辑器写入，构建期即报错暴露而非静默按引用渲染。
func (a blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	if a.cache != nil {
		if nodes, ok := a.cache[blockID]; ok {
			return nodes, nil
		}
	}
	block, err := a.s.blocks.Detail(a.ctx, &blockcontract.DetailReq{ID: blockID})
	if err != nil || block == nil {
		return nil, fmt.Errorf("全局块 %s 不可用", blockID)
	}
	if block.ReuseMode == "template" {
		return nil, fmt.Errorf("全局块 %s 为一次性复制片段，不能被引用展开", blockID)
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return nil, err
	}
	if a.cache != nil {
		a.cache[blockID] = page.Root
	}
	return page.Root, nil
}

// enabledAssembly 启用插件的编译装配素材（无插件契约或查询失败返回 nil）。
// 构建路径为后台任务（无请求 ctx），此处用 context.Background。
func (s *Service) enabledAssembly(ctx context.Context) *plugincontract.Assembly {
	if s.plugins == nil {
		return nil
	}
	asm, err := s.plugins.EnabledAssembly(ctx)
	if err != nil {
		return nil
	}
	return asm
}
