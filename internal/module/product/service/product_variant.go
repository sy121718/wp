package productservice

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
//	· 变体 SKU = <容器主体 SKU>_<属性值段…>_V（属性段拼接在本文件的组合生成里）。
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

// 本文件落地本票四条验收：
//  1. 勾选若干属性值后生成全部组合 —— 对「参与变体」的属性组做笛卡尔积；
//     已存在的规格组合（含同一次请求里重复勾选的值）直接跳过，不产生重复变体；
//  2. 维度与数量上限保护 —— 参与维度或组合总数超上限时**整体拒绝**（一条都不写），
//     错误消息带上限与实际值；
//  3. 新变体逐字段继承商品级默认值 —— 生成路径复用 newVariantFromDefaults
//     （唯一填充入口：只填空字段，「空」以 NULL 判定，0 与 false 视为已填）；
//  4. 无表单路径同样走判空继承 —— 不传勾选（批量生成 / 导入 / 接口）时按
//     「全部参与变体的属性组 × 全部启用值」生成，填充规则与表单路径一字不差。
//
// 组合的载体（商品恒有至少一个变体，见 product_service.go）：商品创建时生成的
// 首个变体没有规格（option_values = {}）。生成组合时它**就地承接第一个组合**
// （只补 option_values，已填字段一个都不覆盖），其余组合新建 —— 否则商品会多出
// 一条无规格的悬挂变体，前台规格选择器也随之多出一行。
//
// 本文件是纯业务编排：组合算法与归一在此，持久化只走 model 的具名方法。

// 这是 VariantAvailabilityPort 面向访问面的包装：调用方给出**工程作用域**（访问面片段从
// URL 参数取 projectId，与 productList 片段同一口径；购物车从请求带的工程取），本方法在该
// 工程的作用域内查库存真源 —— 不属于该工程的变体由策略天然查不到，不需要「按变体反查工程」
// 那一步（反查要先读有策略的 products，正是死结所在）。
//
// 口径说明（与 productList 一致）：片段参数可被篡改，但商品与可用量本就是公开数据，
// 越权面仅限「读到别的工程同样公开的库存数字」；真正的把关在结算写路径。
//
// 三条刻意的口径：
//
//	· **只读真源**：可用量经 s.availability（inventory 实现）读 inventory_stocks，
//	  绝不读 product_variants.stock_total 缓存（那是展示缓存，滞后且可被改；spec 死线）；
//	· **尽力而为**：端口未注入（纯商品单测 / inventory 未装配）时返回空结果而不是报错 ——
//	  调用方据此渲染「以结算时库存为准」，而不是把一个读不到库存的页面变成 500；
//	  （写路径上的加购校验另有 fail-closed 的判定，不靠这里兜底）
//	· **限量**：一次最多查 VariantAvailabilityLookupMaxIDs 个变体，超出即截断（GET 片段不报错）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/inventory/contract"
	"go_wp/internal/module/masterdata/contract"
	"go_wp/internal/module/masterdata/enums"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
	"go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
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
		// 静态产物失效（审计 ARCH-01）：变体是商品聚合的一部分，它的可见字段
		//（价格 / 规格 / 启用状态）直接进详情页与列表页的字节 → 失效目标是**商品**。
		if xerr := s.enqueueProductInvalidationTx(ctx, tx, p.ProjectID, p.ID); xerr != nil {
			return xerr
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
		v.Image = mediaURL(*req.Image)
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
		// 静态产物失效（审计 ARCH-01）：改价 / 改启用状态都会改详情页与列表页的字节。
		if xerr := s.enqueueProductInvalidationTx(ctx, tx, projectID, v.ProductID); xerr != nil {
			return xerr
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
		// 静态产物失效（审计 ARCH-01）：删变体会改商品的价格区间与规格展示。
		if xerr := s.enqueueProductInvalidationTx(ctx, tx, projectID, v.ProductID); xerr != nil {
			return xerr
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
			v.Image = mediaURL(req.Image)
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

const (
	// MaxVariationDimensions 参与笛卡尔积的属性组数量上限（维度保护）。
	MaxVariationDimensions = 4
	// MaxVariantCombinations 单次生成的组合总数上限（数量保护）。
	MaxVariantCombinations = 200
)

// optionPair 组合里的一个「属性组 key → 属性值 key」。
type optionPair struct {
	Key   string
	Value string
}

// variationDimension 一个参与组合的维度：属性组 + 本次实际使用的值。
type variationDimension struct {
	group  *productmodel.ProductAttributeEntity
	values []productdto.AttributeValueResp
}

// GenerateVariants 按勾选的属性值生成全部变体组合（幂等：已存在的组合跳过）。
func (s *Service) GenerateVariants(ctx context.Context, req *productdto.GenerateVariantsReq) (res *productdto.GenerateVariantsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return nil, gerr
	}
	p, gerr := s.m.Get(ctx, req.ProductID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs), projectID)
	if aerr != nil {
		return nil, aerr
	}
	dims, derr := buildVariationDimensions(p, attrs, req.Selections)
	if derr != nil {
		return nil, derr
	}
	total := combinationCount(dims)
	if total > MaxVariantCombinations {
		return nil, fmt.Errorf("%s：%s", productenums.ErrVariationCountLimit,
			i18n.ErrorDetail(productenums.DetailVariationCountExceed,
				"n", strconv.Itoa(total), "max", strconv.Itoa(MaxVariantCombinations)))
	}

	// 归属仓（issue #15）：本批变体统一落在该仓（不选则默认仓），短码参与 SKU 编码，
	// 落库后在同一个仓为每个新建变体生成初始 0 的库存记录。解析失败即整体拒绝。
	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	now := time.Now().UTC()
	existing, lerr := s.m.ListVariants(ctx, p.ID)
	if lerr != nil {
		return nil, lerr
	}
	// 已有的规格组合（option_values 非空）与服务层已占用的 SKU 编码：
	// 组合去重与 SKU 唯一都在内存里判定，落库时一次性写。
	taken := map[string]bool{}
	skus := map[string]bool{}
	var carrier *productmodel.VariantEntity
	for _, v := range existing {
		if v.SKUCode != "" {
			skus[v.SKUCode] = true
		}
		pairs := decodeOptionPairs(v.OptionValues)
		if len(pairs) == 0 {
			// 无规格变体（商品创建时的首个变体）：取最早的一条作为组合载体。
			if carrier == nil || v.CreatedAt.Before(carrier.CreatedAt) {
				carrier = v
			}
			continue
		}
		taken[optionKey(pairs)] = true
	}

	// 容器主体 SKU（规则 A 的主体段）：与「保存清单」路径共用同一条解析
	//（预览、生成、保存三处的容器段必须逐字一致，否则同一组合会给出两种 SKU）。
	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}
	// 属性段的固定顺序：按商品引用属性组的先后（与点选顺序无关）。
	groupOrder := attributeGroupOrder(p, attrs)

	res = &productdto.GenerateVariantsResp{
		ProductID: p.ID, Total: total, Variants: []*productdto.VariantResp{},
	}
	var updated, created []*productmodel.VariantEntity
	// issue #19：组合载体（无规格变体被首个组合承接）改的是同一个变体的 option_values，
	// 改前快照必须先取 —— 它随后会被就地改写。载体至多一个，故一份快照即可。
	var carrierBefore masterdatacontract.FieldSnapshot
	if carrier != nil {
		carrierBefore = variantChangeSnapshot(carrier, nil)
	}
	for i, pairs := range expandCombinations(dims) {
		key := optionKey(pairs)
		if taken[key] {
			// 重复勾选 / 重复提交：组合已存在，原样跳过。
			res.Skipped++
			continue
		}
		taken[key] = true
		raw := encodeOptionPairs(pairs)
		if carrier != nil {
			carrier.OptionValues = raw
			carrier.UpdatedAt = now
			updated = append(updated, carrier)
			// 载体只承接一个组合，后续组合一律新建。
			carrier = nil
			res.Adopted++
			continue
		}
		// 本批的 SKU 编码逐个探测：taken 传入已占用的编码集合，
		// 否则同一批里的新变体都会取到同一个「下一个序号」而互相撞号。
		v, verr := s.newVariantFromDefaults(ctx, p, &productdto.CreateVariantReq{
			ProductID: p.ID, OptionValues: raw, Sort: i,
		}, refCode(ref), skus)
		if verr != nil {
			return nil, verr
		}
		// 规则 A：变体 SKU = <容器主体 SKU>_<属性值段…>_V。
		// 属性段顺序固定（按属性组的既定顺序），因此同一组合无论怎么点选都是同一个 SKU。
		v.SKUCode = uniqueVariantSKU(variantSKUCode(container, raw, groupOrder), skus)
		skus[v.SKUCode] = true
		created = append(created, v)
	}
	// issue #19：本批新增的变体逐条留痕（含默认发货仓与 SKU 编码）；
	// 被承接的载体记一条修改记录（规格组合由空变为具体组合）。
	changeInputs := make([]*masterdatacontract.ChangeInput, 0, len(created)+len(updated))
	for _, v := range updated {
		changeInputs = append(changeInputs, variantChangeInput(p.ProjectID, v, masterdataenums.ActionUpdate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, carrierBefore, variantChangeSnapshot(v, nil)))
	}
	for _, v := range created {
		changeInputs = append(changeInputs, variantChangeInput(p.ProjectID, v, masterdataenums.ActionCreate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, nil, variantChangeSnapshot(v, ref)))
	}
	// 变体清单、各仓库存行、变更记录三处在**同一个事务**里：一次「生成组合」要么整批落库、
	// 要么一行都不落 —— 半截状态会让前台的规格选择器少行（AGENTS.md「写操作的事务与回滚」）。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, p.ProjectID); serr != nil {
			return serr
		}
		if xerr := s.m.SaveVariantsTx(tx, updated, created); xerr != nil {
			return xerr
		}
		// 验收 2：每个新建变体都在归属仓有一条库存记录（初始 0）——
		// 商品创建时的首个变体已在此路径外生成过，这里补的是本批新组合。
		// 数量不填（nil = 不跟踪 = 无限）：这条路径没有数量输入。
		for _, v := range created {
			if xerr := s.ensureVariantStockTx(ctx, tx, ref, p.ID, v.ID, v.SKUCode, nil); xerr != nil {
				return xerr
			}
		}
		return s.recordChangesTx(ctx, tx, changeInputs...)
	}); err != nil {
		return nil, err
	}
	res.Created = len(created)
	// 重算时机之一：变体写操作后 —— 组合生成会新建一整批变体，价格整体变化。
	if err = s.recalcProjectAutoTags(ctx, p.ID, p.ProjectID); err != nil {
		return nil, err
	}

	after, rerr := s.m.ListVariants(ctx, p.ID)
	if rerr != nil {
		return nil, rerr
	}
	for _, v := range after {
		res.Variants = append(res.Variants, toVariantResp(v))
	}
	// 库存展示值查询期投影（issue #32）。
	s.fillVariantStock(ctx, p.ProjectID, res.Variants)
	return res, nil
}

