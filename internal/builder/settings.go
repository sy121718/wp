package builder

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go_wp/pkg/builder/core"

	"golang.org/x/crypto/bcrypt"
)

const (
	LayoutBoxed = "boxed"
	LayoutFull  = "full"
)

const (
	bodyClassPage  = "sky-page"
	bodyClassBoxed = "sky-boxed"
	bodyClassFull  = "sky-full"
)

var bodyClassRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

type PageSettings struct {
	Layout PageLayout `json:"layout"`
	Base   BaseStyle  `json:"base"`

	Theme *ThemeSettings `json:"theme,omitempty"`

	ThemeOverride *ThemeSettings `json:"themeOverride,omitempty"`
	SEO           SEO            `json:"seo"`
	BodyClasses   []string       `json:"bodyClasses,omitempty"`

	Access *AccessGuardSettings `json:"access,omitempty"`

	Structure StructureBindings `json:"structure,omitempty"`
}

type StructureBindings struct {
	HeaderBlockID string `json:"headerBlockId,omitempty"`

	FooterBlockID string `json:"footerBlockId,omitempty"`

	Slots map[string]string `json:"slots,omitempty"`

	HeaderTemplateID string `json:"headerTemplateId,omitempty"`
	FooterTemplateID string `json:"footerTemplateId,omitempty"`

	SlotTemplates map[string]string `json:"slotTemplates,omitempty"`
}

