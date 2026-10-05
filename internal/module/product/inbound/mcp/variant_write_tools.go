package productmcp

// variant_write_tools.go — 商品变体（SKU）的新增 / 修改 / 删除。
//
// 为什么和商品主体（product_tools.go）分开成一批：
// products 一行是「这个东西是什么」，product_variants 一行是「它的哪个版本、卖多少钱、
// 库存挂在哪」—— 定价、SKU、条码、库存归属都长在变体上。用户说「加个蓝色的」「改成 99 块」
// 说的都是变体，而在此之前没有任何工具能碰它们。
//
// **金额单位是本批最容易搞错的地方**：product 模块一律用「元」（float64），
// 而 order 模块用「分」（int64）。同一个仓库里两套口径，所以这里每个金额字段的描述
// 都显式写了单位 —— 模型上一次当成「分」就会把 99 元的商品定成 0.99 元。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/permission"
)

// VariantWriter 变体写能力（给 AI 工具的窄门）。
//
// 刻意不含 GenerateVariants（一次按属性笛卡尔积批量造变体 —— 它会覆盖既有 SKU 的
// 价格与库存口径，是给「刚建完商品、配置规格」这一步用的批量动作，不是问答里的一次修改）
// 与 UpdateVariantCost（改成本价会连锁影响毛利报表与定价规则）。
type VariantWriter interface {
	CreateVariant(ctx context.Context, req *productdto.CreateVariantReq) (*productdto.VariantResp, error)
	UpdateVariant(ctx context.Context, req *productdto.UpdateVariantReq) (*productdto.VariantResp, error)
	DeleteVariant(ctx context.Context, req *productdto.DeleteVariantReq) error
}

