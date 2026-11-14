package projectdto

// SiteSettingsForm 是「站点设置」页主表单的显式字段表。
//
// 字段名与模板控件逐字一致；URL 规则是按实体类型动态展开的 urlPattern_<kind>，
// 因此完整校验（路径规则、GA4、脚本白名单、金额）仍在控制器侧做，
// 这里只负责把表单绑定成具名结构，让写端点契约有权威的字段来源。
type SiteSettingsForm struct {
	ProjectID string `form:"projectId"`
	Name      string `form:"name"`

	SiteName     string `form:"siteName"`
	SiteDesc     string `form:"siteDesc"`
	ContactEmail string `form:"contactEmail"`

	GA4MeasurementID          string `form:"ga4MeasurementId"`
	SearchConsoleVerification string `form:"searchConsoleVerification"`
	IndexNowKey               string `form:"indexNowKey"`
	NotFoundHTML              string `form:"notFoundHtml"`

	HeadScripts string `form:"headScripts"`
	BodyScripts string `form:"bodyScripts"`

	ShippingBaseFee       string `form:"shippingBaseFee"`
	ShippingFreeThreshold string `form:"shippingFreeThreshold"`

	// URL 规则按实体类型的**固定种类**展开。种类清单是编译期常量（siteurl.KnownKinds），
	// 因此这里显式列出每个 urlPattern_<kind>，不按运行时拼接的字段名提交：
	// 静态字段表让契约生成器与字段门禁能真正对账，新增种类时同步加一行。
	URLPatternArticle  string `form:"urlPattern_article"`
	URLPatternProduct  string `form:"urlPattern_product"`
	URLPatternCategory string `form:"urlPattern_product_category"`
	URLPatternBrand    string `form:"urlPattern_product_brand"`
	URLPatternTag      string `form:"urlPattern_product_tag"`
}

// URLPatterns 把静态字段还原成「种类 → 路径模式」，键名与 siteurl.KnownKinds 一致。
func (f *SiteSettingsForm) URLPatterns() map[string]string {
	return map[string]string{
		"article":          f.URLPatternArticle,
		"product":          f.URLPatternProduct,
		"product_category": f.URLPatternCategory,
		"product_brand":    f.URLPatternBrand,
		"product_tag":      f.URLPatternTag,
	}
}

// LocalesForm 是「语言清单」表单：行按提交顺序用下标表示，
// 勾选与默认标记都是按下标的集合，因此必须在绑定前读原始表单。
type LocalesForm struct {
	ProjectID   string `form:"projectId"`
	LangURLMode string `form:"langURLMode"`

	Langs        []string `form:"langs"`
	EnabledIndex []string `form:"enabledIndex"`
	DefaultIndex string   `form:"defaultIndex"`

	// 行片段端点（增 / 删一行）额外提交的字段。
	Action      string `form:"action"`
	NewLang     string `form:"newLang"`
	RemoveIndex string `form:"removeIndex"`
}

// LocaleRowsForm 是语言行片段的表单：同一张 locale-form 加上行操作的三个字段。
type LocaleRowsForm = LocalesForm
