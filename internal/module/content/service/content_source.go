package contentservice

// source="content:{entityType}" → 查 contents 表该类型实体列表 → 展开 data
// 为字段值列表（构建期静态填入，供插件组件集合渲染）。

// 绑定单个内容实体：ResolveString(field) 按字段白名单解析实体的 data 字段
// 为字符串字面量（heading 等组件的 Binding 在构建期静态填入）。

// 本模块是自身实体类型字段白名单的唯一来源；装配期把它们注册进注册表，
// 构建层（内容模板 / 发布实例）据此校验与解析，不再直接依赖本模块的类型判断函数。

// 只读路径：片段端点（anonymous GET）→ SearchPort → model.SearchArticles → contents 表。
// 本文件不判定「已发布」—— contents 表没有状态列，内容的线上可用性由「有没有已上线的
// 详情页产物」决定（presentation 实例的 active 指针），那个判定在搜索片段那一层与
// 发布面的路径解析一起做（见 runtimefragment/search_results.go）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/content/enums"
)

// collectionSourcePrefix 内容实体集合源前缀。
const collectionSourcePrefix = "content:"

// 编译期断言：内容集合源支持按页取数（审计 PERF-019）。
// 没有这行的话，ResolveCollectionPage 被重命名或改签名时不会有任何提示 ——
// 注册表那边只是「探测不到这个能力」然后静默退回取一批再截断。
var _ core.CollectionPager = (*Service)(nil)

// ResolveCollection 实现 core.CollectionResolver：按集合源查实体列表。
// filter 为白名单等值过滤（MVP：仅支持空 filter 或按 entity_type 外的
// 数据字段等值匹配；字段值白名单由调用方组件声明控制）。
func (s *Service) ResolveCollection(ctx context.Context, source string, filter map[string]string) (items []map[string]any, err error) {
	items, _, err = s.resolveCollection(ctx, source, filter, 0, 0)
	return items, err
}

// ResolveCollectionPage 实现可选能力 core.CollectionPager（审计 PERF-019）：按页取数，
// 并给出该过滤条件下的总量。
//
// 构建期不需要它（只取第一屏），所以它是可选能力而不是把参数塞进 CollectionResolver ——
// 那样全部实现与所有测试 fake 都要跟着改，收益为零（见 source.CollectionPager 的注释）。
func (s *Service) ResolveCollectionPage(ctx context.Context, source string, q core.CollectionQuery) (core.CollectionPage, error) {
	items, total, err := s.resolveCollection(ctx, source, q.Filter, q.Offset, q.Limit)
	if err != nil {
		return core.CollectionPage{}, err
	}
	return core.CollectionPage{Items: items, Total: total}, nil
}

// resolveCollection 两个入口的共用实现。
//
// total 的语义：返回的是**满足过滤条件的总数**，不是本页条数。取数不满一页时不必再 COUNT ——
// 后面已经没有了，总量就是 offset + 本页条数；省下的那次查询在列表页每次翻页都会用到。
func (s *Service) resolveCollection(ctx context.Context, source string, filter map[string]string, offset, limit int) (items []map[string]any, total int, err error) {
	entityType, ok := strings.CutPrefix(source, collectionSourcePrefix)
	if !ok || !contentcontract.IsValidType(entityType) {
		return nil, 0, fmt.Errorf("%s: %q（期望 content:{product|article|category}）", contentenums.ErrInvalidType, source)
	}
	// 列投影 + 筛选下推（审计 PERF-008）：此前先 List 取回 100 行**整行**（data 里含正文
	// 全文）再在 Go 里过滤，等于为了渲染几张卡片把正文都读了一遍。
	// 投影字段取集合白名单（自动排除 body / focusKeyword），筛选条件下沉成 SQL。
	fields := contentcontract.CollectionFieldWhitelist(entityType)
	rows, err := s.m.ListForCollection(ctx, entityType, fields, filter, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	items = make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out := map[string]any{}
		if len(r.Fields) > 0 {
			if uerr := json.Unmarshal(r.Fields, &out); uerr != nil {
				return nil, 0, fmt.Errorf("%s: %w", contentenums.ErrDataInvalid, uerr)
			}
		}
		// 复查（防御，不是主路径）：筛选已下推到 SQL，这里再比一次是为了让
		// 「SQL 表达得对不对」永远不改变最终结果 —— 一旦下推的条件与内存语义有偏差，
		// 表现是少渲染几张卡片（复查会拦下），而不是渲染出不该出现的条目。
		if !matchFilter(out, filter) {
			continue
		}
		// 注入系统字段（id/slug/revision 供模板展示）。
		out["id"], out["slug"], out["revision"] = r.ID, r.Slug, r.Revision
		// 媒体字段归一到完整链接：这条路径**不经过 toResp**（集合投影走的是 SQL 侧
		// jsonb_build_object，比整行取数省一大截），所以读出口的归一必须在这里也做一次 ——
		// 否则集合卡（cardstack 的文章 / 商品卡）会渲染出 /storage/... 相对地址，
		// 而同一条数据的详情页却是完整链接。
		normalizeMediaFields(entityType, out)
		items = append(items, out)
	}
	// 取数不满一页说明已经到底：总量就是 offset 加上本页条数，不必再 COUNT 一次。
	// 满页时才需要真去数 —— 列表页每翻一页都会走这里，省下的是每次翻页一次聚合查询。
	if limit > 0 && len(rows) < limit {
		total = offset + len(items)
		return items, total, nil
	}
	n, cerr := s.m.CountForCollection(ctx, entityType, filter)
	if cerr != nil {
		return nil, 0, cerr
	}
	return items, int(n), nil
}

