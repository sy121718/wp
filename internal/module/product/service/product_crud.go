package productservice

// product_crud.go — 商品增删改查（写入时的快照/标签/主数据留痕编排、列表查询）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
)

// 新建商品的 SKU 来源（docs/14 §1.1 的两条入口）。
//
//	custom    —— 自己创建：运营自己填编码（选了仓库则自动附加仓码前缀）；
//	warehouse —— 从仓库选：选仓库 + 该仓的一条货，主体 = <仓短码>_<仓库 SKU>。
//
// 空串按 custom 归一（存量调用方与接口路径都不带这个字段）。捆绑商品不存在于仓库，
// **不允许** warehouse（服务端硬拒，见 Create）。常量值不是 i18n key，只是请求里的取值，
// 所以不需要词条；取值非法时的错误才是 key（inventoryenums.ErrSKUSourceInvalid）。
const (
	skuSourceCustom    = "custom"
	skuSourceWarehouse = "warehouse"
)

// normalizeSKUSource 归一 SKU 来源：空串 = 自己创建，其余取值明确报错。
//
// 大小写不敏感：前端下拉给的是小写字面量，接口调用方可能写成 Warehouse。
// 非法取值**不**静默退化成自己创建 —— 那会让「从仓库选」的意图消失得无声无息。
func normalizeSKUSource(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", skuSourceCustom:
		return skuSourceCustom, nil
	case skuSourceWarehouse:
		return skuSourceWarehouse, nil
	default:
		return "", errors.New(inventoryenums.ErrSKUSourceInvalid)
	}
}

