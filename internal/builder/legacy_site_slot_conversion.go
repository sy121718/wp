package builder

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	json "go_wp/pkg/jsonx"
)

// LegacyDocumentChange 是一次离线转换的可定位回执，不参与生产寻址。
type LegacyDocumentChange struct {
	NodeID string `json:"nodeId"`
	Kind   string `json:"kind"`
	Slot   string `json:"slot,omitempty"`
	PageID string `json:"pageId,omitempty"`
}

// LegacyDocumentConflict 标明不能唯一转换的旧节点；调用方必须整批拒绝。
type LegacyDocumentConflict struct {
	NodeID string `json:"nodeId"`
	Slot   string `json:"slot,omitempty"`
	Reason string `json:"reason"`
}

type LegacyDocumentConversion struct {
	Document  json.RawMessage          `json:"document,omitempty"`
	Changes   []LegacyDocumentChange   `json:"changes"`
	Conflicts []LegacyDocumentConflict `json:"conflicts"`
}

// LegacyProductContext 仅由调用方已确认的详情实例或商品详情模板提供。
type LegacyProductContext struct {
	ProductID             string
	ProductDetailTemplate bool
}

// ConvertLegacySiteSlots 只用于显式控制面迁移。页面目标由调用方验证工程与发布状态；
// 商品上下文必须来自真实详情实体或模板。存在冲突时不交出任何半转换文档。
func ConvertLegacySiteSlots(raw json.RawMessage, targets map[string]string, product LegacyProductContext) (result LegacyDocumentConversion, err error) {
	page, err := ParsePage(raw)
	if err != nil {
		return result, err
	}
	var visit func([]*core.Node) error
	visit = func(nodes []*core.Node) error {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			var props map[string]json.RawMessage
			if len(node.Props) > 0 {
				if err := json.Unmarshal(node.Props, &props); err != nil {
					return fmt.Errorf("节点%s: %w", node.ID, err)
				}
			}
			var source string
			if rawSource, ok := props["collectionSource"]; ok {
				if err := json.Unmarshal(rawSource, &source); err != nil {
					return fmt.Errorf("节点%s集合源无效: %w", node.ID, err)
				}
			}
			slot := ""
			switch node.Type {
			case "core.productList":
				if source == "content:product" {
					slot = "shop"
				}
			case "core.cardstack":
				if source == "content:product" {
					slot = "shop"
				}
				if source == "content:article" {
					slot = "blog"
				}
			case "core.addToCart":
				if conflict := convertLegacyAddToCart(node, props, product, &result); conflict != "" {
					result.Conflicts = append(result.Conflicts, LegacyDocumentConflict{NodeID: node.ID, Reason: conflict})
				}
			}
			if slot != "" {
				if _, explicit := node.Actions["list"]; !explicit {
					pageID := strings.TrimSpace(targets[slot])
					if pageID == "" {
						result.Conflicts = append(result.Conflicts, LegacyDocumentConflict{NodeID: node.ID, Slot: slot, Reason: "旧入口没有唯一有效的页面目标"})
					} else {
						if node.Actions == nil {
							node.Actions = make(map[string]core.ActionTarget)
						}
						node.Actions["list"] = core.ActionTarget{Kind: core.ActionPage, PageID: pageID}
						result.Changes = append(result.Changes, LegacyDocumentChange{NodeID: node.ID, Kind: "list", Slot: slot, PageID: pageID})
					}
				}
			}
			if err := visit(node.Children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(page.Root); err != nil {
		return result, err
	}
	if len(result.Conflicts) > 0 {
		return result, nil
	}
	if len(result.Changes) == 0 {
		result.Document = append(json.RawMessage(nil), raw...)
		return result, nil
	}
	result.Document, err = json.Marshal(page)
	return result, err
}

func convertLegacyAddToCart(node *core.Node, props map[string]json.RawMessage, product LegacyProductContext, result *LegacyDocumentConversion) string {
	keys := []string{"productSource", "productId", "variantId", "source", "optionsField", "variantsField", "currency", "variantMode", "action", "target", "cartTarget"}
	legacy := len(node.Actions) > 0
	for _, key := range keys {
		if _, ok := props[key]; ok {
			legacy = true
		}
	}
	if !legacy {
		return ""
	}
	if strings.TrimSpace(product.ProductID) == "" && !product.ProductDetailTemplate {
		return "旧加购按钮没有真实商品详情上下文，不能自动改变商品归属"
	}
	var productID string
	_ = json.Unmarshal(props["productId"], &productID)
	if productID != "" && productID != product.ProductID {
		return "旧按钮指定的商品与当前详情商品不同"
	}
	for _, key := range keys {
		delete(props, key)
	}
	if _, exists := props["label"]; !exists {
		if text, ok := props["buttonText"]; ok {
			props["label"] = text
		}
	}
	delete(props, "buttonText")
	node.Actions = nil
	var err error
	node.Props, err = json.Marshal(props)
	if err != nil {
		return "旧加购配置无法转换"
	}
	result.Changes = append(result.Changes, LegacyDocumentChange{NodeID: node.ID, Kind: "currentProduct"})
	return ""
}
