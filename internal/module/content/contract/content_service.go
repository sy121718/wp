// Package contentcontract content 模块对外契约（0-A2）。
package contentcontract

import (
	"context"
	"sort"

	"go_wp/internal/builder/source"
	"go_wp/internal/module/content/dto"
)

// 内容实体字段白名单（docs/02-domain.md §2.3，不变量 4 的唯一入口）。
// 键 = entity_type，值 = 允许的字段路径（ContentResolver.ResolveString 与
// contenttemplate 的 Binding 校验共用同一白名单，禁止两处各维护一份）。
var fieldWhitelist = map[string][]string{
	// focusKeyword（主关键词）是编辑期 SEO 评分器的输入之一：关键词密度、关键词位置、
	// H1 是否含关键词这几项检查都以它为基准，缺了它这些项恒判「未设置主关键词」。
	// 它不参与构建产物（不进页面字节），只是给评分器与编辑者用的一句话主题。
	"article": {"title", "body", "excerpt", "featuredImage", "seoTitle", "seoDescription", "focusKeyword"},
}

// translatableFields 各内容类型参与内容翻译（sys_translation）的字段（审计 I18N-006）。
//
// 与商品域同形：只有**作者填写的文本**进译文表。两类字段刻意不列：
//
//   - featuredImage：图片地址。翻了会指向不存在的文件 —— 产物里它要用来发请求。
//   - focusKeyword：编辑期 SEO 评分器的输入，不进构建产物（见上方白名单注释）。
//     它虽然也是作者填的文本，但翻译一个不输出的值没有意义，
//     反而会让「这个词在英文版里评分为何不对」变得难以解释。
//
// 语境命名与商品域一致（docs/06-D §7.5）：article.title / article.body …。
var translatableFields = map[string]map[string]bool{
	"article": {
		"title":   true,
		"body":    true,
		"excerpt": true,
		// seoTitle / seoDescription 不再单列（2026-09-30 字段合并）：两者的值
		// 分别是 title / excerpt 的别名（读侧归一，见 service/content_resolver.go），
		// 单列出来只会得到一个「填了也不生效」的翻译输入框。
	},
}

// richTextFields 富文本字段（译文与原文一样要过 HTML 白名单清洗）。
//
// 只有 body 是富文本：其余可翻译字段都是纯文本，过了清洗反而会把 < > 这类
// 正常字符转义掉（标题里写「A < B」是合法的）。
var richTextFields = map[string]bool{"body": true}

// IsRichTextField 字段是否为富文本（决定译文是否需要 HTML 清洗）。
func IsRichTextField(_, field string) bool {
	return richTextFields[field]
}

// IsTranslatableField 字段是否参与内容翻译。
func IsTranslatableField(entityType, field string) bool {
	return translatableFields[entityType][field]
}

