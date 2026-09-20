// product_variant.go — 变体读写（issue #5 / T3a；归属仓与库存记录见 issue #15）。
//
// 本文件是「商品级默认值 → 新增变体」的**唯一填充入口**（newVariantFromDefaults）。
// 规则（spec §变体模型）：
//
//	· 只走新增路径（单个新增 / 批量生成 / 导入 / 克隆）；
//	· 逐字段判空，只填空着的字段；
//	· 「空」以 NULL 判定 —— 入参用可空类型，0 与 false 视为已填；
//	· 编辑路径一个字都不动，包括调用方主动清空字段。
//
// SKU 编码（2026-09-19 商品域评审第四轮定稿，规则见 docs/14-product-sku-and-cost-model.md §1/§4）：
//
//	· 容器主体 SKU 两种来源 ——
//	  ① 从仓库选：<仓库短码大写>_<仓库里那条 SKU 原样>（不再追加序号）；
//	  ② 自己创建：运营填的编码原样，选了仓则自动附加仓码前缀（SZ_<自定义>）；
//	· 变体 SKU = <容器主体 SKU>_<属性值段…>_V（属性段拼接在 product_variant_generate.go）。
//
// 前缀表达的是**默认发货仓**，不是「这个 SKU 只属于这个仓」——
// SKU 本身是全局的，货可以在多个仓分布、可以调拨，移动与调拨不改变编码。
//
// 唯一性口径（用户 2026-09-19 明确）：
//
//	· 容器主体唯一 —— products (project_id, sku_code) 的部分唯一索引 uq_products_project_sku_code
//	  （sku_code <> ''）。跨商品撞号由该索引拒绝，这里不另写跨商品预检；
//	· 变体只在商品内唯一 —— product_variants 的 UNIQUE (product_id, sku_code)，不额外收紧；
//	· 仓库内唯一 —— 由 inventory_stocks 的 UNIQUE (warehouse_id, sku_code) 保证（批次 A 负责），
//	  商品侧不再叠加一份校验；
//	· **不要求全局唯一** —— 同一段 SKU 文本允许出现在不同仓库。
//	  因此这里绝不可假设 SKU 全局唯一，也不可把「不同商品 / 不同仓的相同 SKU」判成冲突
//	  （SKUExists 的作用域参数 productID 正是这条口径的落点）。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
)

// maxSKUProbe SKU 编码的探测次数上限（基码被历史删除的变体占住时向后找空位）。
const maxSKUProbe = 500

const (
	// VariantSKUSuffix 变体 SKU 后缀（规则 A：变体 SKU = 容器主体 SKU + 属性值段… + _V）。
	VariantSKUSuffix = "_V"
	// BundleSKUSuffix 捆绑容器主体 SKU 后缀（规则 B：捆绑主体自定义，恒以 _B 结尾）。
	BundleSKUSuffix = "_B"
	// skuSeparator 主体 / 属性段 / 后缀之间统一的分隔符。
	skuSeparator = "_"
)

// SKU 编码新规则（2026-09-19 商品域评审第四轮）的三条业务错误已收口到 productenums
// （常量值即 i18n key，真文案在 sys_i18n，迁移 248 与 249 seed）：
//
//	ErrSkuContainerMissing —— 商品 URL 段不含 ASCII 字符，派生不出主体 SKU 编码；
//	ErrContainerSkuInvalid —— 显式填的主体 SKU 编码不合法（空串）；
//	ErrBundleSKURequired   —— 捆绑商品没填主体 SKU（留空不再静默派生，见 normalizeBundleSKU）。
//
// 三条都必须登记进 inbound 的业务错误 sentinel 白名单，否则页面只显示「系统内部错误」，
// 把可行动的提示（填一个编码 / 改 URL 段）丢掉。调用点一律直接 errors.New(enums 常量)，
// 不在服务层另立一份本地别名 —— 别名会让「哪些错误会被回带到页面」多出一份隐形清单。

