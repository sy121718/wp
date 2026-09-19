// Package product — Jet 渲染路径辅助导出。
//
// props 解码 / 字段解析（经 ContentResolver） / CSS 生成保留在 Go，
// HTML 拼装交给 product.jet 模板；越界字段在这里显式报错，
// 不静默渲染空块（构建期必须失败，docs/02 §冻结边界）。
package product

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
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
	// StockNote 无脚本 / 片段未接入时的库存兜底文案（审计 I18N-010）。
	StockNote string
	// Currency 货币符号（价格前缀）。
	Currency string
	// HasMedia 是否有主图。
	HasMedia bool
	// MediaURL 主图地址。
	MediaURL string
	// MediaAlt 主图替代文本（作者填写的 alt 取译文；空串时回退商品名）。
	MediaAlt string
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
	// HasOptions 是否输出规格选择器：有规格维度、且可展示的规格组合 ≥2 才输出。
	// 单变体商品（含只有无规格占位变体的商品）在前台不输出选择器（issue #8）。
	HasOptions bool
	// OptionGroups 规格维度（颜色 / 尺寸），按商品引用属性组的顺序。
	OptionGroups []OptionGroup
	// VariantOptions 规格组合行（每个组合一行：规格标签 + 价格）。
	VariantOptions []VariantOption
}

// OptionValue 规格选择器里的一个可选值。
type OptionValue struct {
	// Key 值标识（稳定，与变体 option_values 中的值一致）。
	Key string
	// Label 值显示名。
	Label string
}

// OptionGroup 一个规格维度（如颜色）。
type OptionGroup struct {
	// Key 维度标识（属性组 key）。
	Key string
	// Name 维度显示名（属性组名）。
	Name string
	// Values 可选值（按属性组内定义顺序）。
	Values []OptionValue
}

// VariantOption 一个规格组合行（前台展示用）。
type VariantOption struct {
	// ID 变体 id（issue #24）：产物里烘进「实时可用量片段」的请求参数 ——
	// 库存是运行期真源，构建期只能把 id 写进产物，可用量每次请求现取。
	ID string
	// SKUCode 变体编码。
	SKUCode string
	// Price 价格（已带货币符号）。
	Price string
	// ComparePrice 划线价（已带货币符号；空串 = 不输出）。
	ComparePrice string
	// Labels 组合的展示文本（如「颜色 红 · 尺寸 S」）。
	Labels string
	// PriceCents 构建期价（**分**）：随 LivePriceGet 一起烘进产物，供实时价格核对片段
	// 与库里的当前价比对（BIZ-2）。用分而不是元文本，是为了与跨模块的
	// VariantSnapshotPort 同一口径 —— 少一次浮点解析，就少一次「99.90 读成 99.9」的假提示。
	PriceCents int64
	// LivePriceGet 实时价格核对片段的请求 URL（构建期拼好；模板只把它放进 hx-get）。
	//
	// 为什么把「构建期价格」也放进 URL：产物是静态字节，改价只落库不进构建管线，
	// 所以片段唯一能拿来对比的基准就是**产物自己烘下来的那份价**。空串 = 价格形状不可解析，
	// 此时不请求片段（片段也只会沉默）。
	LivePriceGet string
	// StockAvailabilityGet 实时可用量片段的请求 URL（构建期拼好；模板只把它放进 hx-get）。
	//
	// 与 LivePriceGet 同族、同一理由：可用量是运行期真源（构建期只烘变体 id），
	// 而片段侧把工程 id 当必填参数（缺它无法定位库存真源），工程只能由构建期送来。
	//
	// 空串 = 缺工程 id（或变体 id 为空）：此时**不请求** —— 带空 projectId 的请求会被
	// 片段判成「缺少参数」并回 500，htmx 换不动目标节点，页面上看不出任何异常。
	StockAvailabilityGet string
}

// optionGroupJSON 商品解析器输出的规格维度结构（product.options）。
type optionGroupJSON struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Values []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	} `json:"values"`
}

// variantJSON 商品解析器输出的规格组合结构（product.variants）。
type variantJSON struct {
	ID           string            `json:"id"`
	SKU          string            `json:"sku"`
	Price        string            `json:"price"`
	ComparePrice string            `json:"comparePrice"`
	Image        string            `json:"image"`
	Enabled      bool              `json:"enabled"`
	Options      map[string]string `json:"options"`
}