// TranslatableFields 返回某类型的可翻译字段（字典序，只读拷贝）。
func TranslatableFields(entityType string) []string {
	set := translatableFields[entityType]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// EntityTypes 全部支持的内容类型（字典序）。
func EntityTypes() []string {
	out := make([]string, 0, len(fieldWhitelist))
	for t := range fieldWhitelist {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// IsValidType 内容类型是否合法。
func IsValidType(t string) bool {
	_, ok := fieldWhitelist[t]
	return ok
}

// IsValidField 字段是否在该类型白名单内（不变量 4 校验）。
func IsValidField(entityType, field string) bool {
	for _, f := range fieldWhitelist[entityType] {
		if f == field {
			return true
		}
	}
	return false
}

// FieldWhitelist 返回该类型的字段白名单（只读拷贝，防调用方篡改）。
func FieldWhitelist(entityType string) []string {
	fields := fieldWhitelist[entityType]
	out := make([]string, len(fields))
	copy(out, fields)
	return out
}

// collectionExcludedFields 不进集合查询投影的字段（审计 PERF-008）。
//
// 集合项渲染的是卡片：标题、摘要、封面、SEO 文案。两类字段不该跟着走这一趟：
//
//   - body：正文全文。集合查询若把每篇文章的正文都拉回来再丢掉，读的是最大的一列
//     （contents.data 里 body 占绝对多数），而集合渲染一个字节都不用。
//   - focusKeyword：编辑期 SEO 评分器的输入，本就不进构建产物（见上方 fieldWhitelist 注释），
//     它连单实体渲染都不该出现，更不必出现在集合项里。
//
// 排除清单放在白名单旁边而不是散在查询里：集合项下拉、构建期字段校验、SQL 投影
// 三处必须用同一份定义，否则模板能选到字段、构建时却是空的（静默为空的典型成因）。
var collectionExcludedFields = map[string]bool{
	"body":         true,
	"focusKeyword": true,
}

// CollectionFieldWhitelist 集合项可用字段 = 数据源白名单减去不进集合投影的字段。
//
// 与 FieldWhitelist 的关系是「子集」：单实体绑定仍可用 body（详情页要渲染正文），
// 集合项绑定不能。两者的差集由 collectionExcludedFields 单处定义。
func CollectionFieldWhitelist(entityType string) []string {
	fields := fieldWhitelist[entityType]
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if collectionExcludedFields[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// IsCollectionField 字段是否可用于集合项绑定。
func IsCollectionField(entityType, field string) bool {
	if !IsValidField(entityType, field) {
		return false
	}
	return !collectionExcludedFields[field]
}

// ContentService CMS 内容管理契约 + 构建期内容解析器工厂。
// ContentService 内容完整契约；issue #35 起嵌入 ContentDataSource（构建期只给受限的一半）。
type ContentService interface {
	// ContentDataSource 构建期数据源（只读）。
	ContentDataSource

	// Create 新建内容实体（revision=1）。
	Create(ctx context.Context, req *contentdto.CreateReq) (res *contentdto.ContentResp, err error)
	// Update 更新内容（revision 递增）。
	Update(ctx context.Context, req *contentdto.UpdateReq) (res *contentdto.ContentResp, err error)
	// Get 按 ID 查询。
	Get(ctx context.Context, req *contentdto.GetReq) (res *contentdto.ContentResp, err error)
	// List 按类型和标题/slug关键词分页列表；Count 使用同一过滤条件。
	List(ctx context.Context, req *contentdto.ListReq) (list []*contentdto.ContentResp, err error)
	// Count 列表总数：与 List **同一份过滤条件**（entityType + keyword），供分页算总页数。
	//
	// 形状与 product 域的 CountProducts 一致：收同一个 ListReq（只认它的过滤维度，
	// Limit / Offset 在这里无意义），返回 (int64, error)。
	//
	// 与 CountForCollection **不是一回事**：那个是集合渲染路径的计数（带 data 字段的
	// 等值过滤，供组件集合翻页），数的是「满足筛选的集合项」；本方法数的是
	// 「这个实体类型下有多少条」，服务的是后台列表页的分页条。
	Count(ctx context.Context, req *contentdto.ListReq) (n int64, err error)
	// Delete 删除实体。
	Delete(ctx context.Context, req *contentdto.DeleteReq) (err error)
	// ResolverFor 返回绑定单个实体的内容解析器（构建期注入：presentation
	// 模块构建 DocumentSnapshot 时按 entityType+entityID 取实体解析 Binding）。
	ResolverFor(ctx context.Context, entityType, entityID string) (r source.ContentResolver, err error)
	// RegisterEntityTypes 把本模块支持的实体类型注册进实体类型注册表（装配期调用）。
	// 注册后，构建层（内容模板 / 发布实例）不再直接依赖本模块的类型判断函数。
	RegisterEntityTypes(reg source.EntitySourceRegistry) error
	// SetDependencyInvalidator 注入依赖失效扇出端口（编排层装配，可空）。
	// 内容实体变更后由本模块推导依赖源键（实体自身 + 所属集合）并交给端口，
	// 端口负责按依赖表反查受影响产物（PIPE-3）。
	SetDependencyInvalidator(inv DependencyInvalidator)
}

// DependencyInvalidator 依赖失效扇出入口（由编排层注入实现，通常是 pipeline.Fanout）。
//
// 契约刻意保持极简（kind + key）：content 模块只负责声明「我是哪个依赖源」，
// 具体反查与重建由发布来源模块完成，避免 content 反向依赖 page/presentation。
// 实现必须容错——失效失败不能影响已经成功的内容写入。
type DependencyInvalidator interface {
	// Invalidate 标记依赖源 (kind,key) 变更；kind/key 语义见
	// docs/03-pipeline.md §8.1 与 pipeline.DepKind* 常量。
	Invalidate(ctx context.Context, kind, key string)
}
