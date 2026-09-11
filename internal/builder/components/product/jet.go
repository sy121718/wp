// Package product — Jet 渲染路径辅助导出。
//
// props 解码 / 字段解析（经 ContentResolver） / CSS 生成保留在 Go，
// HTML 拼装交给 product.jet 模板；越界字段在这里显式报错，
// 不静默渲染空块（构建期必须失败，docs/02 §冻结边界）。
package product

import (
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 商品详情样式编译（媒体 + 信息区两栏，窄容器自动纵向堆叠）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// Image 单张图片的渲染数据。
type Image struct {
	// URL 图片地址（模板输出时由 Jet 默认转义）。
	URL string
	// Alt 替代文本（空串合法 = 装饰性图片；模板始终输出 alt 属性）。
	Alt string
	// Eager 是否立即加载（首图优先，其余懒加载）。
	Eager bool
}

// View 商品详情渲染视图（供 product.jet 模板使用）。
type View struct {
	// TitleTag 标题标签名（h1~h3）。
	TitleTag string
	// Currency 货币符号（价格前缀）。
	Currency string
	// HasMedia 是否有主图。
	HasMedia bool
	// MediaURL 主图地址。
	MediaURL string
	// HasGallery 是否有图集。
	HasGallery bool
	// Gallery 图集（多图，按商品图集顺序）。
	Gallery []Image
	// HasTitle / Title 商品名。
	HasTitle bool
	Title    string
	// HasSubtitle / Subtitle 卖点。
	HasSubtitle bool
	Subtitle    string
	// HasPrice / Price 价格（单变体为一口价，多变体为区间）。
	HasPrice bool
	Price    string
	// HasComparePrice / ComparePrice 划线价（有折扣时显示）。
	HasComparePrice bool
	ComparePrice    string
	// HasDescription / DescriptionHTML 描述（已富文本白名单清洗）。
	HasDescription  bool
	DescriptionHTML string
}

// BuildView 生成商品详情渲染视图：逐槽位经内容解析器取商品字段值。
//
// content 为构建期注入的商品解析器（未注入时声明了槽位即报错，不静默出空块）；
// 解析失败（字段越界 / 类型不符）原样上抛，由编译链报错终止发布。
func BuildView(p *Props, content core.ContentResolver) (View, error) {
	source := effectiveSource(p)
	slots := p.slotFields()
	declared := 0
	for _, s := range slots {
		if s.Field != "" {
			declared++
		}
	}
	if declared == 0 {
		return View{}, fmt.Errorf("至少需要声明一个商品字段（主图/图集/标题/副标题/价格/划线价/描述）")
	}
	if content == nil {
		return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析商品字段（数据源 %s）", source)
	}

	view := View{TitleTag: effectiveTitleTag(p), Currency: effectiveCurrency(p)}
	for _, s := range slots {
		if s.Field == "" {
			continue
		}
		value, err := content.ResolveString(s.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析商品字段 %q 失败: %w", s.Field, err)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		switch s.Slot {
		case slotMedia:
			view.HasMedia, view.MediaURL = true, strings.TrimSpace(value)
		case slotGallery:
			images := parseImages(value, view.Title)
			if len(images) > 0 {
				view.HasGallery, view.Gallery = true, images
			}
		case slotTitle:
			view.HasTitle, view.Title = true, value
		case slotSubtitle:
			view.HasSubtitle, view.Subtitle = true, value
		case slotPrice:
			view.HasPrice, view.Price = true, view.Currency+value
		case slotComparePrice:
			view.HasComparePrice, view.ComparePrice = true, view.Currency+value
		case slotDescription:
			view.HasDescription, view.DescriptionHTML = true, core.RichTextHTML(value)
		}
	}
	// 图集 alt 用商品名兜底（商品名槽位可能声明在图集之后，这里补齐）。
	if view.HasGallery && view.HasTitle {
		for i := range view.Gallery {
			if view.Gallery[i].Alt == "" {
				view.Gallery[i].Alt = view.Title
			}
		}
	}
	return view, nil
}

// parseImages 图集字段值 → 图片列表。
//
// 商品图集字段是 JSON 数组字符串（商品解析器按 JSON 数组输出，core.gallery 亦按此解析）；
// 兼容逗号分隔与单个 URL 两种退化形态。
func parseImages(raw, alt string) (images []Image) {
	var urls []string
	if err := json.Unmarshal([]byte(raw), &urls); err != nil {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				urls = append(urls, part)
			}
		}
	}
	for _, u := range urls {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		images = append(images, Image{URL: u, Alt: alt, Eager: len(images) == 0})
	}
	return images
}
