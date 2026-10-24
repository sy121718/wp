package feature

// product_create_form_helper_test.go — 新建表单的提交值解析（三个建商品链路用例共用）。
//
// 页面写 handler 已迁到 JSON 写基座（static/js/ui/write-base.js + /api/product/create），
// 于是这些用例不再能通过「POST 表单拿跳转」来驱动 —— 但它们验的是 service 的行为
//（多仓各建一行、捆绑 SKU 补齐、初始库存），驱动方式换成直调 service 即可。
// 解析口径与基座一致：多值收数组、数值字段转数字、留空即不设置。

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
)

// cfCreate 按新建表单的字段集合建一个商品（attrFixture 版）。
func cfCreate(f *attrFixture, form url.Values) (*productdto.ProductResp, error) {
	return cfCreateWith(f.svc, form)
}

// cfCreateWith 同上，但直接收 service（仓库侧另有一套 fixture，字段名不同）。
func cfCreateWith(svc *productservice.Service, form url.Values) (*productdto.ProductResp, error) {
	req := &productdto.CreateReq{
		ProjectID:     form.Get("projectId"),
		Name:          form.Get("name"),
		Type:          form.Get("type"),
		Slug:          form.Get("slug"),
		SKUCode:       form.Get("sku"),
		ExternalSKU:   form.Get("externalSku"),
		AttributeIDs:  form["attributeIds"],
		WarehouseIDs:  form["warehouseIds"],
		TrackQuantity: cfBool(form.Get("trackQuantity")),
	}
	if v := strings.TrimSpace(form.Get("defaultPrice")); v != "" {
		if price, err := strconv.ParseFloat(v, 64); err == nil {
			req.DefaultPrice = &price
		}
	}
	if v := strings.TrimSpace(form.Get("quantity")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			req.Quantity = &n
		}
	}
	return svc.Create(context.Background(), req)
}

// cfBool 复选框的取值口径（浏览器提交 "on"，脚本提交 "1" / "true"）。
func cfBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}