// firstNonEmptyCode 取第一个非空串（去首尾空白）。
func firstNonEmptyCode(values ...string) string {
	for _, v := range values {
		if code := strings.TrimSpace(v); code != "" {
			return code
		}
	}
	return ""
}

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
	// 相关商品引用（审计 DB-03 §1.2 / PROD-01）：related_ids 是同表自引用，此前是**唯一**
	// 没有校验的一条引用面（分类 / 标签 / 属性都有「存在 + 同工程」），任意 uuid 都能写进去。
	// 商品 id 先落定：自引用判定要用它（不能指向自己）。
	productID := uuid.NewString()
	relatedIDs, err := s.resolveRelatedIDs(ctx, projectID, productID, req.RelatedIDs)
	if err != nil {
		return nil, err
	}
	// 捆绑配置形状规范化（issue #20）：空 / [] → 空配置对象；形状不对即拒绝。
	// 语义校验（必选 / 上下限 / 整单件数）走 SetBundleConfig 专用入口，这里只保证形状合法。
	bundleItems, berr := normalizeBundleItems(req.BundleItems)
	if berr != nil {
		return nil, berr
	}
	productType, terr := normalizeProductType(req.Type)
	if terr != nil {
		return nil, terr
	}
	// 捆绑容器必须自定价：成员价不参与计价，容器价为空就是「0 元套餐」——
	// 列表与详情会显示 0.00，前台无从判断这是免费还是没配。
	if productType == productmodel.TypeBundle && (req.DefaultPrice == nil || *req.DefaultPrice <= 0) {
		return nil, errors.New(productenums.ErrBundlePriceRequired)
	}
	// SKU 来源（docs/14 §1.1）：自己创建 / 从仓库选。空串按「自己创建」归一 ——
	// 存量调用方与接口路径都不带这个字段。
	skuSource, serr := normalizeSKUSource(req.SKUSource)
	if serr != nil {
		return nil, serr
	}
	// 捆绑商品不存在于仓库：它的主体 SKU 一律自定义，「从仓库选」对它不成立。
	// 前端已把该入口对 bundle 隐去，服务端仍硬拒 —— 前端只是便捷入口。
	if productType == productmodel.TypeBundle && skuSource == skuSourceWarehouse {
		return nil, errors.New(inventoryenums.ErrWarehouseSKUBundleNotAllowed)
	}
	// 容器主体 SKU 的正式输入通道（规则 A / B）：运营在新建抽屉里填的编码走
	// CreateReq.SKUCode（json:"sku"）。变体商品留空即派生 —— 取商品 URL 段的 ASCII 段
	// （选了仓再加仓码前缀），派生不出 ASCII 段时明确报错、不退回随机码；
	// **捆绑商品留空即拒绝**（ErrBundleSKURequired，2026-09-19 用户拍板）—— 主体编码是它的
	// 对外身份，必须由运营看见并确认，抽屉负责预填建议值。metadata 原样落库：
	// 主体编码只留一份真源，就在 products.sku_code。
	customSKU := strings.TrimSpace(req.SKUCode)
	metadata := orJSON(req.Metadata, "{}")
	e := &productmodel.ProductEntity{
		ID: productID, ProjectID: projectID,
		Name: strings.TrimSpace(req.Name), Subtitle: req.Subtitle,
		Type:        productType,
		Description: orJSON(req.Description, "{}"), Slug: slug,
		Status: productenums.StatusDraft,
		Unit:   req.Unit, Weight: req.Weight,
		SEOTitle: req.SEOTitle, SEODescription: req.SEODescription,
		Images: orJSONList(mediaURLs(req.Images)), ImageAlts: orJSONList(req.ImageAlts),
		AttributeIDs:      orJSONList(attributeIDs),
		CategoryIDs:       orJSONList(categoryIDs),
		PrimaryCategoryID: primaryCategoryID,
		BrandID:           brandID,
		TagIDs:            orIDList(tagIDs), RelatedIDs: orIDList(relatedIDs),
		BundleItems: bundleItems,

		DefaultImage: mediaURL(req.DefaultImage),
		DefaultPrice: req.DefaultPrice,
		Metadata:     metadata,
		CreatedAt:    now, UpdatedAt: now,
	}
	if productType == productmodel.TypeBundle {
		// 规则 B：捆绑容器主体 SKU 由运营自定义、恒以 _B 结尾（缺后缀补齐）；
		// **没填即拒绝**（ErrBundleSKURequired）—— 抽屉已预填建议值，留空等于运营
		// 既没看见也没确认这个编码。不再按 URL 段静默派生。
		if e.SKUCode, err = buildProductContainerSKU(containerSKUInput{
			ProductType: productmodel.TypeBundle, Custom: customSKU,
		}); err != nil {
			return nil, err
		}
		// 主体 SKU 唯一性**预检**（工程内唯一，迁移 246 的偏唯一索引）：先给可读的业务错误，
		// 不让 23505 的索引名铺到页面上（CQ-009）；落库侧仍有兜底映射（mapContainerSKUConflict）。
		if err = s.ensureContainerSKUFree(ctx, projectID, e.SKUCode, ""); err != nil {
			return nil, err
		}
		// 捆绑容器不分销自己的 SKU：不生成首个变体、不建库存记录（它的库存由成员变体
		// 按 BOM 扣减，见 inventory_bom_items），也不产生变体那条变更记录。
		// 本分支只有两处写入（商品 + 变更记录），同样包进一个事务：留下「有商品没留痕」
		// 或反过来的半截状态，事后只能靠对账发现。
		if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
			if serr := rls.ScopeTx(tx, projectID); serr != nil {
				return serr
			}
			if xerr := s.m.CreateWithVariantsTx(ctx, tx, e, nil); xerr != nil {
				return mapContainerSKUConflict(xerr)
			}
			// 静态产物失效事件与商品行**同事务**（审计 ARCH-01）：回滚即无事件，
			// 提交后由消费者扇出 direct_content + content_collection 两类键。
			if xerr := s.enqueueProductInvalidationTx(ctx, tx, projectID, e.ID); xerr != nil {
				return xerr
			}
			return s.recordChangesTx(ctx, tx, productChangeInput(e, masterdataenums.ActionCreate,
				masterdataenums.OriginProduct, req.OperatorID, nil, productChangeSnapshot(e)))
		}); err != nil {
			return nil, err
		}
	} else {
		// 归属仓与多仓（issue #15 / 2026-09-19 口径）：勾了哪些仓就在哪些仓各建一行。
		// **认领仓** = 显式指定的第一个仓（未指定则默认仓），只有它决定主体 SKU 的仓码前缀。
		// 解析失败（如工程内没有默认仓 / 某个仓已停用）时整体失败 —— 先解析再落库，
		// 不留「有商品没库存记录」的半截状态。
		refs, werr := s.resolveWarehouseTargets(ctx, projectID, req.WarehouseIDs, req.WarehouseID)
		if werr != nil {
			return nil, werr
		}
		claim := claimWarehouse(refs)
		// 规则 A：容器主体 SKU 落到 products.sku_code（迁移 246），是商品的**身份编码**。
		// ① 从仓库选：<认领仓短码大写>_<仓库里那条 SKU 原样>；② 自己创建：编码原样 + 认领仓仓码前缀。
		// 没填则按商品 URL 段派生（确定性，不退回随机码）。
		//
		// 「从仓库选」的编码**不取前端提交的 sku**：前端提交的仓库编码只是线索，落库前一律在
		// 该仓复核一遍（GetWarehouseSKU）—— 服务端不信任前端，见 docs/14 §9.3。
		skuInput := customSKU
		externalSKU := ""
		if skuSource == skuSourceWarehouse {
			// 复核发生在**认领仓**：主体 SKU 的前缀、以及「那条货确实存在」的判定都取自它。
			picked, perr := s.pickWarehouseSKU(ctx, projectID, claim, req.WarehouseSKU)
			if perr != nil {
				return nil, perr
			}
			skuInput = stripWarehousePrefix(picked.SKUCode, picked.WarehouseCode)
			// 外部编码：显式填的优先；否则带入那条货登记的编码，再否则就是它自己的 SKU ——
			// 「从仓库选」这条来路本身就是在建立「我们的商品 ↔ 那条货」的映射（docs/14 §9.3）。
			externalSKU = firstNonEmptyCode(req.ExternalSKU, picked.ExternalSKU, skuInput)
		} else {
			// 自己创建 + 回填：只在运营显式填了外码时才写（§9.3 的另一条来路），
			// 不替他猜一个 —— 空串是合法状态（该仓用我们自己的 SKU）。
			externalSKU = strings.TrimSpace(req.ExternalSKU)
		}
		// 容器主体 SKU 的唯一入口：类型是 variant，**认领仓**的短码参与前缀。
		if e.SKUCode, werr = buildProductContainerSKU(containerSKUInput{
			ProductType: productmodel.TypeVariant, Slug: slug,
			Custom: skuInput, WarehouseCode: refCode(claim),
		}); werr != nil {
			return nil, werr
		}
		if s.invSvc == nil {
			if externalSKU != "" {
				return nil, errors.New(inventoryenums.ErrStockWarehouseNeeded)
			}
		} else if externalSKU, werr = s.invSvc.NormalizeExternalSKU(externalSKU); werr != nil {
			return nil, werr
		}
		// 主体 SKU 唯一性**预检**（工程内唯一，迁移 246 的偏唯一索引 uq_products_project_sku_code）：
		// 先给出可读的业务错误；落库侧仍有兜底映射（mapContainerSKUConflict），两处都不把
		// 23505 的索引名铺到页面上（CQ-009 的实测缺陷：编辑路径曾把 uq_products_project_sku_code 吐出去）。
		if werr = s.ensureContainerSKUFree(ctx, projectID, e.SKUCode, ""); werr != nil {
			return nil, werr
		}
		// 首个变体：由商品级默认值填充（新增路径）—— 无规格变体就是容器主体本身，
		// 因此它的 SKU 与 e.SKUCode 相同；后续组合生成的变体 SKU 都以该主体拼接
		// （见 product_variant_generate.go）。
		v, verr := s.newVariantFromDefaults(ctx, e, nil, refCode(claim), nil)
		if verr != nil {
			return nil, verr
		}
		// 一次商品创建 = 一个事务：商品 + 首个变体 + 各仓库存行 + 变更记录。
		// 任一步失败整体回滚 —— 「有商品没库存行」这种半截状态只能靠人工对账发现（AGENTS.md「写操作的事务与回滚」）。
		if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
			if serr := rls.ScopeTx(tx, projectID); serr != nil {
				return serr
			}
			if xerr := s.m.CreateWithVariantsTx(ctx, tx, e, []*productmodel.VariantEntity{v}); xerr != nil {
				return mapContainerSKUConflict(xerr)
			}
			// 创建即入库：勾选的每个仓各一行（裸码；该仓已有同裸码的行则复用不新建）。
			// external_sku 写在**认领仓**那一次（外码是「这条货在该仓的名字」，其余仓不猜）。
			if xerr := s.createStockRowsTx(ctx, tx, refs, e.ID, v.ID, v.SKUCode, externalSKU, req.Quantity); xerr != nil {
				return xerr
			}
			// 静态产物失效事件与商品行**同事务**（审计 ARCH-01）。
			if xerr := s.enqueueProductInvalidationTx(ctx, tx, projectID, e.ID); xerr != nil {
				return xerr
			}
			// issue #19：新增关键主数据 → 写变更记录（商品与首个变体各一条，逐字段落行）。
			// 变体那条带上了认领仓，这是该事实在商品侧唯一可留痕的地方。
			return s.recordChangesTx(ctx, tx,
				productChangeInput(e, masterdataenums.ActionCreate, masterdataenums.OriginProduct, req.OperatorID,
					nil, productChangeSnapshot(e)),
				variantChangeInput(projectID, v, masterdataenums.ActionCreate, masterdataenums.OriginVariant, req.OperatorID,
					nil, variantChangeSnapshot(v, claim)),
			)
		}); err != nil {
			return nil, err
		}
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
	// 商品类型只在创建时确定：变体 ↔ 捆绑的切换涉及「有没有自己的 SKU / 价格放在哪 /
	// 库存从哪扣」，不是一次字段更新能表达的（要换类型就新建商品并迁移数据）。
	// 显式拒绝而不是静默忽略：静默忽略会让调用方以为改成功了。
	if req.Type != nil {
		want, terr := normalizeProductType(*req.Type)
		if terr != nil {
			return nil, terr
		}
		if want != e.Type {
			return nil, errors.New(productenums.ErrProductTypeImmutable)
		}
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
		e.Images = orJSONList(mediaURLs(req.Images))
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
		// 相关商品引用（审计 DB-03 §1.2 / PROD-01）：整体替换语义与分类 / 标签 / 属性一致 ——
		// 先校验（存在 + 同工程 + 不指向自己）再落库，非法引用一次列全（见 resolveRelatedIDs）。
		ids, rerr := s.resolveRelatedIDs(ctx, e.ProjectID, e.ID, req.RelatedIDs)
		if rerr != nil {
			return nil, rerr
		}
		e.RelatedIDs = orIDList(ids)
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
		e.DefaultImage = mediaURL(*req.DefaultImage)
	}
	if req.Metadata != nil {
		e.Metadata = orJSON(req.Metadata, "{}")
	}
	if req.SKUCode != nil {
		// 只有运营**显式**给了主体编码才动 products.sku_code；nil 表示本次不改 ——
		// 存量商品的 SKU 一律不重写（新规则只约束新建商品与新生成的变体），
		// 捆绑的「必填」也因此不会拦住任何不带 sku 键的存量商品更新。
		// 给了空串即拒绝：静默忽略会让运营以为改成功了（页面上又看不出差别）。
		custom := strings.TrimSpace(*req.SKUCode)
		if custom == "" {
			return nil, errors.New(productenums.ErrContainerSkuInvalid)
		}
		// 编辑路径不补仓码前缀：仓码前缀是创建时按归属仓确定的，UpdateReq 没有仓上下文，
		// 不在这里猜（要换主体编码的仓前缀，走新建商品）。
		// 走到了这里 custom 必非空，所以捆绑分支只会补 _B 后缀，不会触发必填错误。
		if e.Type == productmodel.TypeBundle {
			// 与新建路径共用同一个入口（本批的合并）：编辑路径同样只补 _B 后缀、
			// 不接仓码前缀 —— 仓码是创建时按归属仓确定的，UpdateReq 没有仓上下文。
			if e.SKUCode, err = buildProductContainerSKU(containerSKUInput{
				ProductType: productmodel.TypeBundle, Custom: custom,
			}); err != nil {
				return nil, err
			}
		} else {
			e.SKUCode = custom
		}
		// 主体 SKU 唯一性**预检**（编辑路径同样要）：excludeID = 自身，
		// 否则「保存一个没改编码的商品」会被自己撞下。落库侧仍有兜底映射
		//（mapContainerSKUConflict）—— 这正是修 CQ-009 的那条：编辑路径曾把
		// uq_products_project_sku_code 这样的索引名直接吐到页面上。
		if err = s.ensureContainerSKUFree(ctx, e.ProjectID, e.SKUCode, e.ID); err != nil {
			return nil, err
		}
	}
	e.UpdatedAt = time.Now().UTC()
	// 商品行与变更记录同事务：改成功了却没留痕（或反过来）都是错账。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if xerr := s.m.UpdateTx(ctx, tx, e); xerr != nil {
			return mapContainerSKUConflict(xerr)
		}
		// 静态产物失效事件与商品行**同事务**（审计 ARCH-01）。
		if xerr := s.enqueueProductInvalidationTx(ctx, tx, e.ProjectID, e.ID); xerr != nil {
			return xerr
		}
		// issue #19：字段级变更留痕 —— 上下架状态 / 名称 / URL 段 / 商品级默认售价 / 品牌。
		// 只写真正变化的字段：改一次备注不会在审计里留一串空记录。
		return s.recordChangesTx(ctx, tx, productChangeInput(e, masterdataenums.ActionUpdate,
			masterdataenums.OriginProduct, req.OperatorID, before, productChangeSnapshot(e)))
	}); err != nil {
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
	attrs, err := s.attributeRespByProduct(ctx, req.ProjectID, rows)
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
	// 库存聚合（列表「库存」列，docs/14 §1.4）：一次批量读回这些商品在各仓的库存行，
	// 按三态归并（∞ / 求和 / 未入库）。放在循环外，避免逐商品一次往返。
	s.fillProductStock(ctx, req.ProjectID, list)
	return list, nil
}