// VariantWriteTools 返回变体写工具集。
func VariantWriteTools(w VariantWriter) ([]mcp.Tool, error) {
	if w == nil {
		return nil, errors.New("productmcp: 变体写依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{variantCreate(w), variantUpdate(w), variantDelete(w)}, nil
}

func variantCreate(w VariantWriter) mcp.Tool {
	return mcp.NewWrite("variant_create", "新增商品变体（SKU）",
		"给某个商品加一个变体（一个可独立售卖、独立库存的 SKU）。\n"+
			"**金额单位是元**（不是分）—— 99 元传 99，不要传 9900。\n"+
			"价格不传就继承商品主价格；库存 quantity 不传 = **不跟踪数量（可无限售出）**，"+
			"传 0 是「暂时没货」，两者不是一回事，别用 0 表示「无限」。\n"+
			"warehouseId 决定这个 SKU 的库存挂在哪个仓；不传用工程默认仓。"+
			"无论选没选，都会在归属仓生成一条库存记录（初始 0）。\n"+
			"productId 用 product_find 拿；该商品已被删除时不能加变体。",
		permission.ProductVariantCreate,
		mcp.Object("新增变体参数", map[string]mcp.Schema{
			"productId":    mcp.String("所属商品 id（用 product_find 拿）"),
			"projectId":    mcp.String("工程 id（可选；不传由后端按唯一工程兜底）"),
			"skuCode":      mcp.String("SKU 编码（可选；留空则由后端生成）"),
			"barcode":      mcp.String("条码（可选）"),
			"price":        money("售价，**单位：元**（可选；不传继承商品主价格），可带小数"),
			"comparePrice": money("划线价，**单位：元**（可选；要比售价大才有意义），可带小数"),
			"costPrice":    money("成本价，**单位：元**（可选），可带小数"),
			"image":        mcp.String("变体图 URL（可选）"),
			"enabled":      mcp.Boolean("是否可售（可选；不传按可售处理）"),
			"sort":         mcp.Integer("排序值（可选，越小越靠前）"),
			"warehouseId":  mcp.String("归属仓库 id（可选；不传用工程默认仓）"),
			"quantity":     mcp.Integer("库存数量（可选；**不传 = 不跟踪数量（无限）**，0 是「没货」）"),
		}, "productId"),
		nil,
		func(ctx context.Context, args variantCreateArgs) (mcp.Result, error) {
			req := &productdto.CreateVariantReq{
				ProductID:   strings.TrimSpace(args.ProductID),
				ProjectID:   strings.TrimSpace(args.ProjectID),
				SKUCode:     strings.TrimSpace(args.SKUCode),
				Barcode:     strings.TrimSpace(args.Barcode),
				Image:       strings.TrimSpace(args.Image),
				Sort:        args.Sort,
				WarehouseID: strings.TrimSpace(args.WarehouseID),
				Quantity:    args.Quantity,
			}
			req.Price = args.Price
			req.ComparePrice = args.ComparePrice
			req.CostPrice = args.CostPrice
			if args.Enabled != nil {
				b := *args.Enabled
				req.Enabled = &b
			}
			res, err := w.CreateVariant(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: variantCreatedText(res, args.Quantity)}, nil
		})
}

type variantCreateArgs struct {
	ProductID    string   `json:"productId"`
	ProjectID    string   `json:"projectId"`
	SKUCode      string   `json:"skuCode"`
	Barcode      string   `json:"barcode"`
	Price        *float64 `json:"price"`
	ComparePrice *float64 `json:"comparePrice"`
	CostPrice    *float64 `json:"costPrice"`
	Image        string   `json:"image"`
	Enabled      *bool    `json:"enabled"`
	Sort         int      `json:"sort"`
	WarehouseID  string   `json:"warehouseId"`
	Quantity     *int     `json:"quantity"`
}

func variantCreatedText(res *productdto.VariantResp, qty *int) string {
	if res == nil {
		return "变体已新增。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已给商品 %s 新增变体（id=%s，SKU %s，售价 %s）。",
		res.ProductID, res.ID, emptyAsDash(res.SKUCode), formatPrice(res.Price))
	if !res.Enabled {
		b.WriteString("\n**它当前是「不可售」状态** —— 前台看不到、买不了。要开卖得改成可售。")
	}
	b.WriteString("\n" + quantityLine(res, qty))
	return b.String()
}

// quantityLine 说清这个变体到底怎么管库存。
//
// create 的 quantity 是**可空**的：不给 = 不跟踪 = 无限售出，给 0 = 没货。
// 这两种在数据库里是同一个 0（库存行数量），只有在「刚新增、且没显式给数量」时
// 才能靠入参区分，所以这句提示只能在这里说 —— 事后再看库存页是看不出来的。
func quantityLine(res *productdto.VariantResp, qty *int) string {
	if qty == nil {
		return "库存：**没有跟踪数量**，可以无限售出。要按实际库存卖，去库存页把它改成跟踪数量。"
	}
	if *qty == 0 {
		return "库存：0 —— 这个 SKU 现在**卖不出去**（不是无限，是没货）。"
	}
	return fmt.Sprintf("库存：已设为 %d，可在库存页加减。", *qty)
}

type variantUpdateArgs struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"projectId"`
	SKUCode      *string  `json:"skuCode"`
	Barcode      *string  `json:"barcode"`
	Price        *float64 `json:"price"`
	ComparePrice *float64 `json:"comparePrice"`
	CostPrice    *float64 `json:"costPrice"`
	Image        *string  `json:"image"`
	Enabled      *bool    `json:"enabled"`
	Sort         *int64   `json:"sort"`
}

func variantUpdate(w VariantWriter) mcp.Tool {
	return mcp.NewWrite("variant_update", "修改商品变体（SKU）",
		"改一个变体的价格、SKU 编码、条码、图、可售状态或排序。\n"+
			"**只改你传的字段**：没传的保持原样。所以「把价格改成 0」要传 price=0，"+
			"而「不动价格」就整个不传 price —— 两者结果不同。\n"+
			"**金额单位是元**（不是分）—— 99 元传 99。\n"+
			"改 skuCode 会影响所有引用这个编码的地方（对账、外部同步）；"+
			"除非用户明确要改编码，否则别动它。\n"+
			"enabled=false 会让它立刻从前台下架（已经下过的单不受影响）。",
		permission.ProductVariantUpdate,
		mcp.Object("修改变体参数", map[string]mcp.Schema{
			"id":           mcp.String("变体 id（用 product_get 看商品的变体列表拿）"),
			"projectId":    mcp.String("工程 id（可选）"),
			"skuCode":      mcp.String("新 SKU 编码（可选；**改动影响面大，非必要别动**）"),
			"barcode":      mcp.String("新条码（可选）"),
			"price":        money("新售价，**单位：元**（可选；改 0 就是免费，慎重），可带小数"),
			"comparePrice": money("新划线价，**单位：元**（可选），可带小数"),
			"costPrice":    money("新成本价，**单位：元**（可选；影响毛利统计），可带小数"),
			"image":        mcp.String("新变体图 URL（可选）"),
			"enabled":      mcp.Boolean("是否可售（可选；false = 立即下架）"),
			"sort":         mcp.Integer("新排序值（可选，越小越靠前）"),
		}, "id"),
		nil,
		func(ctx context.Context, args variantUpdateArgs) (mcp.Result, error) {
			req := &productdto.UpdateVariantReq{
				ID:        strings.TrimSpace(args.ID),
				ProjectID: strings.TrimSpace(args.ProjectID),
				SKUCode:   args.SKUCode,
				Barcode:   args.Barcode,
				Image:     args.Image,
				Enabled:   args.Enabled,
			}
			req.Price = args.Price
			req.ComparePrice = args.ComparePrice
			req.CostPrice = args.CostPrice
			if args.Sort != nil {
				s := int(*args.Sort)
				req.Sort = &s
			}
			if req.SKUCode == nil && req.Barcode == nil && req.Image == nil &&
				req.Enabled == nil && req.Price == nil && req.ComparePrice == nil &&
				req.CostPrice == nil && req.Sort == nil {
				return mcp.Result{}, errors.New("没有要改的字段：至少给一个（只改传了的字段）")
			}
			res, err := w.UpdateVariant(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: variantUpdatedText(res)}, nil
		})
}

// variantUpdatedText 报「改完长什么样」而不是「改成功了」。
//
// 因为「只改传了的字段」这个语义下，用户最想知道的是**现在**的值：
// 说「已修改价格」但不说改成多少，用户还得再去翻一遍。
func variantUpdatedText(res *productdto.VariantResp) string {
	if res == nil {
		return "变体已修改。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "变体 %s（SKU %s）已更新。现在：售价 %s",
		res.ID, emptyAsDash(res.SKUCode), formatPrice(res.Price))
	if res.ComparePrice != nil {
		fmt.Fprintf(&b, "，划线价 %s", formatPrice(*res.ComparePrice))
	}
	if res.CostPrice != nil {
		fmt.Fprintf(&b, "，成本价 %s", formatPrice(*res.CostPrice))
	}
	if res.Enabled {
		b.WriteString("，可售。")
	} else {
		b.WriteString("，**不可售**（前台看不到）。")
	}
	return b.String()
}

type variantDeleteArgs struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
}

func variantDelete(w VariantWriter) mcp.Tool {
	return mcp.NewWrite("variant_delete", "删除商品变体（SKU）",
		"删除一个变体。**它会立刻从前台消失**，且这个 SKU 的库存行一并作废。\n"+
			"历史订单里已经卖出去的那几单不会变（订单存的是当时的快照），"+
			"但之后再想按这个 SKU 对账就找不到了。\n"+
			"**如果只是不想再卖，用 variant_update 把 enabled 设成 false 更合适** ——"+
			"下架可逆，删除不可逆。用户说「下架」「先别卖」时不要调这个工具。\n"+
			"商品只剩一个变体时删除它，会让这个商品在前台没有可买的东西。",
		permission.ProductVariantDelete,
		mcp.Object("删除变体参数", map[string]mcp.Schema{
			"id":        mcp.String("要删除的变体 id"),
			"projectId": mcp.String("工程 id（可选）"),
		}, "id"),
		nil,
		func(ctx context.Context, args variantDeleteArgs) (mcp.Result, error) {
			if err := w.DeleteVariant(ctx, &productdto.DeleteVariantReq{
				ID:        strings.TrimSpace(args.ID),
				ProjectID: strings.TrimSpace(args.ProjectID),
			}); err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: fmt.Sprintf(
				"变体 %s 已删除，它已从前台消失，库存行一并作废。"+
					"如果其实只是想停止售卖，下次可以用 variant_update 下架（可逆）。", args.ID)}, nil
		})
}

// money 构造一个「元」金额字段。
//
// 用 number 而不是 integer：商品价格本来就有 9.9 / 99.5 这种小数，
// 用 integer 会在校验层直接拒掉它们（模型只能取整，等于悄悄改了用户报的价）。
func money(desc string) mcp.Schema {
	return mcp.Schema{Type: "number", Description: desc}
}

// 空值展示用短横线：JSON 里 "" 与「这个字段没有」在阅读上没区别，
// 而在变体上「SKU 为空」往往是后端自动生成的，得让人看出「这里本来就没填」。
func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
