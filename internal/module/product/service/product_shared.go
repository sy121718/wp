package productservice

// 本文件是多个业务主题共用的原语：工程解析、行 JSON 兜底、媒体 URL、slug、分页与取词。
//
// 判据是「被几个文件调用」——只服务单个业务文件的原语不在这里（就近放在那个文件里，
// 例：transactionScope 在 product_variant.go）。

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
	"go_wp/pkg/upload"
	"go_wp/pkg/utils"
)

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// transactionScope 在**调用方已开启的事务**上设置工程作用域（工程 id 为空时跳过）。
//
// 为什么允许空 id 跳过：纯商品单测路径（未注入留痕端口）拿不到工程上下文，
// 而 product_variants 本就没有 RLS 策略 —— 为它编一个工程 id 只会把「没作用域」
// 伪装成「有作用域」。涉及 products（有 FORCE 策略）的路径工程 id 必非空，
// 空串在那里是编程错误，由 rls 直接拒掉，不在这里兜。
func transactionScope(tx *gorm.DB, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return nil
	}
	return rls.ScopeTx(tx, projectID)
}

// resolveProjectID 解析工程：显式指定优先，否则取唯一工程。
func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if projectID != "" {
		return projectID, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(productenums.ErrInvalidParam)
	}
	return list[0].ID, nil
}

// pageArgs 归一化分页参数。
func pageArgs(req *productdto.ListReq) (page, size int) {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	paging := utils.NormalizePaging(inPage, inSize, defaultPageSize, maxPageSize)
	return paging.Page, paging.Size
}

// optionalPaging 归一「可选分页」参数，返回 model 层接受的 (limit, offset)。
//
// 与 pageArgs 的关键差别：**两个入参都是零值时返回 (0, 0) = 不分页**。
// 品牌 / 标签的 Page、Size 是后加的可选字段，同一个请求类型上并存两种调用形态：
//
//	· 显式分页 —— 后台列表页给出 Page/Size，要的是「当页 + 总数」；
//	· 全量取数 —— 集合源筛选选项、内容翻译候选、规则重算的取数来源都传零值，
//	  它们要的是全部行。这里若按 pageArgs 那样兜底成「第 1 页 20 条」，
//	  表现为品牌筛选下拉少了一批选项、重算回执少了一批标签 —— 不报错、只是少了。
//
// 所以「分页」必须是调用方的显式意图，而不是模型层的默认行为。
func optionalPaging(page, size int) (limit, offset int) {
	if page <= 0 && size <= 0 {
		return 0, 0
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return size, (page - 1) * size
}

// normalizeSlug 规范化传入 slug（小写 + 去首尾空白）。
func normalizeSlug(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "-")
	return strings.ToLower(s)
}

// deriveSlug 由商品名派生 slug：保留字母数字、其余转连字符；中文名派生为空，由调用方兜底。
func deriveSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// orJSON jsonb 列的兜底值。
func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

// orJSONList 字符串数组转 jsonb（空数组写 []）。
func orJSONList(items []string) json.RawMessage {
	if items == nil {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(items)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// orIDList id 数组转 jsonb。
func orIDList(items []string) json.RawMessage { return orJSONList(items) }

// decodeStrings jsonb 数组 → 字符串切片（失败返回空切片，不阻断读取）。
// mediaURL 媒体地址 → 对外可用的完整链接。
//
// 商品域里凡是「取自媒体库」的字段（images / defaultImage / 变体 image /
// 分类 image / 品牌 logo）都要过它：媒体库给的是 upload.base_url 前缀下的地址，
// 未配置时是 "/storage/<id>.<ext>"。写入口归一（新数据天然完整），读出口再归一
// （存量相对值也显示对）—— 两侧共用 pkg/upload 的同一个实现，避免各写一份前缀拼接。
//
// 非媒体地址（外链 CDN / data: URI）由 StorageURL 原样返回，不会被改写。
func mediaURL(u string) string { return upload.StorageURL(u) }

// mediaURLs 逐个归一图片数组；空数组返回空数组（不是 nil），
// 免得调用方在 JSON 里拿到 null 又要各自兜一次。
func mediaURLs(list []string) []string {
	out := make([]string, 0, len(list))
	for _, u := range list {
		if s := mediaURL(u); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func decodeStrings(raw json.RawMessage) (out []string) {
	out = []string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

// normalizeProductType 归一化商品类型：空默认 variant，其余仅接受 variant / bundle。
func normalizeProductType(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", productmodel.TypeVariant:
		return productmodel.TypeVariant, nil
	case productmodel.TypeBundle:
		return productmodel.TypeBundle, nil
	default:
		return "", errors.New(productenums.ErrProductTypeInvalid)
	}
}

// product_translate.go — service 层取词函数的传递通道（后台展示文案专用）。
//
// 背景：本模块的 service 要产出**展示文案**（内置定价 / 标签规则的展示名与描述、
// 筛选条件与状态的可读标签），而它不是纯数据 —— 英文界面上必须显示英文。
// service 层没有语言上下文（语言来自后台 Cookie / Accept-Language，只有 gin.Context
// 知道），因此取词函数只能由 inbound 传进来。
//
// 为什么用 ctx 而不是给每个方法加参数：
//   - 这些文案散布在多层内部函数里（pricingStatusLabel → setPricingLineStatus →
//     computePricingLines → PreviewPricing），逐层加到 contract 方法签名会把
//     `tr func(...)` 写进一堆与展示无关的接口（预览、应用、留痕列表、抽屉数据…）；
//   - 传播方向是单向的（inbound → service → 内部函数），与 ctx 的语义一致；
//   - 未注入时的行为是**返回中文兜底**（不是裸 key），与 i18n 未初始化时的降级一致，
//     所以漏注入不会把页面打坏，只是不翻译。
//
// 写入侧只有一处：inbound 的 productTranslateMiddleware（路由组中间件），
// 它把 shell.TranslateFor(c) 放进 Request 的 Context —— handler 里
// `ctx := c.Request.Context()` 拿到的就是带取词函数的 ctx，无需逐调用点改动。
type translateCtxKey struct{}

// TranslateFunc 取词函数签名（与 shell.TranslateFor(c) 返回的函数同型）。
type TranslateFunc func(key, fallback string) string

// WithTranslate 把取词函数放进 ctx（inbound 中间件调用）。
//
// tr 为 nil 时原样返回 ctx（不写入坏值）。
func WithTranslate(ctx context.Context, tr TranslateFunc) context.Context {
	if ctx == nil || tr == nil {
		return ctx
	}
	return context.WithValue(ctx, translateCtxKey{}, tr)
}

// translateFrom 取当前请求的取词函数；未注入时返回**恒等函数**。
//
// 恒等函数的语义与 pkg/i18n.Translate 在词条缺失时一致：返回调用点给的中文兜底
// （兜底为空才退回 key）—— service 的每条文案都在调用点带中文兜底，因此未注入时
// 页面显示的就是中文原文，而不是 `admin.product_pricing.rule.costMultiple.name`
// 这种裸 key。
func translateFrom(ctx context.Context) TranslateFunc {
	if ctx != nil {
		if tr, ok := ctx.Value(translateCtxKey{}).(TranslateFunc); ok && tr != nil {
			return tr
		}
	}
	return func(key, fallback string) string {
		if fallback != "" {
			return fallback
		}
		return key
	}
}