// buildVariationDimensions 组装本次参与组合的维度（顺序 = 商品引用属性组的顺序）。
//
// 规则见 GenerateVariantsReq 注释。勾选里出现非法引用一律报错而不是静默忽略 ——
// 静默忽略会让调用方以为「已经生成」，实际什么都没生成。
func buildVariationDimensions(p *productmodel.ProductEntity, attrs []*productmodel.ProductAttributeEntity, selections []productdto.VariantSelectionReq) (dims []variationDimension, err error) {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	picked := map[string][]string{}
	seenSel := map[string]bool{}
	for _, sel := range selections {
		id := strings.TrimSpace(sel.AttributeID)
		if id == "" {
			continue
		}
		picked[id] = append(picked[id], sel.ValueIDs...)
		if seenSel[id] {
			continue
		}
		seenSel[id] = true
		if a, ok := byID[id]; !ok || !a.IsVariation {
			return nil, fmt.Errorf("%s：%s", productenums.ErrVariationAttributeInvalid,
				i18n.ErrorDetail(productenums.DetailVariationAttributeUnreferenced, "attribute", id))
		}
	}
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok || !a.IsVariation {
			continue
		}
		enabled := enabledAttributeValues(a)
		if len(enabled) == 0 {
			// 组内没有启用的值：参与不了组合，跳过（不是错误）。
			continue
		}
		wanted, hit := picked[id]
		if !hit {
			// 这次没勾这个组 = 该维度取全部启用值（不缩小范围）。
			// 语义上等价于「无表单路径」，但即使其它组被勾选也保持一致 ——
			// 生成的组合恒覆盖全部参与变体的维度，不会产出只有部分维度的
			// 「半截组合」（那种组合在前台规格选择器里根本选不到）。
			dims = append(dims, variationDimension{group: a, values: enabled})
			continue
		}
		chosen, cerr := pickAttributeValues(a, enabled, wanted)
		if cerr != nil {
			return nil, cerr
		}
		if len(chosen) == 0 {
			continue
		}
		dims = append(dims, variationDimension{group: a, values: chosen})
	}
	if len(dims) == 0 {
		return nil, errors.New(productenums.ErrVariationNoDimension)
	}
	if len(dims) > MaxVariationDimensions {
		return nil, fmt.Errorf("%s：%s", productenums.ErrVariationDimensionLimit,
			i18n.ErrorDetail(productenums.DetailVariationDimensionExceed,
				"n", strconv.Itoa(len(dims)), "max", strconv.Itoa(MaxVariationDimensions)))
	}
	return dims, nil
}