// CreateVariant 为商品新增一个变体；未填字段由商品级默认值逐字段补齐。
//
// 归属仓（issue #15）：WarehouseID 为空即兜底该工程的默认仓；无论选没选，
// 该 SKU 都会在归属仓生成一条库存记录（初始 0）—— 库存真源在仓库模块。
func (s *Service) CreateVariant(ctx context.Context, req *productdto.CreateVariantReq) (res *productdto.VariantResp, err error) {
	if req == nil || req.ProductID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	p, err := s.m.Get(ctx, req.ProductID, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	// 归属仓先解析再落库：解析失败（如工程内没有默认仓）时一条变体都不写。
	ref, err := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	v, verr := s.newVariantFromDefaults(ctx, p, req, refCode(ref), nil)
	if verr != nil {
		return nil, verr
	}
	if taken, serr := s.m.SKUExists(ctx, p.ID, v.SKUCode, ""); serr != nil {
		return nil, serr
	} else if taken {
		return nil, errors.New(productenums.ErrSkuTaken)
	}
	// 变体行 + 该仓库存行 + 变更记录必须同事务：变体落了库、库存行没建起来
	//（或留痕没写）就是半截状态，只能靠人工对账发现（AGENTS.md「写操作的事务与回滚」）。
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, p.ProjectID); serr != nil {
			return serr
		}
		if cerr := s.m.CreateVariantTx(ctx, tx, v); cerr != nil {
			return cerr
		}
		// 库存行落**裸码**（剥仓码前缀），数量按表单口径（nil = 不跟踪 = 无限）。
		if serr := s.ensureVariantStockTx(ctx, tx, ref, p.ID, v.ID, v.SKUCode, req.Quantity); serr != nil {
			return serr
		}
		// issue #19：新增变体 → 变更记录。带上归属仓（默认发货仓）与 SKU 编码 ——
		// 这两件事只在此刻确定，之后编辑路径不再改动它们。
		return s.recordChangesTx(ctx, tx, variantChangeInput(p.ProjectID, v, masterdataenums.ActionCreate,
			masterdataenums.OriginVariant, req.OperatorID, nil, variantChangeSnapshot(v, ref)))
	})
	if err != nil {
		return nil, err
	}
	// 重算时机之一：变体写操作后 —— 价格 / 对比价参与自动标签规则判定。
	if err = s.recalcProjectAutoTags(ctx, p.ID, p.ProjectID); err != nil {
		return nil, err
	}
	return toVariantResp(v), nil
}

// UpdateVariant 修改变体。
//
// 编辑路径不做默认值填充：调用方清空某字段就是清空，不回落商品级默认值。
func (s *Service) UpdateVariant(ctx context.Context, req *productdto.UpdateVariantReq) (res *productdto.VariantResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	v, err := s.m.GetVariant(ctx, req.ID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	// issue #19：改前快照必须在任何赋值之前取；工程维度只在留痕端口已注入时才查
	//（纯商品单测路径不读库）。DB-009 之后这次「按变体反查工程」也需要工程作用域，
	// 所以先把请求给的工程解析出来当作用域。
	scopeID := ""
	if s.changes != nil {
		if scopeID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
			return nil, err
		}
	}
	projectID, err := s.variantProjectID(ctx, v, scopeID)
	if err != nil {
		return nil, err
	}
	before := variantChangeSnapshot(v, nil)
	if req.SKUCode != nil {
		code := strings.TrimSpace(*req.SKUCode)
		if code == "" {
			return nil, errors.New(productenums.ErrInvalidParam)
		}
		if taken, serr := s.m.SKUExists(ctx, v.ProductID, code, v.ID); serr != nil {
			return nil, serr
		} else if taken {
			return nil, errors.New(productenums.ErrSkuTaken)
		}
		v.SKUCode = code
	}
	if req.Barcode != nil {
		v.Barcode = *req.Barcode
	}
	if req.Price != nil {
		v.Price = *req.Price
	}
	if req.ComparePrice != nil {
		v.ComparePrice = req.ComparePrice
	}
	if req.CostPrice != nil {
		v.CostPrice = req.CostPrice
	}
	if req.Image != nil {
		v.Image = *req.Image
	}
	if req.OptionValues != nil {
		v.OptionValues = req.OptionValues
	}
	if req.Enabled != nil {
		v.Enabled = *req.Enabled
	}
	if req.Sort != nil {
		v.Sort = *req.Sort
	}
	v.UpdatedAt = time.Now().UTC()
	// 变体行与它的变更记录同事务：留痕写不进去（或变体更新失败）都不该留下半截改动。
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := transactionScope(tx, projectID); serr != nil {
			return serr
		}
		if uerr := s.m.UpdateVariantTx(ctx, tx, v); uerr != nil {
			return uerr
		}
		// issue #19：字段级变更留痕 —— SKU 编码 / 条码 / 售价 / 划线价 / 成本价 /
		// 启用状态 / 规格组合，只写真正变化的字段。
		return s.recordChangesTx(ctx, tx, variantChangeInput(projectID, v, masterdataenums.ActionUpdate,
			masterdataenums.OriginVariant, req.OperatorID, before, variantChangeSnapshot(v, nil)))
	})
	if err != nil {
		return nil, err
	}
	// 重算时机之一：变体写操作后（改价格 / 改启用状态都会改自动标签归属）。
	if err = s.recalcProjectAutoTags(ctx, v.ProductID, projectID); err != nil {
		return nil, err
	}
	return toVariantResp(v), nil
}

