package feature

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
)

func cfCreate(f *attrFixture, form url.Values) (*productdto.ProductResp, error) {
	return cfCreateWith(f.svc, form)
}

func cfCreateWith(svc *productservice.Service, form url.Values) (*productdto.ProductResp, error) {
	req := &productdto.CreateReq{
		ProjectID:     form.Get("projectId"),
		Name:          form.Get("name"),
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

func cfBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}
