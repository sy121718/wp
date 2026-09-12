package pageservice

import (
	"context"
	"errors"

	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	plugincontract "go_wp/internal/module/plugin/contract"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/pipeline"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/pkg/i18n"

	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
)

var _ pagecontract.PageService = (*Service)(nil)

// Service Page 草稿、修订与发布业务服务。
//
// 发布链路持有 pipeline 内核（内存态可由数据库重建）与相邻模块契约，
// 不直接依赖 GORM 之外的模块实现。
type Service struct {
	model     *pagemodel.Model
	project   projectcontract.ProjectService
	artifacts artifactcontract.ArtifactService
	routes    pubcontract.PublicationService
	blocks    blockcontract.BlockService
	plugins   plugincontract.PluginService
	content   core.CollectionResolver
	// productDS 商品构建期数据源（issue #35）：页面里的商品列表组件直连它（受限接口），
	// 未注入时组件回退按名路由。可选依赖不进构造参数，与其它端口同模式。
	productDS productcontract.ProductDataSource
	// navigation 公开站点导航契约：core.nav 绑定菜单位置时构建期解析菜单项。
	navigation navigationcontract.NavigationService
	// media 媒体契约：构建期探测图片变体，输出响应式 srcset（访客零查询）。
	media mediacontract.MediaService
	// contentStore 内容译文读取端口（多语言 P5b）：为 nil 时用 pkg/i18n 默认存储
	// （sys_translation 表 + 默认数据库）。测试经 SetContentTranslationStore 注入
	// 隔离 schema 的存储，用于验证「块内文本进候选集合 + 每页每语言一次查库」。
	contentStore i18n.ContentStore

	publisher *pipeline.Publisher
	store     *pipeline.LocalStore
	// publication 访问面激活存储（active 目录符号链接）：页面删除时必须按路径
	// 解除激活，否则「DB 路由已清、符号链接还在」会让已删内容继续可访问。
	publication *pipeline.LocalPublicationStore
}

// NewService 创建 Page 服务；同时初始化本地产物根（GO_WP_ARTIFACT_ROOT 可覆盖，
// 默认与访问面一致，经 pipeline.DefaultArtifactRoot/ActiveRoot 单源取值）。
// 构建注入装配感知编译器：页眉/页脚块内联（方案 C）+ 插件组件（docs/06）
// + 集合内容解析（docs/06 §9）。
func NewService(model *pagemodel.Model, artifacts artifactcontract.ArtifactService,
	routes pubcontract.PublicationService, project projectcontract.ProjectService,
	blocks blockcontract.BlockService, plugins plugincontract.PluginService,
	content core.CollectionResolver, navigation navigationcontract.NavigationService,
	media mediacontract.MediaService) *Service {
	store := &pipeline.LocalStore{Root: pipeline.DefaultArtifactRoot()}
	publication := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}
	s := &Service{
		model:       model,
		artifacts:   artifacts,
		routes:      routes,
		project:     project,
		blocks:      blocks,
		plugins:     plugins,
		content:     content,
		navigation:  navigation,
		media:       media,
		store:       store,
		publication: publication,
	}
	// 依赖提供者：把文案词条资源版本号写进 Manifest.dependencies
	// （DependencyKind=i18n，改文案触发重建，docs/06-D §10.4）。
	s.publisher = pipeline.NewPublisher(store, publication, pipeline.WithDependencies(s.buildDependencies))
	s.publisher.SetCompile(s.assembleCompile)
	return s
}

// SetContentTranslationStore 注入内容译文读取端口（测试用；生产走 pkg/i18n 默认存储）。
func (s *Service) SetContentTranslationStore(store i18n.ContentStore) {
	s.contentStore = store
}

// newContentTranslator 构造本次编译的内容译文取词器（一次批量查询 + 内存索引）。
//
// 注入端口优先（测试），否则用 pkg/i18n 默认存储（sys_translation）。
// 取词语义与缓存行为完全由 pkg/i18n 决定，本层不做二次缓存（docs/06-D §7.7）。
func (s *Service) newContentTranslator(ctx context.Context, lang string, hashes []string) *i18n.ContentTranslator {
	if s != nil && s.contentStore != nil {
		return i18n.NewContentTranslatorWith(ctx, s.contentStore, lang, hashes)
	}
	return i18n.NewContentTranslator(ctx, lang, hashes)
}

// getExistingPage 查询未删除页面，统一映射未找到错误。
func (s *Service) getExistingPage(ctx context.Context, id string) (page *pagemodel.PageEntity, err error) {
	page, err = s.model.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	return page, nil
}

// SetProductDataSource 注入商品构建期数据源（issue #35，装配期调用）。
func (s *Service) SetProductDataSource(ds productcontract.ProductDataSource) { s.productDS = ds }