// DeleteVariant 删除变体。
//
// 该 SKU 在各仓的库存记录由外键 ON DELETE CASCADE 连带删除
// （变体不再存在即无货可存；库存流水与账目留给 issue #16 的流水表）。
func (s *Service) DeleteVariant(ctx context.Context, req *productdto.DeleteVariantReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(productenums.ErrInvalidParam)
	}
	v, gerr := s.m.GetVariant(ctx, req.ID)
	if gerr != nil {
		return mapNotFound(gerr)
	}
	// issue #19：删除前取快照与工程（删完之后这两个值都查不到了）。
	// DB-009：这次「按变体反查工程」同样需要工程作用域，所以先把请求给的工程解析出来
	//（留痕与守卫端口都没注入时保持原样：一次库都不读 —— 删除守卫读的是
	// inventory_stocks / inventory_bom_items / inventory_stock_movements 与
	// products.bundle_items，四张表都在迁移 215 名单里，缺作用域会静默放行删除）。
	var projectID, scopeID string
	if s.changes != nil || s.inv != nil || s.invSvc != nil {
		if scopeID, gerr = s.resolveProjectID(ctx, req.ProjectID); gerr != nil {
			return gerr
		}
	}
	if s.changes != nil {
		if projectID, gerr = s.variantProjectID(ctx, v, scopeID); gerr != nil {
			return gerr
		}
	}
	// 删除守卫：库存非零 / BOM 引用 / 捆绑成员引用 / 有过库存流水（命中任一即拒绝）。
	// 与「保存变体清单」的清单外删除共用**同一个**守卫函数 —— 两处各写一份必然分叉，
	// 表现为「接口能删掉、抽屉保存删不掉」这类只在一条路径上出现的缺陷。
	guardScope := projectID
	if guardScope == "" {
		guardScope = scopeID
	}
	if reason, detail, rerr := s.variantDeleteBlockReason(ctx, guardScope, v); rerr != nil {
		return rerr
	} else if reason != "" {
		// 单条删除路径的既有文案（ErrVariantHasStock）保留：调用方与页面提示都以它为准，
		// 其余三个引用面用守卫自己的原因 key（词条见迁移 259）。
		//
		// detail 是本批新增的**可定位明细**（引用面 / 工程 / 商品 id）：单条删除返回的是
		// 错误而不是页面回执，按既有的「key：明细」形态拼接即可（productErrKey 前缀识别），
		// 运营因此能看到「被哪个工程的哪个商品引用」，而不是只有一句「被捆绑成员引用」。
		if reason == productenums.VariantSkipHasStock {
			return errors.New(productenums.ErrVariantHasStock)
		}
		if detail != "" {
			return fmt.Errorf("%s：%s", reason, detail)
		}
		return errors.New(reason)
	}
	before := variantChangeSnapshot(v, nil)
	// 删除与留痕同事务：删掉了却没留痕，或留痕了却没删掉，都是错账。
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := transactionScope(tx, projectID); serr != nil {
			return serr
		}
		if derr := s.m.DeleteVariantTx(ctx, tx, req.ID); derr != nil {
			return derr
		}
		return s.recordChangesTx(ctx, tx, variantChangeInput(projectID, v, masterdataenums.ActionDelete,
			masterdataenums.OriginVariant, req.OperatorID, before, nil))
	})
	if err != nil {
		return err
	}
	// 重算时机之一：变体写操作后（删掉唯一命中价格区间的变体会让商品脱钩）。
	recalcScope := scopeID
	if recalcScope == "" {
		recalcScope = projectID
	}
	return s.recalcProjectAutoTags(ctx, v.ProductID, recalcScope)
}