// enabledAttributeValues 组内启用的值（按组定义顺序；历史纯字符串数组由归一兜底）。
func enabledAttributeValues(a *productmodel.ProductAttributeEntity) (out []productdto.AttributeValueResp) {
	out = []productdto.AttributeValueResp{}
	for _, v := range normalizeValuesFromRaw(a.Values) {
		if v.Enabled {
			out = append(out, v)
		}
	}
	return out
}

// pickAttributeValues 从启用值里挑出被勾选的那些（去重 + 保持组内定义顺序）。
//
// 勾选到组内不存在或已停用的值时报错（不静默丢弃：调用方需要知道哪一条没生效）。
func pickAttributeValues(a *productmodel.ProductAttributeEntity, enabled []productdto.AttributeValueResp, wanted []string) (out []productdto.AttributeValueResp, err error) {
	enabledSet := make(map[string]bool, len(enabled))
	for _, v := range enabled {
		enabledSet[v.ID] = true
	}
	want := map[string]bool{}
	for _, id := range wanted {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !enabledSet[id] {
			return nil, fmt.Errorf("%s：%s", productenums.ErrVariationValueInvalid,
				i18n.ErrorDetail(productenums.DetailVariationValueNotEnabled, "attribute", a.Key, "value", id))
		}
		want[id] = true
	}
	out = []productdto.AttributeValueResp{}
	for _, v := range enabled {
		if want[v.ID] {
			out = append(out, v)
		}
	}
	return out, nil
}

// combinationCount 组合总数（各维度取值数之积）。
func combinationCount(dims []variationDimension) int {
	total := 1
	for _, d := range dims {
		total *= len(d.values)
	}
	return total
}

// expandCombinations 展开笛卡尔积：最后一维变化最快（与常见规格表的阅读顺序一致）。
func expandCombinations(dims []variationDimension) (out [][]optionPair) {
	if len(dims) == 0 {
		return nil
	}
	idx := make([]int, len(dims))
	for {
		pairs := make([]optionPair, len(dims))
		for i, d := range dims {
			pairs[i] = optionPair{Key: d.group.Key, Value: attributeValueCode(d.values[idx[i]])}
		}
		out = append(out, pairs)
		i := len(dims) - 1
		for i >= 0 {
			idx[i]++
			if idx[i] < len(dims[i].values) {
				break
			}
			idx[i] = 0
			i--
		}
		if i < 0 {
			break
		}
	}
	return out
}