func (s StructureBindings) TemplateBindings() map[string]string {
	out := make(map[string]string, len(s.SlotTemplates)+2)
	for slot, templateID := range s.SlotTemplates {
		if id := strings.TrimSpace(templateID); id != "" {
			out[slot] = id
		}
	}
	if id := strings.TrimSpace(s.HeaderTemplateID); id != "" && out[SlotHeader] == "" {
		out[SlotHeader] = id
	}
	if id := strings.TrimSpace(s.FooterTemplateID); id != "" && out[SlotFooter] == "" {
		out[SlotFooter] = id
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s StructureBindings) SlotBindings() map[string]string {
	out := make(map[string]string, len(s.Slots)+2)
	for slot, blockID := range s.Slots {
		if id := strings.TrimSpace(blockID); id != "" {
			out[slot] = id
		}
	}
	if id := strings.TrimSpace(s.HeaderBlockID); id != "" && out[SlotHeader] == "" {
		out[SlotHeader] = id
	}
	if id := strings.TrimSpace(s.FooterBlockID); id != "" && out[SlotFooter] == "" {
		out[SlotFooter] = id
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s StructureBindings) IsEmpty() bool {
	return len(s.SlotBindings()) == 0 && len(s.TemplateBindings()) == 0
}

type PageLayout struct {
	Mode string `json:"mode"`

	MaxWidth string `json:"maxWidth,omitempty"`

	MainLandmark bool `json:"mainLandmark,omitempty"`

	SafePadding struct {
		Desktop string `json:"desktop,omitempty"`
		Tablet  string `json:"tablet,omitempty"`
		Mobile  string `json:"mobile,omitempty"`
	} `json:"safePadding,omitempty"`
}

type BaseStyle struct {
	BackgroundColor string `json:"backgroundColor,omitempty"`

	BackgroundImage string `json:"backgroundImage,omitempty"`

	BackgroundFixed bool `json:"backgroundFixed,omitempty"`
}

type ProductOfferLD struct {
	SKU           string  `json:"sku,omitempty"`
	Price         string  `json:"price,omitempty"`
	PriceCurrency string  `json:"priceCurrency,omitempty"`
	Availability  string  `json:"availability,omitempty"`
	RatingValue   float64 `json:"ratingValue,omitempty"`
	RatingCount   int     `json:"ratingCount,omitempty"`
}

type SEO struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`

	FocusKeyword string `json:"focusKeyword,omitempty"`

	SecondaryKeywords []string `json:"secondaryKeywords,omitempty"`

	Canonical string `json:"canonical,omitempty"`

	OGImage string `json:"ogImage,omitempty"`

	Intent string `json:"intent,omitempty"`

	SchemaType string `json:"schemaType,omitempty"`

	ProductOffer *ProductOfferLD `json:"productOffer,omitempty"`

	RobotsIndex string `json:"robotsIndex,omitempty"`

	RobotsFollow string `json:"robotsFollow,omitempty"`
}

const (
	AccessPublic = "public"

	AccessPassword = "password"

	AccessMembers = "members"
)

const MaxAccessPasswordBytes = 72

type AccessGuardSettings struct {
	Type string `json:"type,omitempty"`

	PasswordHash string `json:"passwordHash,omitempty"`
}

func (a *AccessGuardSettings) IsPublic() bool {
	if a == nil {
		return true
	}
	return a.Type == "" || a.Type == AccessPublic
}

func (a *AccessGuardSettings) TypeOrDefault() string {
	if a == nil || a.Type == "" {
		return AccessPublic
	}
	return a.Type
}

func validateSettings(s *PageSettings) (err error) {
	switch s.Layout.Mode {
	case LayoutBoxed, LayoutFull:
	default:
		return fmt.Errorf("无效的版心模式: %q", s.Layout.Mode)
	}

	if s.Layout.Mode == LayoutBoxed {
		if s.Layout.MaxWidth == "" {
			return errors.New("定宽模式下必须设置最大内容宽度")
		}
		if !safeCSS(s.Layout.MaxWidth) {
			return fmt.Errorf("无效的最大内容宽度值: %q", s.Layout.MaxWidth)
		}
	}

	for bp, v := range map[string]string{
		"desktop": s.Layout.SafePadding.Desktop,
		"tablet":  s.Layout.SafePadding.Tablet,
		"mobile":  s.Layout.SafePadding.Mobile,
	} {
		if v != "" && !safeCSS(v) {
			return fmt.Errorf("无效的 %s 端安全留白值: %q", bp, v)
		}
	}

	if !safeCSS(s.Base.BackgroundColor) {
		return fmt.Errorf("无效的背景颜色值: %q", s.Base.BackgroundColor)
	}
	if s.Base.BackgroundImage != "" && !safeCSS(s.Base.BackgroundImage) {
		return fmt.Errorf("无效的背景图地址: %q", s.Base.BackgroundImage)
	}

	if len(s.SEO.Title) > 200 {
		return errors.New("页面标题过长（上限 200 字符）")
	}
	if len(s.SEO.Description) > 500 {
		return errors.New("页面描述过长（上限 500 字符）")
	}

	if s.ThemeOverride != nil {
		if err := ValidateThemeSettings(s.ThemeOverride); err != nil {
			return fmt.Errorf("页面主题覆盖非法: %w", err)
		}
	}

	switch s.SEO.RobotsIndex {
	case "", robotIndex, robotNoIndex:
	default:
		return fmt.Errorf("无效的 robots 索引指令: %q", s.SEO.RobotsIndex)
	}
	switch s.SEO.RobotsFollow {
	case "", robotFollow, robotNoFollow:
	default:
		return fmt.Errorf("无效的 robots 跟踪指令: %q", s.SEO.RobotsFollow)
	}

	if err := validateAccessSettings(s.Access); err != nil {
		return err
	}

	for _, cls := range s.BodyClasses {
		if !bodyClassRe.MatchString(cls) {
			return fmt.Errorf("无效的 body 自定义 class: %q", cls)
		}
	}
	return nil
}

func safeCSS(v string) bool {
	return core.IsSafeCSSValue(v)
}

func validateAccessSettings(a *AccessGuardSettings) error {
	if a == nil {
		return nil
	}
	switch a.Type {
	case "", AccessPublic, AccessMembers:
		return nil
	case AccessPassword:
		hash := strings.TrimSpace(a.PasswordHash)
		if hash == "" {
			return errors.New("密码保护必须先设置访问密码")
		}
		if len(hash) > 100 {
			return errors.New("访问密码哈希长度非法")
		}

		if _, err := bcrypt.Cost([]byte(hash)); err != nil {
			return errors.New("访问密码哈希非法（需为 bcrypt 哈希）")
		}
		return nil
	default:
		return fmt.Errorf("无效的访问权限类型: %q", a.Type)
	}
}

func compileSettingsCSS(s *PageSettings, b *core.CSSBuckets) {
	if s.Theme != nil {

		if v := s.Theme.Typography.Body.FontFamily; v != "" {
			b.Add(core.BreakpointDesktop, "body", []string{"font-family: " + v})
		}
	}
	var body []string
	if v := s.Base.BackgroundColor; v != "" {
		body = append(body, "background-color: "+v)
	}
	if v := s.Base.BackgroundImage; v != "" {
		body = append(body, "background-image: url("+v+")")
	}
	if s.Base.BackgroundFixed {
		body = append(body, "background-attachment: fixed")
	}

	body = append(body, "container-name: sky-theme")
	b.Add(core.BreakpointDesktop, "body", body)

	b.Add(core.BreakpointDesktop, "."+core.SROnlyClass, []string{
		"position: absolute",
		"width: 1px",
		"height: 1px",
		"margin: -1px",
		"padding: 0",
		"border: 0",
		"clip-path: inset(50%)",
		"overflow: hidden",
		"white-space: nowrap",
	})

	if s.Layout.Mode != LayoutBoxed {
		return
	}

	var sec []string
	sec = append(sec, "max-width: "+s.Layout.MaxWidth)
	sec = append(sec, "margin-left: auto")
	sec = append(sec, "margin-right: auto")
	if v := s.Layout.SafePadding.Desktop; v != "" {
		sec = append(sec, "padding-left: "+v)
		sec = append(sec, "padding-right: "+v)
	}
	b.Add(core.BreakpointDesktop, "body."+bodyClassBoxed+" ."+core.SectionClass, sec)

	if v := s.Layout.SafePadding.Tablet; v != "" {
		b.Add(core.BreakpointTablet, "body."+bodyClassBoxed+" ."+core.SectionClass, []string{
			"padding-left: " + v, "padding-right: " + v,
		})
	}
	if v := s.Layout.SafePadding.Mobile; v != "" {
		b.Add(core.BreakpointMobile, "body."+bodyClassBoxed+" ."+core.SectionClass, []string{
			"padding-left: " + v, "padding-right: " + v,
		})
	}
}
