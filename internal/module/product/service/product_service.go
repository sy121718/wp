// Package productservice 商品模块业务实现（issue #5 / T3a）。
//
// 边界：本模块只管商品与变体。库存、采购、订单、客户各由自己的模块负责；
// 这里不反向依赖它们（跨模块只走 contract）。
//
// 两条已定语义在本文件落地：
//  1. 商品主体不存价格 —— 价格全在变体上，商品侧的「价格区间」是从变体派生的只读结果；
//  2. 商品级字段是「新增变体时的默认值模板」，仅新增路径逐字段判空后填充，
//     编辑路径一个字都不动（含调用方主动清空字段）；空值以 NULL 判定，0 与 false 视为已填。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	inventoryservice "go_wp/internal/module/product/inventory/service"
	productmodel "go_wp/internal/module/product/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// Service 商品模块业务实现。
//
// 只持有本模块 model 与 project 契约；不持有 *gorm.DB。
type Service struct {
	m       *productmodel.Model
	project projectcontract.ProjectService
	// inv 库存 model（issue #32：商品与库存合并为同一模块后，商品用例直接持有库存 model，
	// 读真源汇总做**查询期投影**，不再经跨模块端口、也不再有商品侧缓存副本）。
	// 未注入时库存投影为 0（纯商品单测路径）；注入与否都不影响任何可用量判断。
	inv *inventorymodel.Model
	// contentStore 内容译文读取端口（装配期注入，可空）。
	// 构建期商品可翻译字段（name/subtitle/description）按构建语言取译文；
	// 未注入 / 语言为空 / 查询失败一律回退原文（兜底铁律，绝不报错）。
	contentStore i18n.ContentStore
	// invSvc 库存用例（issue #32：商品与库存合并为同一模块后直接持有对方 service，
	// 不再经跨模块端口 —— 归属仓解析与库存记录生成本就是库存模块的用例）。
	// 未注入时变体创建不生成库存记录、SKU 编码不带仓短码前缀（纯商品单测路径）。
	invSvc *inventoryservice.Service
	// availability 库存真源可用量端口（issue #20，由 inventory 模块实现）。
	// 捆绑品的数量上限与整单下限都受可用量约束，且只看真源、绝不读展示缓存；
	// 未注入时整单校验 fail-closed（返回 ErrBundleStockUnavailable），不按「无限制」放行。
	availability productcontract.VariantAvailabilityPort
	// changes 主数据变更记录端口（issue #19，由 masterdata 模块实现）。
	// 商品 / 变体的关键字段变更经它留痕（append-only）；
	// 未注入时静默跳过（纯商品单测路径），生产装配恒注入。
	changes masterdatacontract.MasterDataService
}

// NewService 构造。
// SetInventory 注入库存 model（issue #32，装配期调用；可选依赖不进构造参数）。
func (s *Service) SetInventory(m *inventorymodel.Model) { s.inv = m }