// decodeOptionPairs 读变体的 option_values（jsonb 对象）为组合对。
//
// 只接受「字符串 → 字符串」的对象形态；空对象、数组、字符串等历史形态一律视为
// 「无规格」（返回空），由生成逻辑决定是否把它当作组合载体。
func decodeOptionPairs(raw json.RawMessage) []optionPair {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return nil
	}
	out := make([]optionPair, 0, len(m))
	for k, v := range m {
		out = append(out, optionPair{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// optionKey 组合的规范化键（按属性组 key 排序）。
//
// 排序保证「写入顺序不同的同一条组合」判定为同一个键 —— 这是幂等去重的依据。
func optionKey(pairs []optionPair) string {
	sorted := make([]optionPair, len(pairs))
	copy(sorted, pairs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	var b strings.Builder
	for i, p := range sorted {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(p.Value)
	}
	return b.String()
}

// encodeOptionPairs 组合 → jsonb 原始字节（保持维度顺序：落库与展示都确定）。
func encodeOptionPairs(pairs []optionPair) json.RawMessage {
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(p.Key)
		v, _ := json.Marshal(p.Value)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

// —— SKU 编码新规则：变体 SKU 的拼接（规则 A，2026-09-19）——

// attributeGroupOrder 属性组的固定顺序（属性组 key → 序号）。
//
// 顺序取**商品引用属性组的先后**（products.attribute_ids），这就是「属性组的既定顺序」：
// 与运营点选的先后、与 option_values 的写入顺序都无关。SKU 属性段按它排序，保证
// 「顺序不同但组合相同」得到同一个 SKU。
func attributeGroupOrder(p *productmodel.ProductEntity, attrs []*productmodel.ProductAttributeEntity) map[string]int {
	byID := make(map[string]*productmodel.ProductAttributeEntity, len(attrs))
	for _, a := range attrs {
		byID[a.ID] = a
	}
	order := make(map[string]int, len(attrs))
	next := 0
	for _, id := range decodeStrings(p.AttributeIDs) {
		a, ok := byID[id]
		if !ok {
			continue
		}
		if _, dup := order[a.Key]; dup {
			continue
		}
		order[a.Key] = next
		next++
	}
	// 商品没显式引用的组（理论上不会出现在 option_values 里）排到最后，避免漏项被当成「第 0 位」。
	for _, a := range attrs {
		if _, ok := order[a.Key]; !ok {
			order[a.Key] = next
			next++
		}
	}
	return order
}

// variantSKUCode 变体 SKU = <容器主体 SKU>_<属性值段…>_V（规则 A）。
//
// 属性段用**属性值的标识 key**（不是属性组的 key，也不用中文显示名）；顺序固定为属性组的
// 既定顺序（groupOrder），组内按属性值排序字段 —— 点选顺序不参与，因此同一组合恒得同一 SKU。
// 无规格（option_values 为空）时返回容器主体本身。
func variantSKUCode(container string, optionValues json.RawMessage, groupOrder map[string]int) string {
	pairs := decodeOptionPairs(optionValues)
	if len(pairs) == 0 {
		return container
	}
	pairs = sortedOptionPairs(pairs, groupOrder)
	segments := make([]string, 0, len(pairs))
	for _, p := range pairs {
		segments = append(segments, skuSegment(p.Value))
	}
	return container + skuSeparator + strings.Join(segments, skuSeparator) + VariantSKUSuffix
}

// uniqueVariantSKU 保证变体 SKU 在**商品内**唯一（UNIQUE (product_id, sku_code)）。
//
// 组合去重已保证不同组合得到不同属性段，这里只兜底极端情况（两个值 key 归一化后同段）：
// 在 _V 后缀前插入 _2 / _3 …（不引入随机段，保序且可读）。
func uniqueVariantSKU(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	body := strings.TrimSuffix(base, VariantSKUSuffix)
	for i := 2; i < maxSKUProbe; i++ {
		candidate := fmt.Sprintf("%s%s%d%s", body, skuSeparator, i, VariantSKUSuffix)
		if !taken[candidate] {
			return candidate
		}
	}
	return base
}

// containerSKUOfVariants 存量商品的主体 SKU 回落：取最早创建的那条变体
// （商品创建时的无规格变体，其 SKU 就是当时的容器主体）。
//
// 存量编码**不重写** —— 新生成的变体以它为主体拼接，这是「存量 SKU 一律不重写」的
// 必然结果：主体段保持老编码，只有新增的变体套用新格式。
func containerSKUOfVariants(variants []*productmodel.VariantEntity) string {
	var earliest *productmodel.VariantEntity
	for _, v := range variants {
		if strings.TrimSpace(v.SKUCode) == "" {
			continue
		}
		if earliest == nil || v.CreatedAt.Before(earliest.CreatedAt) ||
			(v.CreatedAt.Equal(earliest.CreatedAt) && v.ID < earliest.ID) {
			earliest = v
		}
	}
	if earliest == nil {
		return ""
	}
	return earliest.SKUCode
}

// attributeValueCode 属性值在规格组合里的编码（写进 option_values 的值）。
//
// 优先用属性值的**标识 key**（key 是标识不是展示文本，筛选按它匹配）；key 为空时用 id 短码
// 兜底 —— 否则空 key 的多条值会在 option_values 里互相覆盖，组合去重也会把它们当成同一个。
// 这里**不做 ASCII 归一化**：option_values 的取值语义必须与筛选口径逐字一致，
// ASCII 化只发生在拼 SKU 段时（skuSegment）。
func attributeValueCode(v productdto.AttributeValueResp) string {
	if code := strings.TrimSpace(v.Key); code != "" {
		return code
	}
	return "v" + shortHash(v.ID)
}

// skuSegment 属性值的 SKU 段：标识 key 是纯 ASCII 时原样用（可读、可与仓库对齐）；
// 否则用确定性短码兜底，保证生成的 SKU **全 ASCII**（中文显示名不进编码）。
func skuSegment(value string) string {
	v := strings.TrimSpace(value)
	if v != "" && isSKUASCII(v) {
		return v
	}
	return "v" + shortHash(v)
}

// isSKUASCII 段是否只含 SKU 允许的 ASCII 字符（字母数字与 . _ -）。
func isSKUASCII(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// shortHash FNV-1a 32 位哈希的 8 位十六进制（跨进程稳定的确定性短码）。
func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// —— 变体清单：预览—保存模型（docs/14 §8，2026-09-19 用户拍板）——
//
// 用户口径：「变体 sku 也是一样系统生产 + 可编辑，生产只是显示，并不会存入数据库，
// 必须保存才行，所以删除不是删除，是相当于清除前端显示，不存入数据库」。
//
// 本段是那句话的落地：
//   · 生成（PreviewVariantCombinations）—— 只算不写，把笛卡尔积按组合去重后交回前端清单；
//   · 删除（前端行为）—— 只是从清单里移出，服务端没有「删一行」的入口；
//   · 保存（SaveVariantList）—— 以清单为准：新增缺失的组合、更新改过的 SKU、
//     删除清单外的既有变体；清单外要删的行若**有非零库存或被 BOM 引用**则跳过，
//     逐条回带原因（不整批失败、不静默）。
//
// 与 GenerateVariants 的关系：后者是「直接落库」的接口路径（导入 / 接口 / 批量生成），
// 语义一字未改；本段服务的是后台抽屉的预览—保存交互。

// PreviewVariantCombinations 组合生成的预览：把勾选值的笛卡尔积与「库里已有的组合 +
// 前端清单已有的组合」去重后返回待追加的行（**一个字节都不写库**）。
//
// 与 GenerateVariants 共用同一套维度归一（buildVariationDimensions）、上限保护、
// 容器主体解析（resolveContainerSKU）与 SKU 拼接（variantSKUCode / uniqueVariantSKU）——
// 预览里看到的 SKU 就是保存后落库的那个 SKU，两处各写一套必然分叉。
func (s *Service) PreviewVariantCombinations(ctx context.Context, req *productdto.PreviewVariantReq) (res *productdto.PreviewVariantResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return nil, gerr
	}
	p, gerr := s.m.Get(ctx, req.ProductID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs), projectID)
	if aerr != nil {
		return nil, aerr
	}
	dims, derr := buildVariationDimensions(p, attrs, req.Selections)
	if derr != nil {
		return nil, derr
	}
	total := combinationCount(dims)
	if total > MaxVariantCombinations {
		return nil, fmt.Errorf("%s：%s", productenums.ErrVariationCountLimit,
			i18n.ErrorDetail(productenums.DetailVariationCountExceed,
				"n", strconv.Itoa(total), "max", strconv.Itoa(MaxVariantCombinations)))
	}
	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	existing, lerr := s.m.ListVariants(ctx, p.ID)
	if lerr != nil {
		return nil, lerr
	}
	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}
	groupOrder := attributeGroupOrder(p, attrs)
	// 已占用的 SKU 编码（库里的全部 + 本批已算出的）与已出现过的组合：
	// 前者保证预览里的 SKU 不会撞号，后者保证「重复点生成不重复追加」。
	taken := make(map[string]bool, len(existing))
	seen := map[string]bool{}
	for _, v := range existing {
		if code := strings.TrimSpace(v.SKUCode); code != "" {
			taken[code] = true
		}
		seen[optionKey(decodeOptionPairs(v.OptionValues))] = true
	}
	for _, raw := range req.ExistingOptionValues {
		if len(raw) == 0 {
			continue
		}
		seen[optionKey(decodeOptionPairs(raw))] = true
	}
	res = &productdto.PreviewVariantResp{ProductID: p.ID, Total: total, Rows: []*productdto.VariantPreviewRow{}}
	for _, pairs := range expandCombinations(dims) {
		key := optionKey(pairs)
		if seen[key] {
			// 库里已有该组合（抽屉打开时的初始行就是它）或清单里已有：不重复追加。
			res.Skipped++
			continue
		}
		seen[key] = true
		raw := encodeOptionPairs(pairs)
		code := uniqueVariantSKU(variantSKUCode(container, raw, groupOrder), taken)
		taken[code] = true
		res.Rows = append(res.Rows, &productdto.VariantPreviewRow{
			OptionValues: raw, OptionKey: key, SKUCode: code,
		})
	}
	return res, nil
}

// SaveVariantList 以清单为准保存变体（本流程**唯一的落库动作**）。
//
// 服务端不信任前端提交的清单形状，逐条重算：
//   - 新增行的规格组合必须由服务端校验（属性组属于该商品、取值在组内启用值集合里、
//     键序按属性组固定顺序归一）；既有行则以**库里的 option_values** 为准
//     （历史组合可能引用了后来被删的组 / 值，用前端传的组合判定会让老数据卡住保存）；
//   - SKU 逐段规范化（normalizeVariantSKU），为空或规范化后为空一律报
//     ErrVariantSKUEmpty；唯一性按 product_id + sku_code（前端提交的值不作数）；
//   - 删除清单外的既有变体前先查引用面（四个：非零库存 / BOM 引用 / 捆绑成员引用 /
//     有过库存流水），命中任一即跳过该行并逐条回带原因，不整批失败。
func (s *Service) SaveVariantList(ctx context.Context, req *productdto.SaveVariantListReq) (res *productdto.SaveVariantListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, gerr := s.resolveProjectID(ctx, req.ProjectID)
	if gerr != nil {
		return nil, gerr
	}
	p, gerr := s.m.Get(ctx, req.ProductID, projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(p.AttributeIDs), projectID)
	if aerr != nil {
		return nil, aerr
	}
	existing, lerr := s.m.ListVariants(ctx, p.ID)
	if lerr != nil {
		return nil, lerr
	}
	ref, werr := s.resolveWarehouseRef(ctx, p.ProjectID, req.WarehouseID)
	if werr != nil {
		return nil, werr
	}
	container, cerr := resolveContainerSKU(p, existing, refCode(ref))
	if cerr != nil {
		return nil, cerr
	}
	groupOrder := attributeGroupOrder(p, attrs)
	allowed := variationValueIndex(attrs)

	byID := make(map[string]*productmodel.VariantEntity, len(existing))
	// owner 是 SKU 编码的占用者（变体 id）：清单里两行不能落到同一个编码上。
	owner := make(map[string]string, len(existing))
	for _, v := range existing {
		byID[v.ID] = v
		if code := strings.TrimSpace(v.SKUCode); code != "" {
			owner[code] = v.ID
		}
	}
	now := time.Now().UTC()
	res = &productdto.SaveVariantListResp{ProductID: p.ID, Skipped: []productdto.VariantSaveSkip{}}
	kept := make(map[string]bool, len(req.Rows))
	seenKeys := make(map[string]bool, len(req.Rows))
	var updated, created []*productmodel.VariantEntity
	changes := make([]*masterdatacontract.ChangeInput, 0, len(req.Rows))
	for i, row := range req.Rows {
		if vid := strings.TrimSpace(row.VariantID); vid != "" {
			v := byID[vid]
			if v == nil {
				// 清单里的既有变体已经被别处删掉（并发删除）：跳过并说明，不当成新增行。
				res.Skipped = append(res.Skipped, productdto.VariantSaveSkip{
					VariantID: vid, Reason: productenums.VariantSkipVariantMissing,
				})
				continue
			}
			key := optionKey(decodeOptionPairs(v.OptionValues))
			if seenKeys[key] {
				res.Skipped = append(res.Skipped, variantSkipOf(v, productenums.VariantSkipDuplicated, ""))
				continue
			}
			seenKeys[key] = true
			kept[v.ID] = true
			code := normalizeVariantSKU(row.SKUCode)
			if code == "" {
				return nil, errors.New(productenums.ErrVariantSKUEmpty)
			}
			if code == v.SKUCode {
				// 没改 SKU：这一行一个字都不写（也不留痕）。
				continue
			}
			if id, taken := owner[code]; taken && id != v.ID {
				return nil, errors.New(productenums.ErrSkuTaken)
			}
			if old := strings.TrimSpace(v.SKUCode); old != "" {
				delete(owner, old)
			}
			owner[code] = v.ID
			before := variantChangeSnapshot(v, nil)
			v.SKUCode = code
			v.UpdatedAt = now
			updated = append(updated, v)
			changes = append(changes, variantChangeInput(p.ProjectID, v, masterdataenums.ActionUpdate,
				masterdataenums.OriginVariantGenerate, req.OperatorID, before, variantChangeSnapshot(v, nil)))
			res.Updated++
			continue
		}
		pairs, perr := normalizeListOptionPairs(row.OptionValues, allowed, groupOrder)
		if perr != nil {
			return nil, perr
		}
		raw := encodeOptionPairs(pairs)
		key := optionKey(pairs)
		if seenKeys[key] {
			res.Skipped = append(res.Skipped, productdto.VariantSaveSkip{
				SKUCode: strings.TrimSpace(row.SKUCode), OptionValues: raw,
				Reason: productenums.VariantSkipDuplicated,
			})
			continue
		}
		seenKeys[key] = true
		code := normalizeVariantSKU(row.SKUCode)
		if code == "" {
			// 清单里没给（或给成了空）：按变体 SKU 规则由系统生成。
			code = variantSKUCode(container, raw, groupOrder)
		}
		if strings.TrimSpace(code) == "" {
			return nil, errors.New(productenums.ErrVariantSKUEmpty)
		}
		if _, taken := owner[code]; taken {
			return nil, errors.New(productenums.ErrSkuTaken)
		}
		v, verr := s.newVariantFromDefaults(ctx, p, &productdto.CreateVariantReq{
			ProductID: p.ID, OptionValues: raw,
		}, refCode(ref), nil)
		if verr != nil {
			return nil, verr
		}
		v.SKUCode = code
		v.Sort = i
		owner[code] = v.ID
		created = append(created, v)
		changes = append(changes, variantChangeInput(p.ProjectID, v, masterdataenums.ActionCreate,
			masterdataenums.OriginVariantGenerate, req.OperatorID, nil, variantChangeSnapshot(v, ref)))
		res.Created++
	}

	// 清单外要删的既有变体：先查引用面（库存 / BOM），跳过的行逐条记原因。
	type pendingDelete struct {
		v      *productmodel.VariantEntity
		before masterdatacontract.FieldSnapshot
	}
	var deletions []pendingDelete
	for _, v := range existing {
		if kept[v.ID] {
			continue
		}
		reason, detail, rerr := s.variantDeleteBlockReason(ctx, p.ProjectID, v)
		if rerr != nil {
			return nil, rerr
		}
		if reason != "" {
			res.Skipped = append(res.Skipped, variantSkipOf(v, reason, detail))
			continue
		}
		deletions = append(deletions, pendingDelete{v: v, before: variantChangeSnapshot(v, nil)})
	}

	// 写：新增 / 更新 / 库存行 / 逐条删除 / 留痕全部在**同一个事务**里 ——
	// 「清单外的行要删、清单里的行要建」是一个整体动作，中途失败留下「删了没建」或
	// 「建了没删」的半截清单，正是这个模型要避免的（AGENTS.md「写操作的事务与回滚」）。
	// 被引用 / 有库存的行在进入这里之前已经逐条跳过（res.Skipped），不构成中途失败。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, p.ProjectID); serr != nil {
			return serr
		}
		if xerr := s.m.SaveVariantsTx(tx, updated, created); xerr != nil {
			return xerr
		}
		for _, v := range created {
			if xerr := s.ensureVariantStockTx(ctx, tx, ref, p.ID, v.ID, v.SKUCode, nil); xerr != nil {
				return xerr
			}
		}
		for _, item := range deletions {
			if xerr := s.m.DeleteVariantTx(ctx, tx, item.v.ID); xerr != nil {
				return xerr
			}
			changes = append(changes, variantChangeInput(p.ProjectID, item.v, masterdataenums.ActionDelete,
				masterdataenums.OriginVariantGenerate, req.OperatorID, item.before, nil))
			res.Deleted++
		}
		return s.recordChangesTx(ctx, tx, changes...)
	}); err != nil {
		return nil, err
	}
	// 重算时机之一：变体写操作后（价格与规格集合都可能变了）。
	if err = s.recalcProjectAutoTags(ctx, p.ID, p.ProjectID); err != nil {
		return nil, err
	}
	after, rerr := s.m.ListVariants(ctx, p.ID)
	if rerr != nil {
		return nil, rerr
	}
	for _, v := range after {
		res.Variants = append(res.Variants, toVariantResp(v))
	}
	s.fillVariantStock(ctx, p.ProjectID, res.Variants)
	return res, nil
}