// newVariantFromDefaults 商品级默认值 → 新变体的**唯一填充入口**。
//
// req 为 nil 表示无表单路径（如商品创建时的首个变体、批量生成、导入）：全部字段取默认值。
// 有 req 时逐字段判空：只有调用方没给（nil）的字段才回落商品级默认值。
//
// SKU 编码（新规则，见文件头）：调用方给了编码就按「容器主体 SKU」口径归一化 ——
// 编码本体原样保留（从仓库选中的那条 SKU 不做任何改写），选了仓只附加仓码前缀；
// 没给则按商品 URL 段派生确定性编码。**任何情况下都不再退回随机码**：派生不出来
// 返回错误，由调用方决定是补录编码还是改 slug。
//
// warehouseCode 是归属仓短码（issue #15，端口未注入时为空串 = 纯商品单测路径）。
// taken 是「本批已占用」的 SKU 编码集合（批量生成一次写多行时逐个探测，
// 否则同一批里的新变体都会取到同一个编码而互相撞号）；单个新增传 nil。
func (s *Service) newVariantFromDefaults(ctx context.Context, p *productmodel.ProductEntity, req *productdto.CreateVariantReq, warehouseCode string, taken map[string]bool) (v *productmodel.VariantEntity, err error) {
	now := time.Now().UTC()
	v = &productmodel.VariantEntity{
		ID: uuid.NewString(), ProductID: p.ID,
		Price:        defaultFloat(p.DefaultPrice, 0),
		ComparePrice: p.DefaultComparePrice,
		CostPrice:    p.DefaultCostPrice,
		Image:        p.DefaultImage,
		OptionValues: json.RawMessage("{}"),
		Enabled:      true,
		Metadata:     json.RawMessage("{}"),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if req != nil {
		if req.Price != nil {
			v.Price = *req.Price
		}
		if req.ComparePrice != nil {
			v.ComparePrice = req.ComparePrice
		}
		if req.CostPrice != nil {
			v.CostPrice = req.CostPrice
		}
		if req.Image != "" {
			v.Image = req.Image
		}
		if len(req.OptionValues) > 0 {
			v.OptionValues = req.OptionValues
		}
		if req.Enabled != nil {
			v.Enabled = *req.Enabled
		}
		v.Sort = req.Sort
		v.Barcode = req.Barcode
	}
	// —— SKU 编码（规则 A）——
	// 1) 调用方给了编码：它就是编码本体（从仓库选中的那条 SKU 原样保留），
	//    选了仓只做前缀拼接，**不再追加序号**。
	if custom := strings.TrimSpace(skuOrEmpty(req)); custom != "" {
		v.SKUCode = attachWarehousePrefix(custom, warehouseCode)
		return v, nil
	}
	// 2) 商品的主体编码（products.sku_code，迁移 246）是无规格变体的编码本体 ——
	//    商品创建时的首个变体走这里，因此「首个变体 = 容器主体」。
	base := strings.TrimSpace(p.SKUCode)
	if base == "" {
		// 3) 商品侧还没有主体编码（存量商品 / 端口未注入的纯商品单测路径）：
		//    按商品 URL 段派生确定性编码；派生不出来直接报错（不退回随机码）。
		//    同样走容器主体 SKU 的唯一入口（本批合并）。
		if base, err = buildProductContainerSKU(containerSKUInput{
			ProductType: p.Type, Slug: p.Slug, WarehouseCode: warehouseCode,
		}); err != nil {
			return nil, err
		}
	}
	// 4) 商品内唯一：该编码已被本商品的其它变体占住时（运营手工再加一个无规格变体）
	//    按 _002 / _003 … 让位。组合生成路径会另行覆盖成 <主体>_<属性段>_V。
	if v.SKUCode, err = s.uniqueSKUCode(ctx, p.ID, base, taken); err != nil {
		return nil, err
	}
	return v, nil
}

// skuOrEmpty 取调用方给的 SKU 编码（req 为 nil 时为空串，交由生成规则兜底）。
func skuOrEmpty(req *productdto.CreateVariantReq) string {
	if req == nil {
		return ""
	}
	return req.SKUCode
}

// uniqueSKUCode 让基码在**商品内**唯一（UNIQUE (product_id, sku_code)）。
//
// 被占用时按 _002 / _003 … 顺序向后找空位（删除变体会留下空洞），**不引入随机段** ——
// 同一商品同一输入恒得同一编码之外，冲突消解也是确定的、可解释的。
// taken 是「本批已占用」的集合（批量生成一次写多行时用），单个新增传 nil（只查库）。
func (s *Service) uniqueSKUCode(ctx context.Context, productID, base string, taken map[string]bool) (string, error) {
	for i := 0; i < maxSKUProbe; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s%s%03d", base, skuSeparator, i+1)
		}
		if taken[candidate] {
			continue
		}
		exists, qerr := s.m.SKUExists(ctx, productID, candidate, "")
		if qerr != nil {
			return "", qerr
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", errors.New(productenums.ErrSkuTaken)
}

// containerSKUInput 容器主体 SKU 的解析输入（三处调用点共用的形状，本批合并）。
//
// 三处取得容器主体 SKU 的路径（docs/14 §1.2「容器 SKU 获取函数合并」）：
//
//	① 商品新建 —— 从仓库选（Custom = 那条仓库 SKU）或自己创建（Custom = 运营填的编码）；
//	② 变体生成 / 预览 / 保存 —— 已存在商品的主体段（products.sku_code），
//	   存量商品回落到最早创建的那条变体，再回落按下述规则派生；
//	③ 捆绑主体 —— 自定义且恒以 _B 结尾（ProductType = bundle）。
//
// 行为差异只有一处，且是刻意的：**捆绑不存在于仓库**，所以它的主体段不接仓码前缀 ——
// 「从仓库选」对捆绑根本不成立（product_crud 已在入口硬拒 ErrWarehouseSKUBundleNotAllowed）。
type containerSKUInput struct {
	// ProductType 商品类型（productmodel.TypeVariant / TypeBundle）：决定后缀规则与是否接仓码。
	ProductType string
	// Slug 商品 URL 段：Custom 为空时按它的 ASCII 段派生（派生不出来明确报错，不退回随机码）。
	Slug string
	// Custom 运营给的编码：①「从仓库选」选中的那条仓库 SKU，或②自己创建填的编码。
	// 两者对拼法没有区别 —— 本体原样保留，选了仓只加一次仓码前缀。
	Custom string
	// WarehouseCode 归属仓短码（空 = 未选仓 / 捆绑 / 端口未注入的纯商品单测路径）。
	WarehouseCode string
}

// buildProductContainerSKU 容器主体 SKU 的**唯一**入口（商品新建 / 变体生成 / 捆绑主体三处共用）。
//
// 合并前：捆绑走 normalizeBundleSKU、变体走 buildContainerSKU，两条路各自被调用点直接拼参数，
// 「谁该加后缀、谁该接仓码」是散在调用点上的约定 —— 新增一条来路时最容易漏的是
// 「捆绑被顺手接上了仓码前缀」（SZ_XX_B），而那种编码在仓库侧根本不存在。
// 现在改成：类型决定分支，分支只有两条，调用点只负责把输入摆好。
//
// 既有 SKU 一律不重算、不改存量：本函数只服务**新建**（商品 / 新生成的变体 SKU 的主体段）。
func buildProductContainerSKU(in containerSKUInput) (string, error) {
	if in.ProductType == productmodel.TypeBundle {
		// 规则 B：捆绑主体由运营自定义、恒以 _B 结尾（缺后缀服务端补齐，大小写不敏感）；
		// 没填即明确报错 ErrBundleSKURequired，不静默按 URL 段派生（见 normalizeBundleSKU）。
		return normalizeBundleSKU(in.Custom)
	}
	return buildContainerSKU(in.Slug, in.Custom, in.WarehouseCode)
}

// buildContainerSKU 决定容器主体 SKU（变体商品的**唯一**拼接点）。
//
//	custom        运营给的编码 —— ①从仓库选中的那条 SKU，或 ②自己创建填的编码。
//	              两者对拼法没有区别：**本体原样保留**，选了仓只加一次仓码前缀。
//	warehouseCode 归属仓短码（空 = 未选仓，或端口未注入的纯商品路径）。
//
// custom 为空时用商品 URL 段派生（大写字母数字）；派生不出来报错，不退回随机码。
// 存量 SKU 一律不经过这里 —— 该函数只作用于新建商品与新生成的变体。
func buildContainerSKU(slug, custom, warehouseCode string) (string, error) {
	code := strings.TrimSpace(custom)
	if code == "" {
		code = productCodeSegment(slug)
		if code == "" {
			return "", errors.New(productenums.ErrSkuContainerMissing)
		}
	}
	return attachWarehousePrefix(code, warehouseCode), nil
}

// attachWarehousePrefix 给容器主体 SKU 附加仓码前缀（形如 SZ_<自定义>）。
//
// 幂等：编码已经以该仓码开头时不重复添加 —— 从仓库选中的那条 SKU 往往本身就带
// 仓码前缀，再拼一次会得到 SZ_SZ_xxx。
func attachWarehousePrefix(code, warehouseCode string) string {
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return code
	}
	if strings.HasPrefix(strings.ToUpper(code), prefix+skuSeparator) {
		return code
	}
	return prefix + skuSeparator + code
}

