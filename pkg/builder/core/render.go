package core

import (
	"context"
	"errors"
	"strings"

	src "go_wp/pkg/builder/source"
)

type RenderContext struct {
	actionResolver ActionResolver
	CSS            *CSSBuckets

	IncompleteNodes map[string]bool

	AllowIncomplete bool

	renderPanel func(blockID string) (string, error)

	refFailure func(RefFailure) error

	Context context.Context

	Content ContentResolver

	Block BlockResolver

	BlockStack []string

	Plugin PluginResolver

	Collection CollectionResolver

	Product ProductDataSource

	ContentSource ContentDataSource

	Navigation NavigationResolver

	ProjectID string

	usage UsageRecorder

	archiveEntityType string
	archiveEntityID   string

	siteLinkResolver func(logicalPath string) string

	CurrentPath string

	RevealInherit string

	RevealDefaultEntrance string

	ImageDefaults ImageDefaults

	AssetProbe func(url string) []AssetVariant

	Lang string

	Translate func(key, fallback string) string

	ContentTranslate func(sourceText, contentContext string) string

	Locales []LocaleLink

	Checkout CheckoutInput

	Features *FeatureSet

	CanvasSlotFrames bool
}

type LocaleLink struct {
	Lang string

	Href string

	Current bool
}

func (c *RenderContext) Text(key, fallback string) string {
	if c != nil && c.Translate != nil {
		if text := c.Translate(key, fallback); text != "" {
			return text
		}
	}
	if fallback != "" {
		return fallback
	}
	return key
}

type ImageDefaults struct {
	LazyLoad bool

	Skeleton bool
}

type ContentResolver = src.ContentResolver

type BlockResolver interface {
	ResolveBlockRoot(blockID string) ([]*Node, error)
}

const (
	RefReasonResolverMissing = "ref_resolver_missing"

	RefReasonUnavailable = "ref_unavailable"

	RefReasonEmpty = "ref_empty"

	RefReasonTemplateUnavailable = "ref_template_unavailable"

	RefReasonTemplateEmpty = "ref_template_empty"
)

type RefDegradeError struct {
	Reason string

	Err error
}

func (e *RefDegradeError) Error() string {
	switch {
	case e == nil:
		return ""
	case e.Err != nil:
		return e.Err.Error()
	default:
		return e.Reason
	}
}

func (e *RefDegradeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func RefDegradeReason(err error, fallback string) string {
	var de *RefDegradeError
	if errors.As(err, &de) && de.Reason != "" {
		return de.Reason
	}
	return fallback
}

type RefFailure struct {
	NodeID, NodeType string

	BlockID string

	Slot string

	Reason string
}

type NavigationItem struct {
	Label string

	URL string

	Target string

	Children []NavigationItem

	PanelBlockID string

	PanelWidth string
}

type NavigationResolver interface {
	ResolveMenu(projectID, kind string) ([]NavigationItem, error)

	ResolveNavigation(projectID, navigationID string) ([]NavigationItem, error)
}

type UsageRecorder interface {
	UseMenu(kind string)

	UseNavigation(navigationID string)

	UseBlock(blockID string)
}

func (c *RenderContext) SetPanelRenderer(fn func(blockID string) (string, error)) {
	if c == nil {
		return
	}
	c.renderPanel = fn
}

func (c *RenderContext) RenderPanel(blockID string) (string, error) {
	if c == nil || c.renderPanel == nil {
		return "", nil
	}
	return c.renderPanel(blockID)
}

func (c *RenderContext) SetRefFailureHandler(fn func(RefFailure) error) {
	if c == nil {
		return
	}
	c.refFailure = fn
}

func (c *RenderContext) RefFailed(f RefFailure) error {
	if c == nil || c.refFailure == nil {
		return nil
	}
	return c.refFailure(f)
}

func (c *RenderContext) SetArchiveEntity(entityType, entityID string) {
	if c == nil {
		return
	}
	c.archiveEntityType = strings.TrimSpace(entityType)
	c.archiveEntityID = strings.TrimSpace(entityID)
}

func (c *RenderContext) ArchiveEntity() (entityType, entityID string) {
	if c == nil {
		return "", ""
	}
	return c.archiveEntityType, c.archiveEntityID
}

func (c *RenderContext) SetSiteLinkResolver(fn func(logicalPath string) string) {
	if c == nil {
		return
	}
	c.siteLinkResolver = fn
}

func (c *RenderContext) ResolveSiteLink(path string) string {
	if c == nil || c.siteLinkResolver == nil {
		return path
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return path
	}

	if i := strings.Index(trimmed, ":"); i > 0 && !strings.Contains(trimmed[:i], "/") {
		return path
	}
	if !strings.HasPrefix(trimmed, "/") {
		return path
	}
	return c.siteLinkResolver(trimmed)
}

func SiteLinkOrSame(siteLink func(string) string, href string) string {
	if siteLink == nil {
		return href
	}
	return siteLink(href)
}

func (c *RenderContext) SetUsageRecorder(r UsageRecorder) {
	if c == nil {
		return
	}
	c.usage = r
}

func (c *RenderContext) UseMenu(kind string) {
	if c == nil || c.usage == nil {
		return
	}
	if k := strings.TrimSpace(kind); k != "" {
		c.usage.UseMenu(k)
	}
}

func (c *RenderContext) UseNavigation(navigationID string) {
	if c == nil || c.usage == nil {
		return
	}
	if id := strings.TrimSpace(navigationID); id != "" {
		c.usage.UseNavigation(id)
	}
}

func (c *RenderContext) UseBlock(blockID string) {
	if c == nil || c.usage == nil {
		return
	}
	if id := strings.TrimSpace(blockID); id != "" {
		c.usage.UseBlock(id)
	}
}

const (
	SiteSlotShop     = "shop"
	SiteSlotBlog     = "blog"
	SiteSlotCart     = "cart"
	SiteSlotCheckout = "checkout"
	SiteSlotAccount  = "account"
	SiteSlotLogin    = "login"
	SiteSlotRegister = "register"
	SiteSlotForgot   = "forgot"
	SiteSlotReset    = "reset"
	SiteSlotOrders   = "orders"
)

type CheckoutCountry struct {
	Code string

	Label string
}

type CheckoutInput struct {
	Countries []CheckoutCountry

	DefaultCountry string
}

// ProductDataSource 是商品数据的窄端口：只要求集合解析与字段目录，
// 业务模块在自己的 outbound/装配里适配到它。
type ProductDataSource interface {
	src.CollectionResolver
	src.CollectionSchemaProvider
	src.CollectionFilterOptionsProvider
}

// ContentDataSource 是内容数据的窄端口。
type ContentDataSource interface {
	src.CollectionResolver
	src.CollectionSchemaProvider
}

// AssetVariant 是资产探测的中立结果：展示只需要 URL 与宽度。
type AssetVariant struct {
	URL   string
	Width int
}