// variantSkipOf 组装「跳过一行」的回带信息（原因取 enums 常量 = i18n key）。
//
// detail 是**可定位明细**（引用面 / 工程 / 商品 id），与 Reason 分开两个字段：
// Reason 只承载受控原因枚举（提示页直接展示）；自由文本若拼进 Reason 会让提示页出现
// 不可控内容，所以明细只能走响应体里的 Detail，不能拼进 Reason。反之单条删除路径
//（DeleteVariant）返回的是错误，那里按 key：detail 的既有形态拼接，明细照常可见。
func variantSkipOf(v *productmodel.VariantEntity, reason, detail string) productdto.VariantSaveSkip {
	if v == nil {
		return productdto.VariantSaveSkip{Reason: reason, Detail: detail}
	}
	return productdto.VariantSaveSkip{
		VariantID: v.ID, SKUCode: v.SKUCode,
		OptionValues: orJSON(v.OptionValues, "{}"), Reason: reason, Detail: detail,
	}
}

// variantDeleteBlockReason 变体删除的守卫：命中**四个引用面**之一时返回原因
// （enums 常量 = i18n key）+ 可定位明细（detail 为空表示该原因没有额外定位信息）。
//
//	① 库存非零（inventory_stocks；已有）—— 该 SKU 还有货，先处理库存或改为停用；
//	② BOM 引用（inventory_bom_items 的 component；已有）—— 先解除引用；
//	③ 捆绑成员引用（products.bundle_items.options[].variantId）——
//	   删掉它会让那些捆绑套餐的成员指向一个不存在的变体；**必须跨工程可发现**：
//	   别的工程把本工程的变体列为捆绑成员时，只在本工程里查会命中 0 行 ⇒ 删除放行 ⇒
//	   那些捆绑的成员清单永久悬空（审计 DB-03 §1.2 的守卫盲区）；
//	④ 有过任何库存流水（inventory_stock_movements）—— 订单一旦建单就必然产生
//	   扣减流水，所以「有流水」等价于「被订单用过」；历史单据按 variant_id 追溯，不允许硬删。
//
// ①②在 inventory 侧（同模块直调 model 具名方法，未注入时不拦 —— 纯商品单测路径）；
// ③在 product 侧（本 module 的表，恒可查，且跨工程可见性由逐工程作用域枚举取得，
// 见 model/product_ref_scan.go）；④经库存用例（契约方法，未注入时不拦）。
//
// ①②④仍按**单个工程作用域**查（这些表都在迁移 215 名单里，缺作用域会静默返回
// 「没被引用」）。它们的跨工程面属库存域，不在本票范围（DB-03 §6 第 7 条已记录）。
func (s *Service) variantDeleteBlockReason(ctx context.Context, projectID string, v *productmodel.VariantEntity) (reason, detail string, err error) {
	if v == nil {
		return "", "", nil
	}
	if s.inv != nil {
		n, cerr := s.inv.CountNonZeroStocksByVariant(ctx, v.ID, projectID)
		if cerr != nil {
			return "", "", cerr
		}
		if n > 0 {
			return productenums.VariantSkipHasStock, "", nil
		}
		hasParent, berr := s.inv.HasBOMParent(ctx, v.ID, projectID)
		if berr != nil {
			return "", "", berr
		}
		if hasParent {
			return productenums.VariantSkipReferenced, "", nil
		}
	}
	// ③ 捆绑成员引用：跨工程扫描（jsonb 包含判断下推，见 model.ProductRefsByBundleVariant 的注释）。
	ref, rerr := s.crossProjectRefs(ctx, func(ids []string) (*productmodel.CrossProjectRef, error) {
		return s.m.ProductRefsByBundleVariant(ctx, v.ID, ids)
	})
	if rerr != nil {
		return "", "", rerr
	}
	if ref.Referenced() {
		return productenums.VariantSkipBundleReferenced, refGuardDetail(ref), nil
	}
	// ④ 库存流水（被订单用过）：订单域与库存域的口径都在这一条上。
	if s.invSvc != nil {
		moved, merr := s.invSvc.VariantHasStockMovement(ctx, projectID, v.ID)
		if merr != nil {
			return "", "", merr
		}
		if moved {
			return productenums.VariantSkipHasMovement, "", nil
		}
	}
	return "", "", nil
}