// BuildView 生成商品详情渲染视图：逐槽位经内容解析器取商品字段值。
//
// content 为构建期注入的商品解析器（未注入时声明了槽位即报错，不静默出空块）；
// 解析失败（字段越界 / 类型不符）原样上抛，由编译链报错终止发布。
//
// projectID 为本次编译的站点工程 id（core.RenderContext.ProjectID）：只用于把工程烘进
// 实时价格核对片段的 URL（见 livePriceFragmentURL 的长注释）。工程为空时该片段不请求，
// 但页面其余部分照常渲染 —— 与其他「站点级资源」组件同一口径（缺工程是降级而不是失败）。
//
// lang 为本次编译的目标语言（core.RenderContext.Lang）：同样只进那两个片段 URL。
// 片段语言只从 lang 查询参数来，不带它请求就恒回落工程默认语言（英文站里价格核对位
// 会拿中文词条拼文案）；空语言（单语言站点 / 独立编译）时不带该参数。
func BuildView(p *Props, content core.ContentResolver, projectID, lang string) (View, error) {
	source := effectiveSource(p)
	slots := p.slotFields()
	declared := 0
	for _, s := range slots {
		if s.Field != "" {
			declared++
		}
	}
	if declared == 0 {
		return View{}, fmt.Errorf("至少需要声明一个商品字段（主图/图集/标题/副标题/价格/划线价/描述/规格维度/变体组合）")
	}
	if content == nil {
		return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析商品字段（数据源 %s）", source)
	}

	// StockNote 先落中文兜底：ApplyI18n 会在 BuildView 之后按语言覆盖；
	// 未接入 i18n 时它就是最终值（产物与抽 key 前逐字一致）。
	view := View{TitleTag: effectiveTitleTag(p), Currency: effectiveCurrency(p), StockNote: textFallbackStockNote}
	// 规格数据与 alt 先收原值，槽位循环结束后再统一解析（图集 alt 要按「第 i 张」
	// 对应，而 alt 槽位可能声明在图集槽位之前；组合行要按维度取标签）。
	var rawOptions, rawVariants, rawMediaAlt, rawGalleryAlt string
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
		case slotMediaAlt:
			rawMediaAlt = strings.TrimSpace(value)
		case slotGallery:
			images := parseImages(value, "")
			if len(images) > 0 {
				view.HasGallery, view.Gallery = true, images
			}
		case slotGalleryAlt:
			rawGalleryAlt = strings.TrimSpace(value)
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
		case slotOptions:
			rawOptions = value
		case slotVariants:
			rawVariants = value
		}
	}
	// 图集 alt（issue #12）：按位填入作者填写的 alt（逐元素已按构建语言取译文）；
	// 缺位 / 空串回退商品名（无商品名时留空 = 装饰性图片，模板仍输出 alt=""）。
	galleryAlts := parseAltList(rawGalleryAlt)
	for i := range view.Gallery {
		if i < len(galleryAlts) && galleryAlts[i] != "" {
			view.Gallery[i].Alt = galleryAlts[i]
			continue
		}
		if view.HasTitle {
			view.Gallery[i].Alt = view.Title
		}
	}
	// 主图 alt：作者填了就用它（取译文后），否则回退商品名。
	if view.HasMedia && rawMediaAlt != "" {
		view.MediaAlt = rawMediaAlt
	} else if view.HasMedia && view.HasTitle {
		view.MediaAlt = view.Title
	}
	// 规格选择器：有维度且可展示的组合 ≥2 才输出 —— 单变体商品不输出选择器。
	view.OptionGroups = ParseOptionGroups(rawOptions)
	view.VariantOptions = ParseVariantOptions(rawVariants, view.OptionGroups, view.Currency, projectID, lang)
	view.HasOptions = len(view.OptionGroups) > 0 && len(view.VariantOptions) > 1
	return view, nil
}