// CountProducts 商品列表总数（后台列表页分页用）。
//
// 过滤条件与 List **逐字一致**（工程作用域 + 关键词 + 状态）：两处口径分叉会让
// 「共 N 条」与列表实际能翻出来的条数对不上，而这种偏差只在跨页时才看得出来。
// 关键词与状态在此归一（TrimSpace）—— handler 传来的原始查询串不是可信边界。
func (s *Service) CountProducts(ctx context.Context, req *productdto.ListReq) (n int64, err error) {
	if req == nil {
		return 0, errors.New(productenums.ErrInvalidParam)
	}
	return s.m.Count(ctx, req.ProjectID, strings.TrimSpace(req.Keyword), req.Status)
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
	inputs := make([]*masterdatacontract.ChangeInput, 0, len(variants)+1)
	inputs = append(inputs, productChangeInput(e, masterdataenums.ActionDelete,
		masterdataenums.OriginProduct, req.OperatorID, productChangeSnapshot(e), nil))
	for _, v := range variants {
		inputs = append(inputs, variantChangeInput(e.ProjectID, v, masterdataenums.ActionDelete,
			masterdataenums.OriginVariant, req.OperatorID, variantChangeSnapshot(v, nil), nil))
	}
	// 删除与留痕同事务：删掉了却没留痕（或留痕了却没删掉）都是错账 ——
	// 商品、变体、各仓库存行由外键级联一起删，留痕是同一事务里的最后一步。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if derr := s.m.DeleteTx(ctx, tx, req.ID); derr != nil {
			return derr
		}
		// 删除 = 集合成员变化，必须发集合键：新产物里已经没有这个实体，
		// 只有集合键能让「列表页少一张卡」这件事被推导出来（审计 ARCH-01）。
		if derr := s.enqueueProductInvalidationTx(ctx, tx, e.ProjectID, req.ID); derr != nil {
			return derr
		}
		return s.recordChangesTx(ctx, tx, inputs...)
	}); err != nil {
		return err
	}
	s.bumpFragmentCache(ctx, e.ProjectID)
	return nil
}