// resolveContainerSKU 容器主体 SKU（规则 A 的主体段）的**唯一**解析入口。
//
//	① 商品的主体编码（products.sku_code，迁移 246）—— 新建商品的权威来源；
//	② 存量商品该列为空 → 回落到最早创建的那条变体（商品创建时的无规格变体，
//	   它的 SKU 就是当时的容器主体）。**不重写它**，新变体以它为主体拼接；
//	③ 都拿不到 → 按商品 URL 段派生（确定性，不退回随机码）。
//
// 预览、生成、保存三条路径共用它：容器段一旦分叉，同一个组合会得到两种 SKU。
func resolveContainerSKU(p *productmodel.ProductEntity, existing []*productmodel.VariantEntity, warehouseCode string) (string, error) {
	if p == nil {
		return "", errors.New(productenums.ErrInvalidParam)
	}
	if container := strings.TrimSpace(p.SKUCode); container != "" {
		return container, nil
	}
	if container := containerSKUOfVariants(existing); container != "" {
		return container, nil
	}
	// 兜底派生也走容器主体 SKU 的**唯一**入口（本批合并）：三处路径共用一条分支逻辑，
	// 否则「新建时加仓码前缀、生成时忘了」这类差异只会在某个组合上暴露。
	return buildProductContainerSKU(containerSKUInput{
		ProductType: p.Type, Slug: p.Slug, WarehouseCode: warehouseCode,
	})
}