// NewService 构造商品用例。
func NewService(m *productmodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

// SetContentStore 注入内容译文读取端口（装配期调用，与其它模块的
// SetDependencyInvalidator / SetSourceResolver 同模式：可选依赖不进构造参数）。
// 传入 nil 表示不翻译（构建期商品字段输出原文）。
func (s *Service) SetContentStore(store i18n.ContentStore) {
	s.contentStore = store
}

// SetVariantStock 注入变体归属仓与库存记录端口（issue #15，装配期调用）。
//
// 端口定义在本模块契约里、实现在 inventory 模块：商品模块只知道
// 「解析归属仓」与「在归属仓生成库存记录」两件事，不认识仓库表结构。
func (s *Service) SetInventoryService(svc *inventoryservice.Service) {
	s.invSvc = svc
}

// SetAvailabilityPort 注入库存真源可用量端口（issue #20，装配期调用）。
//
// 与 SetVariantStock 同一模式：端口定义在本模块契约、实现在 inventory 模块，
// 由顶层装配注入（依赖方向 inventory → product）。
func (s *Service) SetAvailabilityPort(port productcontract.VariantAvailabilityPort) {
	s.availability = port
}

// SetMasterDataChanges 注入主数据变更记录端口（issue #19，装配期调用）。
//
// 与 SetVariantStock 同一模式：可选依赖不进构造参数，装配期由顶层注入
// masterdata 模块的实现（依赖方向 product → masterdata）。
func (s *Service) SetMasterDataChanges(port masterdatacontract.MasterDataService) {
	s.changes = port
}

// 编译期契约断言。
var (
	_ productcontract.ProductService = (*Service)(nil)
	// 库存缓存端口（issue #16）：库存模块经它把真源汇总写进本模块的展示缓存。
	// 成本价写回端口（issue #18）：库存模块经它把入库单价写进 product_variants.cost_price。
	_ productcontract.VariantCostPort = (*Service)(nil)
)

// Create 新建商品，并在同一事务内生成它的第一个变体。
//
// 商品恒有至少一个变体（默认多变体模型，不做「简单/可变商品」二分）；
// 单变体商品在前台按普通商品呈现，不显示规格选择器。
func (s *Service) Create(ctx context.Context, req *productdto.CreateReq) (res *productdto.ProductResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New(productenums.ErrNameRequired)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	slug := normalizeSlug(req.Slug)
	if slug == "" {
		slug = deriveSlug(req.Name)
	}
	now := time.Now().UTC()
	if slug == "" {
		slug = "p-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	}
	if taken, serr := s.m.SlugExists(ctx, projectID, slug, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, errors.New(productenums.ErrSlugTaken)
	}

	// 属性引用先校验再落库（同一工程内 + 必须存在，见 resolveAttributeIDs）。
	attributeIDs, err := s.resolveAttributeIDs(ctx, projectID, req.AttributeIDs)
	if err != nil {
		return nil, err
	}
	// 分类与品牌（issue #10）：引用先校验再落库（同一工程内 + 必须存在），
	// 「主分类必属于附属分类」的不变量由 applyCategoryRefs 维持。
	categoryIDs, primaryCategoryID, err := s.applyCategoryRefs(ctx, projectID, req.CategoryIDs, nil, &req.PrimaryCategoryID, nil)
	if err != nil {
		return nil, err
	}
	brandID, err := s.resolveBrandID(ctx, projectID, req.BrandID)
	if err != nil {
		return nil, err
	}
	// 标签引用（issue #11）：手工标签才可手工挂载；自动标签由规则重算维护。
	tagIDs, err := s.resolveTagIDs(ctx, projectID, req.TagIDs)
	if err != nil {
		return nil, err
	}
	// 捆绑配置形状规范化（issue #20）：空 / [] → 空配置对象；形状不对即拒绝。
	// 语义校验（必选 / 上下限 / 整单件数）走 SetBundleConfig 专用入口，这里只保证形状合法。
	bundleItems, berr := normalizeBundleItems(req.BundleItems)
	if berr != nil {
		return nil, berr
	}
	e := &productmodel.ProductEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Name: strings.TrimSpace(req.Name), Subtitle: req.Subtitle,
		Description: orJSON(req.Description, "{}"), Slug: slug,
		Status: productenums.StatusDraft,
		Unit:   req.Unit, Weight: req.Weight,
		SEOTitle: req.SEOTitle, SEODescription: req.SEODescription,
		Images: orJSONList(req.Images), ImageAlts: orJSONList(req.ImageAlts),
		AttributeIDs:      orJSONList(attributeIDs),
		CategoryIDs:       orJSONList(categoryIDs),
		PrimaryCategoryID: primaryCategoryID,
		BrandID:           brandID,
		TagIDs:            orIDList(tagIDs), RelatedIDs: orIDList(req.RelatedIDs),
		BundleItems: bundleItems,

		DefaultImage: req.DefaultImage,
		DefaultPrice: req.DefaultPrice,
		Metadata:     orJSON(req.Metadata, "{}"),
		CreatedAt:    now, UpdatedAt: now,
	}
	// 归属仓（issue #15）先解析再落库：req.WarehouseID 为空即兜底该工程的默认仓，
	// 短码同时决定首个变体的 SKU 编码前缀；解析失败（如工程内没有默认仓）
	// 时整个创建失败，不留「有商品没库存记录」的半截状态。
	ref, err := s.resolveWarehouseRef(ctx, projectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	// 首个变体：由商品级默认值填充（新增路径）。
	v := s.newVariantFromDefaults(ctx, e, nil, refCode(ref), nil)
	if err = s.m.CreateWithVariants(ctx, e, []*productmodel.VariantEntity{v}); err != nil {
		return nil, err
	}
	// 首个变体的库存记录落在同一个归属仓（初始 0）。
	if err = s.ensureVariantStock(ctx, ref, e.ID, v.ID, v.SKUCode); err != nil {
		return nil, err
	}
	// issue #19：新增关键主数据 → 写变更记录（商品与首个变体各一条，逐字段落行）。
	// 变体那条带上了归属仓（默认发货仓），这是该事实在商品侧唯一可留痕的地方。
	if err = s.recordChanges(ctx,
		productChangeInput(e, masterdataenums.ActionCreate, masterdataenums.OriginProduct, req.OperatorID,
			nil, productChangeSnapshot(e)),
		variantChangeInput(projectID, v, masterdataenums.ActionCreate, masterdataenums.OriginVariant, req.OperatorID,
			nil, variantChangeSnapshot(v, ref)),
	); err != nil {
		return nil, err
	}
	// 重算时机之一：商品写操作后 —— 新建商品若已满足某条自动规则（如价格区间），
	// 立刻归位，不必等到下一次重算。
	if err = s.recalcAutoTags(ctx, projectID); err != nil {
		return nil, err
	}
	return s.toResp(ctx, e)
}

// Update 修改商品（含 slug 改名；变体走独立接口）。
func (s *Service) Update(ctx context.Context, req *productdto.UpdateReq) (res *productdto.ProductResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	// issue #19：改前快照必须在任何赋值之前取（之后的字段级 diff 以它为基准）。
	before := productChangeSnapshot(e)
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return nil, errors.New(productenums.ErrNameRequired)
		}
		e.Name = strings.TrimSpace(*req.Name)
	}
	if req.Slug != nil {
		slug := normalizeSlug(*req.Slug)
		if slug == "" {
			return nil, errors.New(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.SlugExists(ctx, e.ProjectID, slug, e.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, errors.New(productenums.ErrSlugTaken)
		}
		e.Slug = slug
	}
	if req.Subtitle != nil {
		e.Subtitle = *req.Subtitle
	}
	if req.Description != nil {
		e.Description = req.Description
	}
	if req.Status != nil {
		newStatus := strings.TrimSpace(*req.Status)
		// 上架时间（issue #11）：进入 published 时记录，作为自动标签「新品」规则的基准。
		// 已是 published 的重复提交不刷新（否则改一次名字就把商品「重新上架」了）。
		if newStatus == productenums.StatusPublished && e.Status != productenums.StatusPublished {
			now := time.Now().UTC()
			e.PublishedAt = &now
		}
		e.Status = newStatus
	}
	if req.Unit != nil {
		e.Unit = *req.Unit
	}
	if req.Weight != nil {
		e.Weight = req.Weight
	}
	if req.SEOTitle != nil {
		e.SEOTitle = *req.SEOTitle
	}
	if req.SEODescription != nil {
		e.SEODescription = *req.SEODescription
	}
	if req.Images != nil {
		e.Images = orJSONList(req.Images)
	}
	if req.ImageAlts != nil {
		e.ImageAlts = orJSONList(req.ImageAlts)
	}
	if req.AttributeIDs != nil {
		ids, aerr := s.resolveAttributeIDs(ctx, e.ProjectID, req.AttributeIDs)
		if aerr != nil {
			return nil, aerr
		}
		e.AttributeIDs = orJSONList(ids)
	}
	if req.CategoryIDs != nil || req.PrimaryCategoryID != nil {
		ids, primaryID, aerr := s.applyCategoryRefs(ctx, e.ProjectID, req.CategoryIDs,
			decodeStrings(e.CategoryIDs), req.PrimaryCategoryID, e.PrimaryCategoryID)
		if aerr != nil {
			return nil, aerr
		}
		e.CategoryIDs = orJSONList(ids)
		e.PrimaryCategoryID = primaryID
	}
	if req.TagIDs != nil {
		// 手工标签引用先校验再落库（同一工程内 + 必须存在 + 必须手工标签）。
		// 自动标签的归属由 recalcProjectAutoTags 在本次更新末尾重算，这里给的空数组
		// 不会真的把它们摘掉。
		ids, terr := s.resolveTagIDs(ctx, e.ProjectID, req.TagIDs)
		if terr != nil {
			return nil, terr
		}
		e.TagIDs = orIDList(ids)
	}
	if req.RelatedIDs != nil {
		e.RelatedIDs = orIDList(req.RelatedIDs)
	}
	if req.BundleItems != nil {
		// 形状规范化（issue #20）：整体替换语义不变，但写进去的必须是合法对象。
		bundleItems, berr := normalizeBundleItems(req.BundleItems)
		if berr != nil {
			return nil, berr
		}
		e.BundleItems = bundleItems
	}
	if req.BrandID != nil {
		// 品牌引用同样先校验（空串 = 解绑，与分类的「整体替换」语义一致）。
		brandID, berr := s.resolveBrandID(ctx, e.ProjectID, *req.BrandID)
		if berr != nil {
			return nil, berr
		}
		e.BrandID = brandID
	}
	if req.DefaultPrice != nil {
		e.DefaultPrice = req.DefaultPrice
	}
	if req.DefaultImage != nil {
		e.DefaultImage = *req.DefaultImage
	}
	if req.Metadata != nil {
		e.Metadata = req.Metadata
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.Update(ctx, e); err != nil {
		return nil, err
	}
	// issue #19：字段级变更留痕 —— 上下架状态 / 名称 / URL 段 / 商品级默认售价 / 品牌。
	// 只写真正变化的字段：改一次备注不会在审计里留一串空记录。
	if err = s.recordChanges(ctx, productChangeInput(e, masterdataenums.ActionUpdate,
		masterdataenums.OriginProduct, req.OperatorID, before, productChangeSnapshot(e))); err != nil {
		return nil, err
	}
	// 重算时机之一：商品写操作后 —— 改状态（上架 / 下架）与改标签引用都会影响自动标签归属。
	if err = s.recalcProjectAutoTags(ctx, e.ID); err != nil {
		return nil, err
	}
	return s.toResp(ctx, e)
}

// Get 商品详情（含变体与价格区间）。
func (s *Service) Get(ctx context.Context, req *productdto.GetReq) (res *productdto.ProductResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return s.toResp(ctx, e)
}

// List 商品列表（不返回 metadata 与变体明细；价格区间由变体聚合算出）。
func (s *Service) List(ctx context.Context, req *productdto.ListReq) (list []*productdto.ProductResp, err error) {
	page, size := pageArgs(req)
	rows, err := s.m.List(ctx, req.ProjectID, strings.TrimSpace(req.Keyword), req.Status, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	variants, err := s.m.ListVariantsByProducts(ctx, ids)
	if err != nil {
		return nil, err
	}
	byProduct := map[string][]*productmodel.VariantEntity{}
	for _, v := range variants {
		byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
	}
	// 属性组按 id 全局去重后批量取一次：多个商品引用同一组时只查一次（复用语义）。
	attrs, err := s.attributeRespByProduct(ctx, rows)
	if err != nil {
		return nil, err
	}
	list = make([]*productdto.ProductResp, 0, len(rows))
	for _, r := range rows {
		resp := s.toListResp(r)
		resp.AttributeIDs = decodeStrings(r.AttributeIDs)
		resp.Attributes = attrs[r.ID]
		applyPriceRange(resp, byProduct[r.ID])
		list = append(list, resp)
	}
	return list, nil
}

// Delete 删除商品（变体连带删除）。
func (s *Service) Delete(ctx context.Context, req *productdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	e, gerr := s.m.Get(ctx, req.ID)
	if gerr != nil {
		return mapNotFound(gerr)
	}
	// issue #19：删除是最需要留痕的一类动作 —— 先取商品与全部变体的快照，
	// 删成功后逐实体落「delete」记录（new 为空、old 为删除前的取值）。
	variants, verr := s.m.ListVariants(ctx, req.ID)
	if verr != nil {
		return verr
	}
	if err = s.m.Delete(ctx, req.ID); err != nil {
		return err
	}
	inputs := make([]*masterdatacontract.ChangeInput, 0, len(variants)+1)
	inputs = append(inputs, productChangeInput(e, masterdataenums.ActionDelete,
		masterdataenums.OriginProduct, req.OperatorID, productChangeSnapshot(e), nil))
	for _, v := range variants {
		inputs = append(inputs, variantChangeInput(e.ProjectID, v, masterdataenums.ActionDelete,
			masterdataenums.OriginVariant, req.OperatorID, variantChangeSnapshot(v, nil), nil))
	}
	return s.recordChanges(ctx, inputs...)
}

// attributeRespByProduct 批量取各商品引用的属性组（列表页专用，零 N+1）。
//
// products.attribute_ids 只存 id：多个商品可引用同一个属性组，定义只存一份。
// 这里按 id 去重后一次性取回，再按商品拆分；引用已失效（组被删）时跳过，
// 不让列表因为一条悬空引用整体失败。
func (s *Service) attributeRespByProduct(ctx context.Context, products []*productmodel.ProductEntity) (out map[string][]*productdto.AttributeResp, err error) {
	out = map[string][]*productdto.AttributeResp{}
	idSet := map[string]bool{}
	for _, p := range products {
		for _, id := range decodeStrings(p.AttributeIDs) {
			idSet[id] = true
		}
	}
	if len(idSet) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	rows, lerr := s.m.ListAttributesByIDs(ctx, ids)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productdto.AttributeResp, len(rows))
	for _, r := range rows {
		byID[r.ID] = toAttributeResp(r)
	}
	for _, p := range products {
		refs := decodeStrings(p.AttributeIDs)
		if len(refs) == 0 {
			continue
		}
		items := make([]*productdto.AttributeResp, 0, len(refs))
		for _, id := range refs {
			if a, ok := byID[id]; ok {
				items = append(items, a)
			}
		}
		if len(items) > 0 {
			out[p.ID] = items
		}
	}
	return out, nil
}

// resolveProjectID 解析工程：显式指定优先，否则取唯一工程。
func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if projectID != "" {
		return projectID, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(productenums.ErrInvalidParam)
	}
	return list[0].ID, nil
}

// pageArgs 归一化分页参数。
func pageArgs(req *productdto.ListReq) (page, size int) {
	page, size = 1, defaultPageSize
	if req == nil {
		return page, size
	}
	if req.Page > 0 {
		page = req.Page
	}
	if req.Size > 0 {
		size = req.Size
		if size > maxPageSize {
			size = maxPageSize
		}
	}
	return page, size
}

// normalizeSlug 规范化传入 slug（小写 + 去首尾空白）。
func normalizeSlug(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "-")
	return strings.ToLower(s)
}

// deriveSlug 由商品名派生 slug：保留字母数字、其余转连字符；中文名派生为空，由调用方兜底。
func deriveSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// orJSON jsonb 列的兜底值。
func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

// orJSONList 字符串数组转 jsonb（空数组写 []）。
func orJSONList(items []string) json.RawMessage {
	if items == nil {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(items)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// orIDList id 数组转 jsonb。
func orIDList(items []string) json.RawMessage { return orJSONList(items) }

// toResp 组装商品详情（含变体与价格区间）。
func (s *Service) toResp(ctx context.Context, e *productmodel.ProductEntity) (res *productdto.ProductResp, err error) {
	variants, err := s.m.ListVariants(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	resp := s.toListResp(e)
	resp.ImageAlts = decodeStrings(e.ImageAlts)
	resp.Description = orJSON(e.Description, "{}")
	resp.Subtitle = e.Subtitle
	resp.Unit = e.Unit
	resp.Weight = e.Weight
	resp.SEOTitle = e.SEOTitle
	resp.SEODescription = e.SEODescription
	resp.AttributeIDs = decodeStrings(e.AttributeIDs)
	resp.CategoryIDs = decodeStrings(e.CategoryIDs)
	if e.PrimaryCategoryID != nil {
		resp.PrimaryCategoryID = *e.PrimaryCategoryID
	}
	resp.TagIDs = decodeStrings(e.TagIDs)
	if e.PublishedAt != nil {
		resp.PublishedAt = e.PublishedAt.Format(time.RFC3339)
	}
	resp.RelatedIDs = decodeStrings(e.RelatedIDs)
	resp.BundleItems = orJSON(e.BundleItems, string(defaultBundleItemsJSON))
	resp.DefaultPrice = e.DefaultPrice
	resp.DefaultImage = e.DefaultImage
	resp.Metadata = orJSON(e.Metadata, "{}")
	if e.BrandID != nil {
		resp.BrandID = *e.BrandID
	}
	for _, v := range variants {
		resp.Variants = append(resp.Variants, toVariantResp(v))
	}
	// 库存展示值查询期投影（issue #32）：一次批量取真源汇总。
	s.fillVariantStock(ctx, resp.ProjectID, resp.Variants)
	// 引用到的属性组（组 + 值），供后台与详情页直接渲染规格选择器。
	if groups, aerr := s.attributeRespByProduct(ctx, []*productmodel.ProductEntity{e}); aerr != nil {
		return nil, aerr
	} else {
		resp.Attributes = groups[e.ID]
	}
	applyPriceRange(resp, variants)
	// 挂载的分类 / 品牌 / 标签（issue #12）：翻译工作台要按分类名、品牌名、标签名
	// 展示与取词，详情接口一并返回实体（后台翻译页不为每个 id 再打一次接口）。
	if rerr := s.fillRelated(ctx, resp, e); rerr != nil {
		return nil, rerr
	}
	return resp, nil
}

// fillRelated 把商品引用的分类 / 品牌 / 标签填入详情响应（失败不阻断详情读取）。
func (s *Service) fillRelated(ctx context.Context, resp *productdto.ProductResp, e *productmodel.ProductEntity) (err error) {
	ids := decodeStrings(e.CategoryIDs)
	if len(ids) > 0 {
		rows, cerr := s.m.ListCategoriesByIDs(ctx, ids)
		if cerr != nil {
			return cerr
		}
		byID := make(map[string]*productmodel.ProductCategoryEntity, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, id := range ids {
			if row, ok := byID[id]; ok {
				resp.Categories = append(resp.Categories, toCategoryResp(row))
			}
		}
	}
	if e.BrandID != nil && strings.TrimSpace(*e.BrandID) != "" {
		if row, berr := s.m.GetBrand(ctx, *e.BrandID); berr == nil {
			resp.Brand = toBrandResp(row)
		}
	}
	tagIDs := decodeStrings(e.TagIDs)
	if len(tagIDs) > 0 {
		rows, terr := s.m.ListTagsByIDs(ctx, tagIDs)
		if terr != nil {
			return terr
		}
		byID := make(map[string]*productmodel.ProductTagEntity, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, id := range tagIDs {
			if row, ok := byID[id]; ok {
				resp.Tags = append(resp.Tags, toTagResp(row))
			}
		}
	}
	return nil
}

// toListResp 组装列表项（不含 metadata / description）。
func (s *Service) toListResp(e *productmodel.ProductEntity) *productdto.ProductResp {
	resp := &productdto.ProductResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Slug: e.Slug,
		Status: e.Status, Sort: e.Sort, Images: decodeStrings(e.Images),
		ImageAlts: decodeStrings(e.ImageAlts),
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if len(resp.Images) == 0 {
		resp.Images = []string{}
	}
	return resp
}

// applyPriceRange 用变体算出商品的价格区间（商品主体不存价格，区间是只读派生）。
func applyPriceRange(resp *productdto.ProductResp, variants []*productmodel.VariantEntity) {
	resp.VariantCount = len(variants)
	if len(variants) == 0 {
		return
	}
	minV, maxV := variants[0].Price, variants[0].Price
	for _, v := range variants[1:] {
		if v.Price < minV {
			minV = v.Price
		}
		if v.Price > maxV {
			maxV = v.Price
		}
	}
	resp.PriceMin, resp.PriceMax = minV, maxV
}

// decodeStrings jsonb 数组 → 字符串切片（失败返回空切片，不阻断读取）。
func decodeStrings(raw json.RawMessage) (out []string) {
	out = []string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

// fillVariantStock 用库存真源汇总填充变体响应的库存投影值（issue #32）。
//
// 库存不再有商品侧缓存列，展示值只能**查询期投影**：
//
//	· 一次批量取真源按变体汇总（不是逐条查）；
//	· 未注入库存 model（纯商品单测路径）时保持 0 —— 调用方**不可**据此判断可用量；
//	· 投影失败不影响商品本身的读取（库存是展示值，不是商品的组成部分）。
//
// 死线不变：可用量判断一律走库存真源的带行锁路径，永远不看这个投影值。
func (s *Service) fillVariantStock(ctx context.Context, projectID string, variants []*productdto.VariantResp) {
	if s.inv == nil || len(variants) == 0 {
		return
	}
	ids := make([]string, 0, len(variants))
	for _, v := range variants {
		if v != nil && v.ID != "" {
			ids = append(ids, v.ID)
		}
	}
	rows, err := s.inv.StockTotals(ctx, projectID, ids)
	if err != nil {
		return
	}
	totals := make(map[string]int, len(rows))
	for _, row := range rows {
		if row != nil {
			totals[row.VariantID] = row.Total
		}
	}
	for _, v := range variants {
		if v != nil {
			v.StockTotal = totals[v.ID]
		}
	}
}

// toVariantResp 变体实体 → 响应。
func toVariantResp(v *productmodel.VariantEntity) *productdto.VariantResp {
	return &productdto.VariantResp{
		ID: v.ID, ProductID: v.ProductID, SKUCode: v.SKUCode, Barcode: v.Barcode,
		Price: v.Price, ComparePrice: v.ComparePrice, CostPrice: v.CostPrice,
		Image: v.Image, OptionValues: orJSON(v.OptionValues, "{}"),
		Enabled: v.Enabled, Sort: v.Sort,
		CreatedAt: v.CreatedAt.Format(time.RFC3339), UpdatedAt: v.UpdatedAt.Format(time.RFC3339),
	}
}

// formatTimePtr 可空时间 → RFC3339（nil → 空串）。
func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