// —— 「从仓库选」的落点（docs/14 §9.3，迁移 251）——

// pickWarehouseSKU 在**已解析的归属仓**里复核「那条货确实存在」，返回它。
//
// 宁可拒绝，也不静默退化成「拿前端给的字符串自造编码」—— 那正是「服务端不信任前端」
// 要防的形态。未给编码 / 未接库存能力时给 ErrWarehouseSKURequired；
// 该仓没有这条编码时给 ErrWarehouseSKUNotFound（由库存用例判定）。
func (s *Service) pickWarehouseSKU(ctx context.Context, projectID string, ref *productcontract.WarehouseRef, warehouseSKU string) (res *inventorydto.WarehouseSKUResp, err error) {
	code := strings.TrimSpace(warehouseSKU)
	if code == "" || s.invSvc == nil || ref == nil {
		return nil, errors.New(inventoryenums.ErrWarehouseSKURequired)
	}
	return s.invSvc.GetWarehouseSKU(ctx, &inventorydto.GetWarehouseSKUReq{
		ProjectID: projectID, WarehouseID: ref.ID, SKUCode: code,
	})
}

// —— 多仓建行与主体 SKU 唯一性（2026-09-19 口径）——

// resolveWarehouseTargets 解析本次要写库存行的仓（保持调用方给的顺序、按仓 id 去重）。
//
// 取值优先级：WarehouseIDs（多仓勾选）> WarehouseID（单值兼容）> 空（默认仓）。
// **顺序必须保持**：第一个非空项是认领仓（决定主体 SKU 的仓码前缀），顺序一变，
// 同一份表单就会生成不同的编码。去重按**解析后的仓 id** —— 「勾了默认仓 + 又显式勾了
// 默认仓那一个」只该建一行（同一份选择产生两行是幂等性漏洞，不是用户意图）。
//
// 未装库存能力（纯商品单测路径）时 resolveWarehouseRef 返回 nil：结果是空列表，
// 调用方据此走「不建库存行、SKU 不带前缀」的既有路径。
func (s *Service) resolveWarehouseTargets(ctx context.Context, projectID string, ids []string, legacyID string) (refs []*productcontract.WarehouseRef, err error) {
	targets := make([]string, 0, len(ids)+1)
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			targets = append(targets, trimmed)
		}
	}
	if len(targets) == 0 {
		targets = append(targets, strings.TrimSpace(legacyID))
	}
	seen := make(map[string]bool, len(targets))
	for _, id := range targets {
		ref, rerr := s.resolveWarehouseRef(ctx, projectID, id)
		if rerr != nil {
			return nil, rerr
		}
		if ref == nil || ref.ID == "" || seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		refs = append(refs, ref)
	}
	return refs, nil
}