// stripWarehousePrefix 剥掉仓码前缀 —— attachWarehousePrefix 的**逆操作**。
//
// 仓库侧只认裸码（DRAWERSMOKE_001），仓码前缀只属于商品侧（SZ_DRAWERSMOKE_001，
// 标注哪个仓认领了它）。商品侧建库存行时统一在这里剥一次，调用点不必各自记得。
//
// 三条性质与 attachWarehousePrefix 对称：
//
//	· **幂等** —— 编码本来就不带前缀时原样返回，剥两次与剥一次相同；
//	· **大小写不敏感** —— 前缀按大写比对（仓短码在工程内是大写 ASCII，见迁移 099/244），
//	  所以 sz_drawersmoke_001 也能剥出 drawersmoke_001；
//	· **只剥一次** —— SZ_SZ_X 剥成 SZ_X（不会一路剥到 X）。
//
// 仓码为空（未选仓 / 捆绑 / 端口未注入）时原样返回：没有前缀就无所谓剥离。
func stripWarehousePrefix(code, warehouseCode string) string {
	trimmed := strings.TrimSpace(code)
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return trimmed
	}
	// 前缀与大写化后的码逐字比对；仓短码是 ASCII（迁移 099 的 code 语义），
	// 因此按字节切与按字符切等价。
	if strings.HasPrefix(strings.ToUpper(trimmed), prefix+skuSeparator) {
		return trimmed[len(prefix)+len(skuSeparator):]
	}
	return trimmed
}

