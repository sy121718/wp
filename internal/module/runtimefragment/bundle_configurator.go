// bundle_configurator.go — 捆绑品前台配置器片段（issue #20 验收 3/7）。
//
// 两个 capability：
//
//	bundleConfigurator       GET  —— 渲染配置器（选项 + 默认数量 + 可用量 + 套餐价）；
//	bundleConfiguratorCheck  POST —— 整单硬校验（纯计算，不写库），返回结论片段。
//
// 为什么配置器要放在 runtimefragment 而不是某个页面里：静态站点的商品详情是**已编译产物**，
// 选项数量与可用量每次请求都要变，只能由访问面的片段端点现算（docs/04 §1.1）。
//
// 依赖注入是包级的（SetBundleProvider）：runtimefragment 不持有 gorm、也不属于模块装配链，
// 顶层装配时把商品模块的 BundleConfiguratorPort 递进来。未注入时两个能力都返回明确错误
// （fail-closed），而不是渲染一个「看起来能用但不算数」的配置器。
package runtimefragment

import (
	"context"
	"errors"
	"strconv"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/templates"
)

// bundleProvider 捆绑配置器依赖（装配期注入）。
var bundleProvider productcontract.BundleConfiguratorPort

// SetBundleProvider 注入捆绑配置器能力（装配期调用；传 nil 表示未接入）。
func SetBundleProvider(port productcontract.BundleConfiguratorPort) {
	bundleProvider = port
}

func init() {
	Register(Spec{
		Type:   "bundleConfigurator",
		Method: "GET",
		Auth:   AuthAnonymous,
		Render: renderBundleConfigurator,
	})
	// 校验片段是纯计算（不写库、不预占库存），故 anonymous 可达；
	// 将来若要「加入购物车」，必须另开 session + CSRF 的写能力，不能复用本能力。
	Register(Spec{
		Type:   "bundleConfiguratorCheck",
		Method: "POST",
		Auth:   AuthAnonymous,
		Render: renderBundleConfiguratorCheck,
	})
}

// bundleConfiguratorView 配置器片段的模板数据。
type bundleConfiguratorView struct {
	ProductID   string
	ProductName string
	Price       string
	TotalHint   string
	Items       []bundleOptionView
}

// bundleOptionView 一个选项的展示数据（数量上限为 0 时按「不设上限」呈现）。
type bundleOptionView struct {
	VariantID    string
	SKUCode      string
	ProductName  string
	RequiredText string
	DefaultQty   int
	MinQty       int
	MaxAttr      string
	MaxText      string
	Available    int
	Disabled     bool
}

// bundleResultView 校验结论片段的模板数据。
type bundleResultView struct {
	OK         bool
	Message    string
	TotalQty   int
	TotalPrice string
	Items      []bundleResultItemView
}

type bundleResultItemView struct {
	SKUCode     string
	ProductName string
	Qty         int
	UnitPrice   string
	Available   int
}

// renderBundleConfigurator 渲染配置器（GET /_fragments/bundleConfigurator?productId=…）。
func renderBundleConfigurator(ctx context.Context, r *Request) (string, error) {
	if bundleProvider == nil {
		return "", errors.New("捆绑配置器未接入商品模块")
	}
	productID := strings.TrimSpace(r.Params["productId"])
	if productID == "" {
		return "", errors.New("缺少 productId")
	}
	data, err := bundleProvider.BundleConfiguratorData(ctx, productID)
	if err != nil {
		return "", err
	}
	view := bundleConfiguratorView{
		ProductID:   data.ProductID,
		ProductName: data.ProductName,
		Price:       formatAmount(data.BasePrice),
		TotalHint:   totalHint(data.Config),
		Items:       make([]bundleOptionView, 0, len(data.Options)),
	}
	for _, o := range data.Options {
		view.Items = append(view.Items, bundleOptionView{
			VariantID:    o.VariantID,
			SKUCode:      o.SKUCode,
			ProductName:  o.ProductName,
			RequiredText: requiredText(o.Required),
			DefaultQty:   o.DefaultQty,
			MinQty:       o.MinQty,
			MaxAttr:      maxAttr(o.MaxQty),
			MaxText:      maxText(o.MaxQty),
			Available:    o.Available,
			Disabled:     !o.Enabled,
		})
	}
	return templates.RenderFragment("bundle_configurator", view)
}