// claimWarehouse 认领仓 = 解析出的第一个仓：只有它决定主体 SKU 的仓码前缀。
//
// 其余勾选的仓只各建一行（裸码），不参与命名 —— 「前缀标注哪个仓认领了它」，
// 多仓共用同一个前缀是刻意的（SKU 是商品的对外身份，不该随勾选数量变化）。
func claimWarehouse(refs []*productcontract.WarehouseRef) *productcontract.WarehouseRef {
	if len(refs) == 0 {
		return nil
	}
	return refs[0]
}

// warehouseCodeExists 该仓是否已有这条**裸码**（有即复用，不再新建）。
//
// 「认领 / 复用」是这条规则的全部：下拉里选到的是仓库已有的裸码 → 那一行本来就在，
// 直接复用；自己打新码 → 在勾选的各仓各建一行。两条路都不会产生「同仓同码两行」
// （UNIQUE (warehouse_id, sku_code)，迁移 244）—— 这也正是库存侧的幂等建行兜不住的那一段：
// inventory 的 ON CONFLICT 只覆盖 (variant_id, warehouse_id)，撞到仓内唯一约束是一句
// 会**把调用方事务标记为 aborted** 的报错（商品 + 变体一起回滚）。
//
// 用 GetWarehouseSKU 而不是 ListStocks：这是最小查询，且与「从仓库选」的复核共用同一个入口。
func (s *Service) warehouseCodeExists(ctx context.Context, projectID, warehouseID, skuCode string) (exists bool, err error) {
	if s.invSvc == nil || strings.TrimSpace(skuCode) == "" {
		return false, nil
	}
	_, gerr := s.invSvc.GetWarehouseSKU(ctx, &inventorydto.GetWarehouseSKUReq{
		ProjectID: projectID, WarehouseID: warehouseID, SKUCode: skuCode,
	})
	if gerr == nil {
		return true, nil
	}
	// 不存在是常态（自己打的新码），不是错误；其余错误（参数 / 库故障）原样上抛。
	if gerr.Error() == inventoryenums.ErrWarehouseSKUNotFound {
		return false, nil
	}
	return false, gerr
}