// StripWarehousePrefix 是 stripWarehousePrefix 对本模块 inbound 层（后台页面）的出口。
//
// 页面侧也要剥：新建抽屉的「从仓库选」候选来自库存表的 sku_code，而历史行可能还带着
// 仓码前缀（迁移 262 只清了一次存量）。规则只能有一份实现 —— 页面上再抄一遍，
// 两边就会在边界（大小写 / 双前缀）分叉，表现为「下拉里的编码和落库的不是一个」。
func StripWarehousePrefix(code, warehouseCode string) string {
	return stripWarehousePrefix(code, warehouseCode)
}

// normalizeBundleSKU 捆绑容器主体 SKU（规则 B）。
//
// 捆绑品不存在于仓库，主体由运营自定义，且**恒以 _B 结尾**：
//
//	· 没填（空 / 纯空白）→ 明确报错 ErrBundleSKURequired，**不再静默派生**；
//	· 填了但缺后缀 → 服务端补齐（大小写不敏感）。
//
// 为什么不再派生（用户 2026-09-19 拍板）：SKU 是商品的对外身份，旧实现「留空即按
// <商品段>_B 派生」会让运营**看不见那个编码**就建出了商品 —— 列表、仓库、后续变体都以它
// 为准，事后才发现号不对。现在改由新建抽屉预填建议值（同一套商品段算法，见
// products.html 的 codeSegment）并允许运营修改：编码必须被看见并确认。
// 变体商品保持原状（可留空派生）—— 它的主体常来自「从仓库选 SKU」，不适用本条。
//
// 编辑路径同样只对**显式传入**的 sku 键生效（nil = 不改存量编码，见 product_crud.Update）；
// 显式传空串在更上层就被 ErrContainerSkuInvalid 拦下，所以这里只管「补 _B 后缀」。
func normalizeBundleSKU(custom string) (string, error) {
	code := strings.TrimSpace(custom)
	if code == "" {
		return "", errors.New(productenums.ErrBundleSKURequired)
	}
	if !strings.HasSuffix(strings.ToUpper(code), strings.ToUpper(BundleSKUSuffix)) {
		code += BundleSKUSuffix
	}
	return code, nil
}