// ParseOptionGroups 规格维度 JSON → 视图结构。
//
// 结构对不上（空串 / 非法 JSON / 旧形态）时返回空：选择器不输出，
// 而不是让整个商品详情页构建失败（字段本身已由白名单校验过合法性）。
func ParseOptionGroups(raw string) []OptionGroup {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var rows []optionGroupJSON
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	out := make([]OptionGroup, 0, len(rows))
	for _, r := range rows {
		if r.Key == "" {
			continue
		}
		values := make([]OptionValue, 0, len(r.Values))
		for _, v := range r.Values {
			if v.Key == "" {
				continue
			}
			values = append(values, OptionValue{Key: v.Key, Label: v.Label})
		}
		if len(values) == 0 {
			continue
		}
		out = append(out, OptionGroup{Key: r.Key, Name: r.Name, Values: values})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ParseVariantOptions 规格组合 JSON → 视图行（只保留能对上全部维度的组合）。
//
// 两条过滤规则：
//   - 无规格组合（option_values 为空）不进规格清单 —— 它是商品的占位 / 手工变体，
//     不是规格选择器里的一格；
//   - 未启用的变体不上架，因而不出现在选择器里（组合计数也不含它）。
//
// projectID 只透传给实时价格核对片段的 URL（见 livePriceFragmentURL）：三个调用方
// （商品详情 / 独立选择器 / 加购）都必须给出自己那份构建上下文里的工程 id。
//
// lang 与 projectID 一样只透传给那两个片段 URL：片段语言只能从 lang 查询参数来（见 BuildView）。
func ParseVariantOptions(raw string, groups []OptionGroup, currency, projectID, lang string) []VariantOption {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(groups) == 0 {
		return nil
	}
	var rows []variantJSON
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	labels := make(map[string]map[string]string, len(groups))
	for _, g := range groups {
		m := make(map[string]string, len(g.Values))
		for _, v := range g.Values {
			m[v.Key] = v.Label
		}
		labels[g.Key] = m
	}
	out := make([]VariantOption, 0, len(rows))
	for _, r := range rows {
		if !r.Enabled || len(r.Options) == 0 {
			continue
		}
		parts := make([]string, 0, len(groups))
		complete := true
		for _, g := range groups {
			key, ok := r.Options[g.Key]
			if !ok {
				complete = false
				break
			}
			label, ok := labels[g.Key][key]
			if !ok {
				complete = false
				break
			}
			name := g.Name
			if name == "" {
				name = g.Key
			}
			parts = append(parts, name+" "+label)
		}
		if !complete {
			continue
		}
		row := VariantOption{
			ID:      r.ID,
			SKUCode: r.SKU, Labels: strings.Join(parts, " · "),
			Price: currency + r.Price,
		}
		// 实时可用量位（issue #24）：与价格核对位同形 —— 构建期只烘「变体 id + 工程 id」，
		// 可用量每次请求现取（烘进产物等于发布一份过期库存）。
		row.StockAvailabilityGet = stockAvailabilityFragmentURL(r.ID, projectID, lang)
		// 实时价格核对（BIZ-2）：价格形状可解析时才烘 URL —— 解析不出来的价拿去比对
		// 只会得到一句错话，不如不请求。
		if cents, ok := priceYuanCents(r.Price); ok {
			row.PriceCents = cents
			row.LivePriceGet = livePriceFragmentURL(r.ID, cents, currency, projectID, lang)
		}
		if strings.TrimSpace(r.ComparePrice) != "" {
			row.ComparePrice = currency + r.ComparePrice
		}
		out = append(out, row)
	}
	return out
}

// LivePriceFragmentPath 实时价格核对片段端点（与 runtimefragment 的 capability 名一致）。
const LivePriceFragmentPath = "/_fragments/productLivePrice"

// StockAvailabilityFragmentPath 实时可用量片段端点（与 runtimefragment 的 capability 名一致）。
const StockAvailabilityFragmentPath = "/_fragments/productVariantAvailability"

// paramProjectID 片段参数名：站点工程 id。
//
// 商品侧的两个片段（实时价格核对 / 实时可用量）都以它为**必填**参数，runtimefragment 侧
// 各有同名常量（livePriceParamProjectID / variantAvailabilityParamProjectID）。
// 这里收敛成一个常量：组件侧只有一处真源，就不会出现「改了一个 URL、忘了另一个」。
const paramProjectID = "projectId"

// livePriceFragmentURL 拼实时价格核对片段的请求 URL（逐变体一行一个请求，与库存片段同构）。
//
// 参数四个：变体 id、构建期价（分）、货币符号、站点工程 id。
//
// **为什么工程必须烘进 URL**（审计 DB-009）：片段端经 VariantSnapshotPort 取「这个变体
// 此刻的价与启用态」，而该端口必带工程作用域 —— products 在迁移 215 的 RLS 名单里，
// 没有作用域的读取在非超级角色下**静默返回 0 行**。片段查不到变体就只能沉默（降级设计），
// 于是整条价格核对链一声不响地失效。而 runtimefragment.Request 里没有工程字段
// （只有 Params / Cookies / UserID），工程只能由唯一知道它的构建期送来。
//
// 代价是**已发布产物的字节会变**，这是预期的组件升级而不是缺陷：组件清单指纹变化 ⇒
// builder.RegistryVersion() 与 page_artifacts.registry_version 不等 ⇒ 启动时把相关页面
// 标记 stale（只标记、不重建）⇒ 运维经 page.RebuildStale 重建。不要为了「字节不变」把工程
// 塞进 header / 包级变量 / 从 referer 推断 —— 那是把一次可审计的升级换成隐式状态。
//
// 工程为空时返回空串（不请求片段）：URL 里带一个空的 projectId 会被片段端判成「缺少参数」
// 并返回 500，与「价格形状不可解析就不请求」是同一条判断 —— 没有工程就没有核对可言。
//
// lang 同理必须进 URL（I18N-011）：片段语言只从 lang 参数来，端点不读 Accept-Language、
// 不读任何语言 cookie；不带它请求就恒回落工程默认语言，英文站的核对文案会是中文。
// 语言为空（单语言站点 / 独立编译）时不带该参数 —— 空值只让片段多做一次无用判断。
func livePriceFragmentURL(variantID string, cents int64, currency, projectID, lang string) string {
	if strings.TrimSpace(variantID) == "" {
		return ""
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ""
	}
	q := url.Values{}
	q.Set(paramProjectID, projectID)
	q.Set("variantIds", variantID)
	q.Set("prices", strconv.FormatInt(cents, 10))
	if strings.TrimSpace(currency) != "" {
		q.Set("currency", currency)
	}
	if l := strings.TrimSpace(lang); l != "" {
		q.Set("lang", l)
	}
	return LivePriceFragmentPath + "?" + q.Encode()
}

// stockAvailabilityFragmentURL 拼实时可用量片段的请求 URL（逐变体一行一个请求）。
//
// 与 livePriceFragmentURL 同一形状、同一理由（工程必进 URL，见那个函数的长注释）：
// 片段侧 renderVariantAvailability 把 projectId 当**必填**参数（缺它无法定位库存真源），
// 而 runtimefragment.Request 里没有工程字段，工程只能由构建期烘进来。
//
// 缺工程 id 时返回空串：不请求一个必然被判「缺少参数」的 URL。调用方（模板）据此不输出
// hx-get，该位退化为纯兜底文案 —— 访客看到的是「以结算时库存为准」，而不是一个永远空着的位。
//
// lang 与工程同理进 URL（I18N-011）：片段语言只从它来，不带就恒回落工程默认语言。
// 为空时不带该参数。
func stockAvailabilityFragmentURL(variantID, projectID, lang string) string {
	if strings.TrimSpace(variantID) == "" {
		return ""
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ""
	}
	q := url.Values{}
	q.Set(paramProjectID, projectID)
	q.Set("variantIds", variantID)
	if l := strings.TrimSpace(lang); l != "" {
		q.Set("lang", l)
	}
	return StockAvailabilityFragmentPath + "?" + q.Encode()
}

// priceYuanCents 元文本（商品字段解析器 formatPrice 的输出形态，如 "99" / "99.5"）→ 分。
//
// 解析失败或负值返回 false：调用方据此不请求片段（而不是按 0 元比对，
// 那会把每个变体都判成「价格已更新」）。
func priceYuanCents(text string) (int64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}
	return int64(math.Round(v * 100)), true
}

// parseAltList 图集 alt 字段值（JSON 字符串数组）→ alt 列表。
//
// 结构对不上（空串 / 非法 JSON）返回空列表：调用方逐位回退商品名，
// 不让整个商品详情页构建失败。元素中的纯空白按「未填写」处理。
func parseAltList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var alts []string
	if err := json.Unmarshal([]byte(raw), &alts); err != nil {
		return nil
	}
	out := make([]string, 0, len(alts))
	for _, a := range alts {
		out = append(out, strings.TrimSpace(a))
	}
	return out
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

// DeclareFeatures 实现 core.ViewFeatureDeclarer（审计 PERF-014）：商品详情的规格组合行
// 恒定挂「实时可用量」片段（库存是运行期真源，构建期只烘变体 id），并可选挂实时价格
// 核对位。没有可展示的规格组合时模板不渲染这一段，属性也就不存在。
func (v View) DeclareFeatures() (attrs, classes []string) {
	if !v.HasOptions || len(v.VariantOptions) == 0 {
		return nil, nil
	}
	// 按**实际烘出的 URL** 判断，而不是按「有规格组合」：缺工程 id 时两个片段位都不输出，
	// 此时登记 hx-* 会让产物白白多注入一份 htmx 脚本（PERF-014 的登记就是为这件事设的）。
	for _, vo := range v.VariantOptions {
		if vo.LivePriceGet != "" || vo.StockAvailabilityGet != "" {
			return []string{"hx-get", "hx-trigger", "hx-swap"}, nil
		}
	}
	return nil, nil
}