// createStockRowsTx 在勾选的各仓各建一行库存（裸码），已有同裸码的行**复用不新建**（幂等）。
//
// 三条口径（docs/14 §1.1 / §9.3，2026-09-19 用户拍板）：
//
//	· 仓库侧写**裸码** —— 剥掉仓码前缀的那一串（属性段保留）；剥前缀由 stripWarehousePrefix 统一做；
//	· 该仓已有同裸码的行 → **不新建**（那一行就是这条货）——「从仓库选」这条来路正是靠它成立；
//	· 外码与数量只作用于本次**新建**的行：外码写在认领仓那一次（外码是「这条货在该仓的名字」），
//	  数量按表单口径（nil = 不跟踪 = 无限；非 nil = 跟踪并写入）。
//
// 调用方必须已开事务（tx 已设工程作用域）：多仓逐行写入要么全成、要么全不成。
func (s *Service) createStockRowsTx(ctx context.Context, tx *gorm.DB, refs []*productcontract.WarehouseRef,
	productID, variantID, productSKU, externalSKU string, quantity *int) (err error) {
	if s.invSvc == nil {
		return nil
	}
	// 裸码只剥**一次**，而且按**认领仓**的仓码剥：其余仓写的是同一条裸码
	//（「同一商品多仓各一行同名裸码」）。各仓各自剥前缀反而是错的 —— 目标仓与认领仓不同，
	// SZ_X 在上海仓（仓码 SH）剥不掉，于是上海仓凭空多出一个带 SZ_ 前缀的编码。
	bare := stripWarehousePrefix(productSKU, refCode(claimWarehouse(refs)))
	for i, ref := range refs {
		if ref == nil {
			continue
		}
		exists, qerr := s.warehouseCodeExists(ctx, ref.ProjectID, ref.ID, bare)
		if qerr != nil {
			return qerr
		}
		if exists {
			// 认领 / 复用：这一行本来就在，不新建（也不改它的外码与数量 —— 覆盖走库存页的显式入口）。
			continue
		}
		ext := ""
		if i == 0 {
			ext = strings.TrimSpace(externalSKU)
			// N:1 弱校验（迁移 251）：同一仓内同一外码必须指向同一个商品。
			// 与库存页的绑定路径共用同一条规则，错误里带上冲突的商品 id。
			if ext != "" {
				if xerr := s.invSvc.CheckExternalSKUProductScope(ctx, ref.ProjectID, ref.ID, ext, productID); xerr != nil {
					return xerr
				}
			}
		}
		if xerr := s.ensureVariantStockBareWithExternalTx(ctx, tx, ref, productID, variantID, bare, ext, quantity); xerr != nil {
			return xerr
		}
	}
	return nil
}