// normalizeVariantSKU 清单里运营可编辑的 SKU 文本的规范化。
//
// 逐段经 skuSegment 归一（ASCII 段原样保留、非 ASCII 段取确定性短码）——
// 与系统生成变体 SKU 时用的是同一个段规则（不是另写一套大小写 / 字符白名单）。
// 出现空段（含纯空白）或整串为空白时返回空串，由调用方报 ErrVariantSKUEmpty：
// 静默补一个「系统生成」的编码会让运营以为自己填的那个已经生效。
func normalizeVariantSKU(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, skuSeparator)
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			return ""
		}
		parts[i] = skuSegment(part)
	}
	return strings.Join(parts, skuSeparator)
}

// normalizeListOptionPairs 校验并归一清单**新增行**的规格组合（服务端重算）。
//
// 属性组必须属于该商品且参与变体、取值必须是组内启用值（按 attributeValueCode 口径，
// 与 option_values 的写入口径逐字一致），键序按属性组固定顺序归一 ——
// 这三条与 buildVariationDimensions 对勾选的判定是同一份规则。
func normalizeListOptionPairs(raw json.RawMessage, allowed map[string]map[string]bool, groupOrder map[string]int) ([]optionPair, error) {
	pairs := decodeOptionPairs(raw)
	if len(pairs) == 0 {
		return nil, errors.New(productenums.ErrVariantOptionsInvalid)
	}
	for _, p := range pairs {
		values, ok := allowed[p.Key]
		if !ok {
			return nil, fmt.Errorf("%s：%s", productenums.ErrVariationAttributeInvalid,
				i18n.ErrorDetail(productenums.DetailVariationAttributeNotInProduct, "attribute", p.Key))
		}
		if !values[p.Value] {
			return nil, fmt.Errorf("%s：%s", productenums.ErrVariationValueInvalid,
				i18n.ErrorDetail(productenums.DetailVariationValueNotEnabled, "attribute", p.Key, "value", p.Value))
		}
	}
	return sortedOptionPairs(pairs, groupOrder), nil
}

// variationValueIndex 参与变体的属性组 → 组内启用值的编码集合（清单新增行的校验依据）。
func variationValueIndex(attrs []*productmodel.ProductAttributeEntity) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(attrs))
	for _, a := range attrs {
		if a == nil || !a.IsVariation {
			continue
		}
		set := map[string]bool{}
		for _, v := range enabledAttributeValues(a) {
			set[attributeValueCode(v)] = true
		}
		if len(set) > 0 {
			out[a.Key] = set
		}
	}
	return out
}

// sortedOptionPairs 组合对按**属性组固定顺序**排序（组内按属性组 key 字典序兜底）。
//
// SKU 属性段的拼接与 option_values 的写入顺序都取它 —— 顺序不固定则同一组合会
// 生成不同 SKU（docs/14 §1.2 的第一条约束）。
func sortedOptionPairs(pairs []optionPair, groupOrder map[string]int) []optionPair {
	sorted := make([]optionPair, len(pairs))
	copy(sorted, pairs)
	fallback := len(groupOrder) + 1
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, iok := groupOrder[sorted[i].Key]
		if !iok {
			ri = fallback
		}
		rj, jok := groupOrder[sorted[j].Key]
		if !jok {
			rj = fallback
		}
		if ri != rj {
			return ri < rj
		}
		return sorted[i].Key < sorted[j].Key
	})
	return sorted
}

// 编译期断言：本 service 满足订单域需要的快照端口。
var _ productcontract.VariantSnapshotPort = (*Service)(nil)

// VariantSnapshots 按变体 id 批量取下单快照需要的事实。只读、无副作用。
//
// projectID 是必填的工程作用域（审计 DB-009）：见端口与 model.ListByIDs 的注释。
func (s *Service) VariantSnapshots(ctx context.Context, variantIDs []string, projectID string) (list []*productcontract.VariantSnapshot, err error) {
	ids := dedupeNonEmpty(variantIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	variants, err := s.m.ListVariantsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	if len(variants) == 0 {
		return nil, nil
	}
	// 变体表没有商品名与工程列，按 product_id 批量补齐（不逐条查，避免 N+1）
	productIDs := make([]string, 0, len(variants))
	seen := make(map[string]bool, len(variants))
	for _, v := range variants {
		if !seen[v.ProductID] {
			seen[v.ProductID] = true
			productIDs = append(productIDs, v.ProductID)
		}
	}
	// 商品名与工程一并补齐：作用域与变体那次读取同源（端口入参的 projectID），
	// 不在这里另取工程上下文 —— 消费方给的工程就是唯一真源。
	products, err := s.m.ListByIDs(ctx, productIDs, projectID)
	if err != nil {
		return nil, err
	}
	nameOf := make(map[string]string, len(products))
	projectOf := make(map[string]string, len(products))
	for _, p := range products {
		nameOf[p.ID] = p.Name
		projectOf[p.ID] = p.ProjectID
	}
	// 成本来自 **(仓库, SKU)**：该变体在归属仓（未指定仓库时按库存域既有的归属仓解析
	// 规则 —— 默认仓）的当前成本。成本搬到仓库之后，变体级 product_variants.cost_price
	// 不再是订单成本快照的来源（docs/14 §9.3 的 ⚠️：沿用变体级会让订单利润与仓库侧
	// 对不上，且改价后历史利润会漂移）。
	refs := make([]inventorycontract.VariantWarehouseCostRef, 0, len(variants))
	for _, v := range variants {
		refs = append(refs, inventorycontract.VariantWarehouseCostRef{VariantID: v.ID})
	}
	costs := s.variantWarehouseCosts(ctx, projectID, refs)

	list = make([]*productcontract.VariantSnapshot, 0, len(variants))
	for _, v := range variants {
		// 商品行在本工程作用域内不可见 ⇒ 该变体不属于本工程，**丢掉它**。
		//
		// 这一步不能省，也不能改成「照旧返回一条 ProductName / ProjectID 为空的快照」：
		// 变体表（product_variants）不在迁移 215 的名单里、没有策略，所以别的工程的变体 id
		// 会被照常读出来；若把它交给消费方，order / cart 那道
		// 「sn.ProjectID != "" && sn.ProjectID != projectID」的守卫会因为 ProjectID 为空而
		// **放行** —— 那是用一次静默降级换掉一条越权拦截（订单会落一行商品名为空的快照项）。
		//
		// 丢掉之后，消费方看到的现象与「这个规格不存在」完全一致 —— 端口契约本来就这么写
		// （查不到的 id 不出现在返回里），调用方按差集判定「不存在或已删除」。
		ownerProject := projectOf[v.ProductID]
		if ownerProject == "" {
			continue
		}
		list = append(list, &productcontract.VariantSnapshot{
			VariantID:    v.ID,
			ProductID:    v.ProductID,
			ProjectID:    ownerProject,
			ProductName:  nameOf[v.ProductID],
			VariantLabel: variantOptionLabel(v.OptionValues),
			SKU:          v.SKUCode,
			Price:        yuanToCents(v.Price),
			CostPrice:    costOf(costs, v.ID),
			Enabled:      v.Enabled,
		})
	}
	return list, nil
}

// variantOptionLabel 把 option_values（属性组 key → 属性值 key）拼成一行规格文本。
//
// 用值 key 而不是属性值表里的展示 label：快照要的是**下单那一刻看到的那套组合**，
// 现去查属性值表还会把「事后改了 label」带进来。只有一处例外要注意 ——
// 值 key 是英文/拼音时读起来不如中文 label，展示层需要更好看的名字时应另做映射，
// 而不是让快照去依赖一张会变的表。
func variantOptionLabel(raw json.RawMessage) string {
	pairs := decodeOptionPairs(raw)
	if len(pairs) == 0 {
		return ""
	}
	vals := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if strings.TrimSpace(p.Value) != "" {
			vals = append(vals, p.Value)
		}
	}
	return strings.Join(vals, " / ")
}