// productCodeSegment 商品 slug → SKU 编码里的商品段（大写字母数字）。
//
// 派生不出来（slug 全是非 ASCII，例如中文）时返回空串 —— 由调用方**明确报错**：
// 新规则下不再退回随机码，静默降级会留下无法与仓库对齐的编码。
func productCodeSegment(slug string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(slug) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// —— 归属仓与库存记录（issue #15，端口由 inventory 模块实现）——

// resolveWarehouseRef 解析变体归属仓。
//
// issue #32：商品与库存同属一个模块，这里直接调库存用例（不再是跨模块端口）；
// 未注入时返回 nil —— 表示「不生成库存记录、SKU 不带仓前缀」（纯商品单测路径）。
func (s *Service) resolveWarehouseRef(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error) {
	if s.invSvc == nil {
		return nil, nil
	}
	return s.invSvc.ResolveWarehouse(ctx, projectID, warehouseID)
}

// ensureVariantStock 在归属仓为该 SKU 生成库存记录（已存在则复用）。
//
// 库存真源在库存模块；商品侧不留任何库存副本（issue #32 删掉了缓存列），
// 展示值按需投影、可用量判断一律走真源。未注入 / 未解析到归属仓时空转。
//
// skuCode 传的是**商品侧编码**（带仓码前缀）：剥前缀在这一层做 ——
// 这是商品侧与库存侧的唯一交接点，剥在这里意味着任何调用点都不必自己记得剥
// （少一处记得，就少一处「仓库里出现 SZ_ 开头的东西」）。见 stripWarehousePrefix。
//
// quantity 见 inventory 的签名口径：nil = 不跟踪（无限），非 nil = 跟踪并写入该数量。
func (s *Service) ensureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (err error) {
	if s.invSvc == nil || ref == nil {
		return nil
	}
	return s.invSvc.EnsureVariantStock(ctx, ref, productID, variantID, stripWarehousePrefix(skuCode, ref.Code), quantity)
}

// ensureVariantStockTx 与 ensureVariantStock 相同，但复用**调用方已开启的事务**。
//
// 一次商品保存要落「商品 + 变体 + 各仓库存行 + 变更记录」，库存行是其中一步：
// 走非事务版本会另取一条连接、独立提交，前一步失败时这行已经落库 ——
// 半截状态正是事务要消灭的东西（CQ-026）。
func (s *Service) ensureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (err error) {
	if s.invSvc == nil || ref == nil {
		return nil
	}
	return s.invSvc.EnsureVariantStockTx(ctx, tx, ref, productID, variantID, stripWarehousePrefix(skuCode, ref.Code), quantity)
}

// ensureVariantStockBareWithExternalTx 在指定仓为该**裸码**建库存行并写入该仓的外部编码（事务版）。
//
// 入参已经是**仓库侧裸码**（调用方按认领仓剥好），本函数不再剥：多仓建行时目标仓 ≠ 认领仓，
// 拿目标仓的仓码去剥认领仓的前缀是剥不掉的（SZ_X 在上海仓会原样留下 SZ_ 前缀）。
// 单仓路径用 ensureVariantStockTx（接受商品侧编码、内部剥前缀）—— 两个名字把这个差别写在脸上。
//
// external_sku 只作用于**新建**那一行（已存在的行沿用既有值，覆盖走 BindExternalSKU）：
// 「认领」路径下这一行本来就属于别的商品，商品侧不改它的外码。
func (s *Service) ensureVariantStockBareWithExternalTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, bareCode, externalSKU string, quantity *int) (err error) {
	if s.invSvc == nil || ref == nil {
		return nil
	}
	return s.invSvc.EnsureVariantStockWithExternalTx(ctx, tx, ref, productID, variantID, bareCode, externalSKU, quantity)
}

// refCode 归属仓短码（ref 为 nil 时为空串，即「无仓前缀」路径）。
func refCode(ref *productcontract.WarehouseRef) string {
	if ref == nil {
		return ""
	}
	return ref.Code
}

// defaultFloat 可空浮点取默认。
func defaultFloat(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}