// ensureContainerSKUFree 主体 SKU 的工程内唯一性**预检**（excludeID 是编辑时的自身）。
//
// 预检与唯一索引兜底（mapContainerSKUConflict）成对存在：预检负责给出可读文案，
// 索引负责挡住预检与落库之间那个时间窗里的并发写入。少任何一个都会在页面上漏出原始 SQL 错误。
func (s *Service) ensureContainerSKUFree(ctx context.Context, projectID, skuCode, excludeID string) (err error) {
	taken, qerr := s.m.SKUCodeExists(ctx, projectID, skuCode, excludeID)
	if qerr != nil {
		return qerr
	}
	if taken {
		return errors.New(productenums.ErrContainerSKUTaken)
	}
	return nil
}

// mapContainerSKUConflict 把落库时的唯一键冲突**归一**成业务错误（绝不回原始 SQL 错误）。
//
// 生产连接开了 gorm 的 TranslateError（pkg/database），拿到的是 gorm.ErrDuplicatedKey；
// 未翻译的连接（部分测试 fixture）拿到的是原始 23505 文本 —— 两条都认，
// 否则同一个缺陷会随连接配置不同而时隐时现。
func mapContainerSKUConflict(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errors.New(productenums.ErrContainerSKUTaken)
	}
	msg := err.Error()
	if strings.Contains(msg, "23505") || strings.Contains(msg, "uq_products_project_sku_code") {
		return errors.New(productenums.ErrContainerSKUTaken)
	}
	return err
}

