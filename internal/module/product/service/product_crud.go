package productservice

// product_crud.go — 商品增删改查（写入时的快照/标签/主数据留痕编排、列表查询）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
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
	s.bumpFragmentCache(ctx, projectID)
	return s.toResp(ctx, e)
}

// Update 修改商品（含 slug 改名；变体走独立接口）。
func (s *Service) Update(ctx context.Context, req *productdto.UpdateReq) (res *productdto.ProductResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.Get(ctx, req.ID, projectID)
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
	if err = s.recalcProjectAutoTags(ctx, e.ID, e.ProjectID); err != nil {
		return nil, err
	}
	s.bumpFragmentCache(ctx, e.ProjectID)
	return s.toResp(ctx, e)
}

// Get 商品详情（含变体与价格区间）。
func (s *Service) Get(ctx context.Context, req *productdto.GetReq) (res *productdto.ProductResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.Get(ctx, req.ID, projectID)
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
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return gerr
	}
	e, gerr := s.m.Get(ctx, req.ID, projectID)
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
	if err = s.recordChanges(ctx, inputs...); err != nil {
		return err
	}
	s.bumpFragmentCache(ctx, e.ProjectID)
	return nil
}