// matchFilter 等值过滤（全部键匹配才保留）。
func matchFilter(data map[string]any, filter map[string]string) bool {
	for k, want := range filter {
		v, ok := data[k]
		if !ok || scalarString(v) != want {
			return false
		}
	}
	return true
}

// CollectionSchemas 实现 core.CollectionSchemaProvider：暴露内容集合源与字段白名单。
// 内置组件（如 cardstack）用它在构建期校验字段映射，工作台用它渲染字段下拉。
func (s *Service) CollectionSchemas(_ context.Context) ([]core.CollectionSchema, error) {
	types := contentcontract.EntityTypes()
	out := make([]core.CollectionSchema, 0, len(types))
	for _, t := range types {
		// 展示名给 (key, 中文兜底) 两份：service 拿不到请求语言（构建期根本没有请求），
		// 取词在出口 handler —— 见 contentenums.EntityTypeLabel 与 CollectionSchema.LabelKey。
		key, fallback := contentenums.EntityTypeLabel(t)
		out = append(out, core.CollectionSchema{
			Source:   collectionSourcePrefix + t,
			Label:    fallback,
			LabelKey: key,
			// 集合项字段用**集合白名单**：下拉里不该出现正文与主关键词，
			// 它们既不在查询投影里、也不会被渲染。三处（下拉 / 构建期校验 / SQL 投影）
			// 共用这一个定义，才不会出现「选得到、构建出来是空」的静默失败。
			Fields: contentcontract.CollectionFieldWhitelist(t),
		})
	}
	return out, nil
}

// 编译期断言：Service 实现集合解析与集合元数据两个契约。
var (
	_ core.CollectionResolver       = (*Service)(nil)
	_ core.CollectionSchemaProvider = (*Service)(nil)
)

// ResolverFor 返回绑定单个实体的内容解析器。
// 构建期注入：presentation 模块派生 DocumentSnapshot 时按 entityType+entityID
// 取实体，把 Binding 字段解析为字面量（文档已解析，后续编译零依赖实体）。
func (s *Service) ResolverFor(ctx context.Context, entityType, entityID string) (r core.ContentResolver, err error) {
	if !contentcontract.IsValidType(entityType) {
		return nil, errors.New(contentenums.ErrInvalidType)
	}
	e, gerr := s.m.Get(ctx, entityID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, errors.New(contentenums.ErrNotFound)
		}
		return nil, gerr
	}
	if e.EntityType != entityType {
		return nil, fmt.Errorf("实体 %s 类型 %s 与请求 %s 不符", entityID, e.EntityType, entityType)
	}
	var data map[string]any
	if err = json.Unmarshal(e.Data, &data); err != nil {
		return nil, fmt.Errorf("%s: %w", contentenums.ErrDataInvalid, err)
	}
	// 多语言（审计 I18N-006）：字段值按当前构建语言取译文。
	//
	// 语言从 ctx 取（core.BuildLang）—— 与商品域的实体解析同一约定：
	// 签名里再传一个 lang 就有「两处可能不一致」的空间，而它只在英文站点上才暴露。
	// 没有译文时逐字节回退原文（TranslateContent 的语义），英文站点缺译文是常态。
	lang := core.BuildLang(ctx)
	data = s.translateData(ctx, lang, entityType, data)
	mergeSEOFields(entityType, data)
	return &entityResolver{entityType: entityType, data: data}, nil
}

// mergeSEOFields 把已合并的 SEO 字段指向正文字段（2026-09-30）。
//
// 文章标题即 <title>、摘要即 meta description，编辑页不再有单独的 SEO 输入框。
// 归一放在**翻译之后**：seoTitle / seoDescription 直接取已翻译的 title / excerpt，
// 历史数据里遗留的旧 SEO 值因此也漏不进发布产物（否则一篇没重新保存过的老文章，
// 线上 <title> 与编辑页看到的标题会不是同一个）。字段白名单仍保留这两个字段
// （旧文档里的 Binding 可能绑着它们），所以这里是「值从哪来」的唯一收口。
func mergeSEOFields(entityType string, data map[string]any) {
	if entityType != "article" || data == nil {
		return
	}
	if v, ok := data["title"]; ok {
		data["seoTitle"] = v
	}
	if v, ok := data["excerpt"]; ok {
		data["seoDescription"] = v
	}
}