// resolveRelatedIDs 校验并归一商品引用的相关商品 id（products.related_ids）。
//
// 为什么补这一条（审计 DB-03 §1.2 / PROD-01）：category_ids / tag_ids / attribute_ids
// 与 brand_id 都有「存在 + 同工程」的服务层校验，related_ids 此前是**唯一**原样落库的
// 引用面 —— 可以写入任意 uuid（别的工程、已被删除、甚至自己）。它落在 JSONB 数组里，
// 没有任何数据库级外键兜底，写脏了只能靠人工对账（DB-03 附录 A 的 Q6 / Q7）发现。
//
// 四条规则（与同表其它引用列同口径，外加一条自引用）：
//  1. 去空白 + 去重（保留首次出现的顺序）——「同一次提交写出同一份数组」；
//  2. 引用必须真实存在；
//  3. 引用必须与商品同工程（跨工程引用等于把别人的商品挂到自己的「相关商品」里）；
//  4. 不能指向自己（related_ids 是商品表的自引用，指向自己会让「相关商品」成环）。
//
// 三类问题**一次列全**（同一条错误的 tail 里分段给出 id 与数量）：逐个返回只会让运营
// 「改一条、提交一次」，而这批引用本就来自同一次提交。跨工程的条目额外带上**对方工程 id**，
// 让操作者知道该去找谁解除（PROD-01 的验收点）。
//
// 拒绝即整体拒绝：不静默丢弃非法 id、不自动过滤、不自动修正 —— 那些都会让运营以为
// 「保存成功了」，而实际的相关商品集合与他提交的不是同一份。
//
// selfID 是本次写入的商品自身 id（新建路径先用生成的 id，编辑路径用行上的 id）。
func (s *Service) resolveRelatedIDs(ctx context.Context, projectID, selfID string, ids []string) (out []string, err error) {
	out = []string{}
	if len(ids) == 0 {
		return out, nil
	}
	seen := make(map[string]bool, len(ids))
	dedup := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		dedup = append(dedup, id)
	}
	if len(dedup) == 0 {
		return out, nil
	}
	// 一次批量取回（复用 ListProductsByIDs，避免逐个 id 一次往返）：该方法自带工程作用域，
	// 这里不改它的实现（作用域补全由 DB-05 在另一条线上统一处理）。
	rows, lerr := s.m.ListProductsByIDs(ctx, dedup, projectID)
	if lerr != nil {
		return nil, lerr
	}
	byID := make(map[string]*productmodel.ProductEntity, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		byID[r.ID] = r
	}
	var selfRefs, missing, crossProject []string
	for _, id := range dedup {
		if selfID != "" && id == selfID {
			selfRefs = append(selfRefs, id)
			continue
		}
		row, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		if projectID != "" && row.ProjectID != projectID {
			// 带上对方工程 id：跨工程引用只能靠「两个工程的运营对账」解决，
			// 光有商品 id 找不到人（PROD-01 要求拒绝信息可定位）。
			crossProject = append(crossProject, fmt.Sprintf("%s（属于工程 %s）", id, row.ProjectID))
			continue
		}
		out = append(out, id)
	}
	if len(selfRefs)+len(missing)+len(crossProject) == 0 {
		return out, nil
	}
	parts := make([]string, 0, 3)
	if len(selfRefs) > 0 {
		parts = append(parts, fmt.Sprintf("不能指向自己（%d 个：%s）", len(selfRefs), strings.Join(selfRefs, ", ")))
	}
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("不存在（%d 个：%s）", len(missing), strings.Join(missing, ", ")))
	}
	if len(crossProject) > 0 {
		parts = append(parts, fmt.Sprintf("不属于本工程（%d 个：%s）", len(crossProject), strings.Join(crossProject, "；")))
	}
	return nil, fmt.Errorf("%s：%s", productenums.ErrRelatedInvalid, strings.Join(parts, "；"))
}
