// product_bundle.go — 捆绑品配置与整单校验契约（issue #20）。
//
// 两个消费者：
//
//	· 后台配置页与后台 API —— 走 ProductService（管理侧，见 product_service.go）；
//	· 前台运行时片段（runtimefragment 的 bundleConfigurator）—— 走本文件的
//	  BundleConfiguratorPort（只读渲染 + 整单校验），它不需要商品的任何管理能力。
//
// 单独立一个窄接口而不是让 runtimefragment 依赖 ProductService：片段处理器只需要
// 「读配置」与「校验一次选择」两件事，接口越窄，依赖方向越不会被将来的商品功能带偏。
package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// BundleConfiguratorPort 前台捆绑配置器所需的全部能力（由 product/service 实现）。
type BundleConfiguratorPort interface {
	// BundleConfiguratorData 读某商品的捆绑配置 + 每个选项的 SKU 快照与**真源可用量**。
	// 商品不存在、或该商品没配任何选项（不是捆绑品）时返回错误。
	BundleConfiguratorData(ctx context.Context, productID string) (res *productdto.BundleConfigResp, err error)
	// ValidateBundleSelection 整单硬校验 + 算价（纯读，不写库）：
	// 漏填必选项 / 单项超上下限 / 低于整单最小总件数 / 数量非法 / 超可用量一律拒绝。
	ValidateBundleSelection(ctx context.Context, req *productdto.ValidateBundleSelectionReq) (res *productdto.BundleSelectionResp, err error)
}