// entityResolver 绑定单实体的字段解析器。
type entityResolver struct {
	entityType string
	data       map[string]any
}

// ResolveString 按字段白名单解析字段值为字符串（不存在返回空串）。
// field 形如 "product.name"（entityType.field，与 heading 组件 fieldPathRe
// 的两段格式一致）；拆前缀校验类型匹配 + 字段名白名单（不变量 4）。
func (r *entityResolver) ResolveString(field string) (string, error) {
	parts := strings.SplitN(field, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("%s: %q（期望 entityType.field）", contentenums.ErrInvalidField, field)
	}
	entityType, fieldName := parts[0], parts[1]
	if entityType != r.entityType {
		return "", fmt.Errorf("绑定字段 %q 类型 %q 与当前实体 %q 不符", field, entityType, r.entityType)
	}
	if !contentcontract.IsValidField(entityType, fieldName) {
		return "", fmt.Errorf("%s: %q", contentenums.ErrInvalidField, fieldName)
	}
	v, ok := r.data[fieldName]
	if !ok || v == nil {
		return "", nil
	}
	return scalarString(v), nil
}

// scalarString 字段值归一为字符串：string 原样；数值转字符串；
// 数组取首元素（如 images 的封面）；其他 JSON 序列化。
func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []any:
		if len(x) > 0 {
			return scalarString(x[0])
		}
		return ""
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// 编译期断言：entityResolver 实现 core.ContentResolver。
var _ core.ContentResolver = (*entityResolver)(nil)

// entityFieldSource 单个内容实体类型的字段来源适配器。
type entityFieldSource struct {
	svc        *Service
	entityType string
}

// EntityType 实现 core.EntityFieldSource。
func (e *entityFieldSource) EntityType() string { return e.entityType }

// FieldWhitelist 实现 core.EntityFieldSource（白名单仍取自本模块契约的唯一来源）。
func (e *entityFieldSource) FieldWhitelist() []string {
	return contentcontract.FieldWhitelist(e.entityType)
}

// ResolverFor 实现 core.EntityFieldSource。
func (e *entityFieldSource) ResolverFor(ctx context.Context, entityID string) (core.ContentResolver, error) {
	return e.svc.ResolverFor(ctx, e.entityType, entityID)
}

// RegisterEntityTypes 把本模块全部实体类型注册进注册表（装配期调用）。
//
// 注册表为 nil 视为装配缺陷（fail-closed）：静默跳过会让构建层到运行期
// 才发现「所有类型都非法」，比装配期直接报错更难排查。
func (s *Service) RegisterEntityTypes(reg core.EntitySourceRegistry) error {
	if reg == nil {
		return errors.New("实体类型注册表为空")
	}
	for _, t := range contentcontract.EntityTypes() {
		if err := reg.Register(&entityFieldSource{svc: s, entityType: t}); err != nil {
			return err
		}
	}
	return nil
}

// 编译期断言：适配器实现 core.EntityFieldSource。
var _ core.EntityFieldSource = (*entityFieldSource)(nil)

// 编译期断言：本 service 提供访问面检索需要的只读能力。
var _ contentcontract.SearchPort = (*Service)(nil)

// SearchArticles 按关键词检索文章（实现 contentcontract.SearchPort）。
//
// 取词用**原文**而不是译文：检索命中的是内容本身的字，拿译文去命中会让
// 「搜中文标题、原文是英文」这类情况永远搜不到（多语言检索属后续票的范围）。
func (s *Service) SearchArticles(ctx context.Context, keyword string, limit int) (hits []*contentcontract.ArticleSearchHit, err error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	rows, err := s.m.SearchArticles(ctx, contentcontract.EntityTypeArticle, keyword, limit)
	if err != nil {
		return nil, err
	}
	hits = make([]*contentcontract.ArticleSearchHit, 0, len(rows))
	for _, row := range rows {
		if row == nil || strings.TrimSpace(row.ID) == "" {
			continue
		}
		title, excerpt := articleSearchText(row.Data)
		hits = append(hits, &contentcontract.ArticleSearchHit{
			ID: row.ID, Slug: row.Slug, Title: title, Excerpt: excerpt,
		})
	}
	return hits, nil
}

// articleSearchText 从 contents.data（JSONB）取标题与摘要。
//
// 解析失败或字段缺失时返回空串而不是报错：结果条目少一行标题，比整页搜索 500 好得多；
// 而 data 的形状在保存期已由字段白名单校验过，这里的容错是给历史数据兜底。
func articleSearchText(raw json.RawMessage) (title, excerpt string) {
	if len(raw) == 0 {
		return "", ""
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", ""
	}
	title, _ = fields["title"].(string)
	excerpt, _ = fields["excerpt"].(string)
	return strings.TrimSpace(title), strings.TrimSpace(excerpt)
}