// yuanToCents 元 → 分。商品域是 numeric(12,2)（元），订单域一律整数分。
func yuanToCents(yuan float64) int64 {
	return int64(math.Round(yuan * 100))
}

// costOf 取某个变体的成本快照（分）；没有仓库侧成本时返回 nil（= 尚未核算）。
//
// 返回指针而不是哨兵值：0 是合法的显式成本（赠品 / 内部划拨），拿 0 冒充「未知」
// 会让订单利润凭空多出一笔；用负值当哨兵则把「契约能否表达未知」藏进实现侧注释里。
func costOf(costs map[string]int64, variantID string) *int64 {
	cents, ok := costs[variantID]
	if !ok {
		return nil
	}
	value := cents
	return &value
}

// variantWarehouseCosts 取这些变体在**各自归属仓**的当前成本（分）。
//
// 返回的 map 只含确实有成本的变体：未核算（cost_price IS NULL）与「该仓没有这条
// 库存行」都不进 map，由 costOf 统一落成 nil。
//
// 解析失败为什么不升级成错误：价与启用态才是快照的主事实（下单 / 加购 / 实时价片段
// 三个消费方都靠它），成本是附加的记账事实；而它的失败形态（工程没有默认仓、库存行
// 还没生成）恰恰就是「尚未核算」的常见样子。把它变成错误会让「没有默认仓的工程连
// 加购都点不动」—— 那不是本次口径收口要换来的行为。成本真缺了会在订单行上表现为
// NULL，而不是被 0 掩盖。
//
// 注意**读的是当前成本**：这不是「历史成本」，历史成本由出库流水的 unit_cost 留痕
// （迁移 256）；本函数只负责下单那一刻的取值。
func (s *Service) variantWarehouseCosts(ctx context.Context, projectID string, refs []inventorycontract.VariantWarehouseCostRef) map[string]int64 {
	out := make(map[string]int64, len(refs))
	if len(refs) == 0 {
		return out
	}
	if s.invSvc == nil {
		// 未注入库存用例（纯商品单测路径）：没有库存域就没有仓库侧成本可言 ⇒ 全部未知。
		// 生产装配下 SetInventoryService 是 required-port（wiring 自检漏接即 panic）。
		return out
	}
	costs, err := s.invSvc.ResolveVariantWarehouseCosts(ctx, projectID, refs)
	if err != nil {
		return out
	}
	for _, c := range costs {
		if c.CostPrice == nil {
			continue
		}
		out[c.VariantID] = yuanToCents(*c.CostPrice)
	}
	return out
}

// dedupeNonEmpty 去空去重，保持首次出现顺序。
func dedupeNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// 编译期断言：本文件是契约端口 VariantAvailabilityLookupPort 的实现处。
//
// 装配层（internal/routers/assembly.go）持有的是接口值，只能做运行时类型断言 ——
// 端口签名漂移要靠这里的断言在编译期拦住，而不是等到进程启动 panic。
var _ productcontract.VariantAvailabilityLookupPort = (*Service)(nil)

// VariantAvailabilityLookupMaxIDs 单次查询上界（片段参数长度与查询 IN 列表的双重保护）。
const VariantAvailabilityLookupMaxIDs = 100

// VariantAvailabilities 实现 productcontract.VariantAvailabilityLookupPort。
func (s *Service) VariantAvailabilities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error) {
	out = map[string]int{}
	ids := normalizeVariantIDs(variantIDs)
	if len(ids) == 0 {
		return out, nil
	}
	if s.availability == nil {
		return out, nil // 降级：调用方渲染「以结算时库存为准」
	}
	// 工程作用域由调用方给定；工程号非法（空串 / 非 uuid）时返回空结果而不是报错 ——
	// 本端口整体是「尽力而为」语义（读不到库存不该把页面变成 500）。
	pid := strings.TrimSpace(projectID)
	if pid == "" || !isUUID(pid) {
		return out, nil
	}
	// 该工程的可用量一次查回：不属于本工程的变体由 inventory_stocks 的策略挡在外面，
	// 它们不会出现在结果里，调用方按「未知」渲染兜底文案。
	avail, aerr := s.availability.AvailableQuantities(ctx, pid, ids)
	if aerr != nil {
		return nil, aerr
	}
	for id, n := range avail {
		if n < 0 {
			n = 0
		}
		out[id] = n
	}
	return out, nil
}

// normalizeVariantIDs 去空、去重、校验形状、截断；顺序保持首次出现（片段输出顺序可预测）。
//
// 形状校验是必需的，不是防御性洁癖：片段参数来自 URL（任何人都能写 variantIds=abc），
// 而非 uuid 的字符串带进 `WHERE id IN ?` 会让 PostgreSQL 直接报
// `invalid input syntax for type uuid`（SQLSTATE 22P02）—— 页面变成 500。
// 非法 id 在这里被丢弃，调用方按「未知」渲染兜底文案（与查不到同一条路）。
func normalizeVariantIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !isUUID(id) {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) >= VariantAvailabilityLookupMaxIDs {
			break
		}
	}
	return out
}
