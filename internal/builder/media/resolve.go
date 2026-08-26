package media

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// ResolveMedia 实现 core.MediaResolver（规范 §4 构建期变体注入）：
// assetId + 期望规格 → 稳定 URL、宽高、同格式 srcset 与现代格式 <source> 集合、全局 Alt。
//
// 图片/SVG 走变体选择；视频/文档返回原文件基础元数据（无变体语义）。
func (s *Store) ResolveMedia(assetID, variant string) (meta *core.MediaMeta, err error) {
	asset, err := s.Get(assetID)
	if err != nil {
		return nil, err
	}

	meta = &core.MediaMeta{
		Type:     asset.Type,
		MimeType: asset.MimeType,
		Alt:      asset.Alt,
		Title:    asset.Title,
	}

	// 视频/文档：无变体语义，直接返回原文件元数据。
	if asset.Type != core.MediaTypeImage && asset.Type != core.MediaTypeSVG {
		meta.URL = stableURL(asset)
		return meta, nil
	}

	if variant == "" {
		variant = VariantOriginal
	}
	if _, ok := variantWidthRank[variant]; !ok {
		return nil, fmt.Errorf("无效的变体规格: %q", variant)
	}

	base := selectVariant(asset.Variants, variant)
	if base == nil {
		return nil, fmt.Errorf("媒体资产 %s 缺少 %s 变体", assetID, variant)
	}

	meta.URL = base.URL
	meta.Width = base.Width
	meta.Height = base.Height

	// srcset：与选中变体同格式的全部尺寸，按宽度升序（浏览器自选最优）。
	meta.Srcset = buildSrcset(asset.Variants, base.Format)

	// 现代格式 source：WebP / AVIF 分组各建一条（<picture> 内按声明顺序优先）。
	for _, format := range []string{"avif", "webp"} {
		if ss := buildSrcset(asset.Variants, format); ss != "" {
			meta.Sources = append(meta.Sources, core.PictureSource{
				Type:   "image/" + format,
				Srcset: ss,
			})
		}
	}
	return meta, nil
}

// stableURL 非 图片类资产的稳定访问地址。
// 当前内存实现返回内容寻址占位；生产实现由媒体存储层提供（如 /media/<assetId>/<hash>.pdf）。
func stableURL(a *Asset) string {
	return "/media/" + a.ID + "/" + a.Hash[:12] + "/" + a.FileName
}

// selectVariant 按期望规格选择基础变体：精确匹配 → 不小于期望宽度的最小者 → original。
func selectVariant(variants []Variant, want string) (v *Variant) {
	rank, ok := variantWidthRank[want]
	if !ok {
		rank = variantWidthRank[VariantOriginal]
	}
	var exact, larger, original *Variant
	for i := range variants {
		cur := variants[i]
		switch {
		case cur.Kind == want:
			exact = &variants[i]
		case cur.Width >= rank && (larger == nil || cur.Width < larger.Width):
			larger = &variants[i]
		case cur.Kind == VariantOriginal:
			original = &variants[i]
		}
	}
	for _, cand := range []*Variant{exact, larger, original} {
		if cand != nil {
			return cand
		}
	}
	if len(variants) > 0 {
		return &variants[0]
	}
	return nil
}

// buildSrcset 把同格式的变体按宽度升序拼为 srcset 值（"url 320w, url 768w"）。
func buildSrcset(variants []Variant, format string) string {
	var picked []Variant
	for _, v := range variants {
		if v.Format == format && v.Width > 0 && v.URL != "" {
			picked = append(picked, v)
		}
	}
	sort.Slice(picked, func(i, j int) bool { return picked[i].Width < picked[j].Width })
	parts := make([]string, 0, len(picked))
	for _, v := range picked {
		parts = append(parts, v.URL+" "+strconv.Itoa(v.Width)+"w")
	}
	return strings.Join(parts, ", ")
}