package productbinding

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	json "go_wp/pkg/jsonx"
)

const (
	Current  = "current"
	Specific = "specific"
)

func Validate(mode, id string) error {
	if mode != "" && mode != Current && mode != Specific {
		return fmt.Errorf("未知的商品来源 %q", mode)
	}
	if id != "" && uuid.Validate(id) != nil {
		return fmt.Errorf("无效的商品引用")
	}
	if mode == Specific && strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: 请先选择商品", core.ErrIncompleteNode)
	}
	return nil
}

func Resolve(ctx *core.RenderContext, mode, productID string) (core.ContentResolver, error) {
	if err := Validate(mode, productID); err != nil {
		return nil, err
	}
	if mode != Specific {
		if ctx == nil || ctx.Content == nil {
			return nil, fmt.Errorf("%w: 当前页面没有商品，请改用指定商品", core.ErrIncompleteNode)
		}
		return ctx.Content, nil
	}
	if ctx == nil || ctx.Product == nil {
		return nil, fmt.Errorf("指定商品的数据源尚未装配")
	}
	reader, ok := ctx.Product.(productcontract.PublicProductOptionsReader)
	if !ok {
		return nil, fmt.Errorf("指定商品的数据源缺少规格读取能力")
	}
	data, err := reader.PublicProductOptions(ctx.Context, &productdto.PublicProductOptionsReq{ProjectID: ctx.ProjectID, ProductID: productID, Lang: ctx.Lang})
	if err != nil {
		return nil, err
	}
	if data == nil || data.ID != productID {
		return nil, fmt.Errorf("指定商品返回了不匹配的数据")
	}

	type value struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	type group struct {
		Key    string  `json:"key"`
		Name   string  `json:"name"`
		Values []value `json:"values"`
	}
	groups := make([]group, 0, len(data.Options))
	for _, option := range data.Options {
		g := group{Key: option.Key, Name: option.Name, Values: []value{}}
		for _, v := range option.Values {
			g.Values = append(g.Values, value{Key: v.Key, Label: v.Name})
		}
		groups = append(groups, g)
	}
	type variant struct {
		ID           string            `json:"id"`
		SKU          string            `json:"sku"`
		Price        string            `json:"price"`
		ComparePrice string            `json:"comparePrice,omitempty"`
		Enabled      bool              `json:"enabled"`
		Options      map[string]string `json:"options"`
	}
	variants := make([]variant, 0, len(data.Variants))
	for _, v := range data.Variants {
		row := variant{ID: v.ID, SKU: v.SKU, Price: decimal(v.Price), Enabled: true, Options: v.Options}
		if v.ComparePrice != nil {
			row.ComparePrice = decimal(*v.ComparePrice)
		}
		variants = append(variants, row)
	}
	optionsJSON, err := json.Marshal(groups)
	if err != nil {
		return nil, err
	}
	variantsJSON, err := json.Marshal(variants)
	if err != nil {
		return nil, err
	}
	return fields{"product.options": string(optionsJSON), "product.variants": string(variantsJSON)}, nil
}

type fields map[string]string

func (f fields) ResolveString(key string) (string, error) {
	if value, ok := f[key]; ok {
		return value, nil
	}
	return "", fmt.Errorf("商品规格不提供字段 %q", key)
}

func decimal(cents int64) string {
	negative := ""
	if cents < 0 {
		negative, cents = "-", -cents
	}
	digits := strconv.FormatInt(cents, 10)
	for len(digits) < 3 {
		digits = "0" + digits
	}
	return negative + digits[:len(digits)-2] + "." + digits[len(digits)-2:]
}
