package orderdto

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type ShipLineReq struct {
	OrderItemID uint64 `form:"orderItemId" json:"orderItemId" binding:"required"`

	WarehouseID string `form:"warehouseId" json:"warehouseId"`
	Quantity    int    `form:"quantity" json:"quantity"`
}

type ShipOrderReq struct {
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	ProjectID string `form:"projectId" json:"projectId"`
	RequestID string `form:"requestId" json:"requestId"`

	CarrierID   string `form:"carrierId" json:"carrierId"`
	CarrierName string `form:"carrierName" json:"carrierName"`
	TrackingNo  string `form:"trackingNo" json:"trackingNo"`
	Remark      string `form:"remark" json:"remark"`

	OperatorID   int64         `form:"operatorId" json:"operatorId"`
	OperatorName string        `form:"operatorName" json:"operatorName"`
	Lines        []ShipLineReq `form:"lines" json:"lines" binding:"required"`
}

// shipLineKeyRe 匹配表单交上来的行控件名：lines[<group>][<field>]。
//
// 为什么需要 BindForm：这个表单是 multipart，浏览器只能把 name 原样送上来，
// 而 gin 的表单绑定按 tag 精确取键（form["lines"]），lines[1701][quantity]
// 与 "lines" 不相等 —— ShouldBind 不报错，Lines 静默为 nil。
var shipLineKeyReValue = regexp.MustCompile(`^lines\[([^\]]+)\]\[([A-Za-z0-9_]+)\]$`)

// shipLineFieldWhitelist 只认这三个键；多带的键忽略而不是报错。
//
// 为什么忽略：浏览器与写基座可能顺手带上别的控件，为多一个键整单 400
// 是把内部实现细节变成用户错误。
var shipLineFieldWhitelist = map[string]bool{
	"orderItemId": true,
	"quantity":    true,
	"warehouseId": true,
}

// BindForm 把 lines[<下标>][<字段>] 形状的扁平键收成 []ShipLineReq。
//
// 语义三条（与 planShipment 的分工一致，service 侧不需要改）：
//  1. 所有行都没填 → Lines 为空，planShipment 报 ErrShipLinesRequired；
//  2. 部分行有值 → 只收有值的行，其余忽略（部分发货的常规操作）；
//  3. 显式填 0 → 保留，由 planShipment 的 Quantity <= 0 报 ErrQuantityInvalid。
//
// OrderItemID 以**值**为准、下标只用于分组：下标由服务端渲染，被改了不该改变语义；
// 值来自客户端，归属校验在 planShipment（order_shipment.go:238 按订单行集合比对）。
func (r *ShipOrderReq) BindForm(vals url.Values) error {
	type lineAcc struct {
		itemID      uint64
		quantity    int
		hasQuantity bool
		warehouseID string
	}
	accs := make(map[string]*lineAcc, 4)
	order := make([]string, 0, 4)
	for key, values := range vals {
		matched := shipLineKeyReValue.FindStringSubmatch(key)
		if matched == nil {
			continue
		}
		field := matched[2]
		if !shipLineFieldWhitelist[field] {
			continue
		}
		if len(values) == 0 {
			continue
		}
		raw := strings.TrimSpace(values[0])
		group := matched[1]
		acc, seen := accs[group]
		if !seen {
			acc = &lineAcc{}
			accs[group] = acc
			order = append(order, group)
		}
		switch field {
		case "orderItemId":
			if id, err := strconv.ParseUint(raw, 10, 64); err == nil {
				acc.itemID = id
			}
		case "quantity":
			// 空串与非法值都当"这行没填数量"，不在这里报错，也不在这里判 0。
			if id, err := strconv.Atoi(raw); err == nil {
				acc.quantity = id
				acc.hasQuantity = true
			}
		case "warehouseId":
			acc.warehouseID = raw
		}
	}

	lines := make([]ShipLineReq, 0, len(order))
	for _, group := range order {
		acc := accs[group]
		if acc == nil || acc.itemID == 0 || !acc.hasQuantity {
			continue
		}
		lines = append(lines, ShipLineReq{
			OrderItemID: acc.itemID,
			WarehouseID: acc.warehouseID,
			Quantity:    acc.quantity,
		})
	}
	if len(lines) > 0 {
		r.Lines = lines
	}
	return nil
}

type ListShipmentReq struct {
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	ProjectID string `form:"projectId" json:"projectId"`
}

type CarrierCreateReq struct {
	ProjectID           string `form:"projectId" json:"projectId"`
	Code                string `form:"code" json:"code" binding:"required"`
	Name                string `form:"name" json:"name" binding:"required"`
	TrackingURLTemplate string `form:"trackingUrlTemplate" json:"trackingUrlTemplate"`
	Sort                int    `form:"sort" json:"sort"`
}

type CarrierUpdateReq struct {
	ID        string  `form:"id" json:"id" binding:"required"`
	ProjectID string  `form:"projectId" json:"projectId"`
	Code      *string `form:"code" json:"code"`
	Name      *string `form:"name" json:"name"`

	TrackingURLTemplate *string `form:"trackingUrlTemplate" json:"trackingUrlTemplate"`
	Status              *string `form:"status" json:"status"`
	Sort                *int    `form:"sort" json:"sort"`
}

type ListCarrierReq struct {
	ProjectID string `form:"projectId" json:"projectId"`

	IncludeDisabled bool `form:"includeDisabled" json:"includeDisabled"`
}

type PendingShipReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Keyword   string `form:"keyword" json:"keyword"`
	Offset    int    `form:"offset" json:"offset"`
	Limit     int    `form:"limit" json:"limit"`
}

type ShipmentPageQueryReq struct {
	ProjectID string `form:"project" json:"project"`
	Keyword   string `form:"keyword" json:"keyword"`
	Page      int    `form:"page" json:"page"`
	Limit     int    `form:"limit" json:"limit"`
}