// renderBundleConfiguratorCheck 整单校验并返回结论片段（POST /_fragments/bundleConfiguratorCheck）。
//
// 校验失败不是「片段渲染失败」：它是**业务结论**，要渲染成页面上的提示（200 + 提示文案），
// 而不是 500。只有依赖未接入 / 参数结构不可解析才算真正的渲染失败。
func renderBundleConfiguratorCheck(ctx context.Context, r *Request) (string, error) {
	if bundleProvider == nil {
		return "", errors.New("捆绑配置器未接入商品模块")
	}
	productID := strings.TrimSpace(r.Params["productId"])
	if productID == "" {
		return "", errors.New("缺少 productId")
	}
	ids := r.Values["variantId"]
	qtys := r.Values["qty"]
	items := make([]productdto.BundleSelectItem, 0, len(ids))
	for i, vid := range ids {
		qty := 0
		if i < len(qtys) {
			v := strings.TrimSpace(qtys[i])
			if v != "" {
				n, cerr := strconv.Atoi(v)
				if cerr != nil {
					// 非整数数量：按参数错误呈现给买家（不是 500）。
					return templates.RenderFragment("bundle_configurator_result", bundleResultView{
						OK: false, Message: bundleMessage(productenums.ErrBundleQtyInvalid),
					})
				}
				qty = n
			}
		}
		items = append(items, productdto.BundleSelectItem{VariantID: strings.TrimSpace(vid), Qty: qty})
	}
	res, err := bundleProvider.ValidateBundleSelection(ctx, &productdto.ValidateBundleSelectionReq{
		ProductID: productID,
		Items:     items,
	})
	if err != nil {
		return templates.RenderFragment("bundle_configurator_result", bundleResultView{
			OK: false, Message: bundleMessage(err.Error()),
		})
	}
	view := bundleResultView{
		OK:         true,
		Message:    "已按当前数量计算套餐价",
		TotalQty:   res.TotalQty,
		TotalPrice: formatAmount(res.TotalPrice),
		Items:      make([]bundleResultItemView, 0, len(res.Items)),
	}
	for _, it := range res.Items {
		view.Items = append(view.Items, bundleResultItemView{
			SKUCode: it.SKUCode, ProductName: it.ProductName, Qty: it.Qty,
			UnitPrice: formatAmount(it.UnitPrice), Available: it.Available,
		})
	}
	return templates.RenderFragment("bundle_configurator_result", view)
}

// —— 展示口径的小工具（数值 → 前台文案，只在本文件用）——

func formatAmount(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func requiredText(required bool) string {
	if required {
		return "必选"
	}
	return "可选"
}

// maxAttr 数量输入的 max 属性（不设上限时返回空串，模板据此省略该属性）。
func maxAttr(maxQty int) string {
	if maxQty > 0 {
		return strconv.Itoa(maxQty)
	}
	return ""
}

func maxText(maxQty int) string {
	if maxQty > 0 {
		return strconv.Itoa(maxQty) + " 件"
	}
	return "不限"
}

// totalHint 整单件数区间的可读描述。
func totalHint(cfg productdto.BundleConfig) string {
	min := cfg.MinTotalQty
	max := cfg.MaxTotalQty
	switch {
	case min > 0 && max > 0:
		return "整单 " + strconv.Itoa(min) + " ~ " + strconv.Itoa(max) + " 件"
	case min > 0:
		return "整单至少 " + strconv.Itoa(min) + " 件"
	case max > 0:
		return "整单最多 " + strconv.Itoa(max) + " 件"
	default:
		return "整单件数不限"
	}
}

// bundleMessage 把 service 返回的错误码翻成买家看得懂的一句话。
//
// 访问面自持文案（后台的 enums key 走 admin 的 i18n 链路）：片段返回的是公开站点的 HTML，
// 不能把内部错误码原样显示给访客。未知错误码回退成一句通用提示，绝不漏出内部细节。
func bundleMessage(code string) string {
	switch code {
	case productenums.ErrBundleNotConfigured:
		return "该商品未配置可选规格"
	case productenums.ErrBundleOptionRequired:
		return "有必选项还没选数量"
	case productenums.ErrBundleQtyInvalid:
		return "数量必须是整数且不为负"
	case productenums.ErrBundleQtyBelowMin:
		return "有选项的数量低于该项的最小数量"
	case productenums.ErrBundleQtyAboveMax:
		return "有选项的数量超过了该项的最大数量"
	case productenums.ErrBundleTotalBelowMin:
		return "整单总件数未达到最小购买数量"
	case productenums.ErrBundleTotalAboveMax:
		return "整单总件数超过了上限"
	case productenums.ErrBundleQtyAboveStock:
		return "有选项的数量超过了当前可用库存"
	case productenums.ErrBundleVariantNotInConfig:
		return "选择的规格不属于该套餐"
	case productenums.ErrBundleVariantDuplicated:
		return "同一个规格重复提交了数量"
	case productenums.ErrBundleStockUnavailable:
		return "暂时无法确认库存，请稍后再试"
	default:
		return "当前选择无法下单，请检查数量"
	}
}
