package orderhttp

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/backpath"
	"go_wp/pkg/binding"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const (
	orderCreatePageTitle = "代客建单"

	orderCreateDefaultRows = 2

	orderCreateMaxRows = 20
)

type orderCreateItemRow struct {
	VariantID string
	Quantity  string
}

type orderCreateForm struct {
	ProjectID        string
	CandidateKeyword string
	Email            string
	CustomerName     string
	CustomerPhone    string
	PayMethod        string
	PayTitle         string

	Currency      string
	ShippingTotal string
	CouponCode    string
	Remark        string
	AdminNote     string
	RequestID     string

	ProvisionAccount bool

	SameBilling bool

	ItemsTooMany bool
	Ship         orderdto.OrderAddress
	Bill         orderdto.OrderAddress
	Items        []orderCreateItemRow
}

type orderCreatePageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService

	catalog productcontract.ProductService

	availability productcontract.VariantAvailabilityLookupPort

	dict sysconfigcontract.DictReader
}

func NewOrderCreatePageHandle(orders ordercontract.OrderService,
	projects projectcontract.ProjectService,
	catalog productcontract.ProductService,
	dict sysconfigcontract.DictReader) *orderCreatePageHandle {
	h := &orderCreatePageHandle{orders: orders, projects: projects, catalog: catalog, dict: dict}
	if catalog != nil {
		if lookup, ok := catalog.(productcontract.VariantAvailabilityLookupPort); ok {
			h.availability = lookup
		}
	}
	return h
}

func (h *orderCreatePageHandle) OrderCreatePage(c *gin.Context) {
	form := orderCreateForm{
		ProjectID:        strings.TrimSpace(c.Query("project")),
		CandidateKeyword: strings.TrimSpace(c.Query("keyword")),

		RequestID: utils.NewTimeOrderedID(),
	}
	form.Items = make([]orderCreateItemRow, orderCreateRowCount(c.Query("rows")))
	h.render(c, form, nil, "")
}

// orderCreatedBackURL 按实体事实生成建单后的回列表链接（保留工程与订单上下文）。
func orderCreatedBackURL(projectID string, res *orderdto.CreateOrderResp) string {
	params := map[string]string{"project": projectID}
	if res != nil && res.ID != 0 {
		params["orderId"] = strconv.FormatUint(res.ID, 10)
	}
	return backpath.WithParams("/admin/orders", params)
}

func (h *orderCreatePageHandle) OrderCreateSubmit(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// 代客建单是普通表单写：显式表单入口只读 body，失败交本域 HTML 出口；
	// 扁平地址前缀与明细行的装配由 DTO 的 Normalize 显式完成。
	form, err := binding.ParseForm[orderdto.AdminOrderCreateFormReq](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	form.Normalize()
	req, fieldErrs := orderCreateReqFromForm(c, form)
	if len(fieldErrs) > 0 {
		orderWriteFail(c, createFormFieldError(fieldErrs))
		return
	}

	req.IPAddress = c.ClientIP()
	req.UserAgent = c.Request.UserAgent()
	req.CreateBy = builtin.CurrentUserID(c)

	res, err := h.orders.CreateAdminOrder(c.Request.Context(), req)
	if err != nil {
		orderWriteFail(c, err)
		return
	}

	// 成功：结论片段带上按实体事实生成的下一步链接，前端按声明导航。
	orderWriteResultNav(c, "ordCreated", orderCreatedBackURL(req.ProjectID, res))
}

// createFormFieldError 把字段级校验错误收成一条业务失败事实：
// 词条取自字段级 key，模板逐条取词，不在 Go 里拼成品句。
func createFormFieldError(fieldErrs map[string]string) error {
	keys := make([]string, 0, len(fieldErrs))
	for name := range fieldErrs {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	items := make([]facing.BlockedItem, 0, len(keys))
	for _, name := range keys {
		items = append(items, facing.BlockedItem{ID: name, ReasonKey: fieldErrs[name]})
	}
	return &orderBulkRejectedError{items: items}
}

func (h *orderCreatePageHandle) render(c *gin.Context, form orderCreateForm, fieldErrs map[string]string, pageErr string) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)
	loadFailed := loadErr != nil
	if loadFailed {

		projects = nil
		pageErr = firstNonEmpty(pageErr, orderFacingError(c, loadErr))
	}

	selected := strings.TrimSpace(form.ProjectID)
	if selected == "" && !loadFailed && len(projects) > 0 {
		selected = projects[0].ID
	}

	noProject := !loadFailed && len(projects) == 0
	formReady := !loadFailed && !noProject

	var candidates []SkuOptionResp
	candidateErr := ""
	if h.catalog != nil && selected != "" {
		skus, cerr := h.catalog.ListVariantOptions(ctx, &productdto.ListVariantOptionReq{
			ProjectID: selected,
			Keyword:   form.CandidateKeyword,
		})
		if cerr != nil {
			candidateErr = orderFacingError(c, cerr)
		} else {
			candidates = h.candidateViews(c, selected, skus)
		}
	}

	fieldText := make(map[string]string, len(fieldErrs))
	for name, msg := range fieldErrs {
		fieldText[name] = msg
	}

	if len(form.Items) == 0 {

		form.Items = []orderCreateItemRow{{}}
	}

	rowAdd := url.Values{}
	rowAdd.Set("project", selected)
	if form.CandidateKeyword != "" {
		rowAdd.Set("keyword", form.CandidateKeyword)
	}
	rowAdd.Set("rows", strconv.Itoa(len(form.Items)+1))

	tr := orderTr(c)
	countries := h.countryList(c)
	shipCountries := countryOptionViews(tr, countries, form.Ship.Country)
	billCountries := countryOptionViews(tr, countries, form.Bill.Country)

	view := &OrderNewPageResp{
		Title: orderCreatePageTitle,
		Menu:  "orders",

		Projects:        projects,
		SelectedProject: selected,
		LoadFailed:      loadFailed,
		NoProject:       noProject,
		FormReady:       formReady,

		CandidateKeyword: form.CandidateKeyword,
		Candidates:       candidates,
		CandidateCount:   len(candidates),
		CandidateError:   candidateErr,
		ItemRows:         toOrderNewItemRows(form.Items),
		MaxRows:          orderCreateMaxRows,

		ShipCountries: toCountryOptionResps(shipCountries),
		BillCountries: toCountryOptionResps(billCountries),

		CurrencyOptions: toCountryOptionResps(
			currencyOptionViews(tr, h.currencyList(c), form.Currency)),
		RowAddURL: "/admin/orders/new?" + rowAdd.Encode(),

		Err: pageErr,

		FieldErrors:   fieldText,
		HasFieldError: len(fieldErrs) > 0,

		Form: form,
		Echo: couponEcho(c),
	}
	pageview.RenderTemplate(c, OrderNewPageTemplate, view)
}

const countryNoneLabelKey = orderenums.OrderNewFieldCountryNone

type countryOptionView struct {
	Code     string
	Label    string
	Selected bool
}

func (h *orderCreatePageHandle) countryList(c *gin.Context) []sysconfigcontract.CountryOption {
	if h.dict == nil {
		return nil
	}
	opts, err := h.dict.ListCountryOptions(c.Request.Context(), response.RequestLanguage(c))
	if err != nil {
		return nil
	}
	return opts
}

func (h *orderCreatePageHandle) currencyList(c *gin.Context) []sysconfigcontract.DictOption {
	if h.dict == nil {
		return nil
	}
	opts, err := h.dict.ListDictOptions(c.Request.Context(), currencyDictType)
	if err != nil {
		return nil
	}
	return opts
}

const currencyDictType = "currency"

func currencyOptionViews(tr translate, currencies []sysconfigcontract.DictOption, submitted string) []countryOptionView {
	def := strings.ToUpper(strings.TrimSpace(i18n.GetDefaultCurrency()))
	selected := strings.ToUpper(strings.TrimSpace(submitted))
	if selected == "" {
		selected = def
	}
	out := make([]countryOptionView, 0, len(currencies)+2)
	out = append(out, countryOptionView{
		Label:    tr(orderenums.OrderNewFieldCurrencyUse, "用站点默认币") + "（" + def + "）",
		Selected: selected == "" || selected == def,
	})
	hit := false
	for _, o := range currencies {
		code := strings.ToUpper(strings.TrimSpace(o.Code))
		if code == "" {
			continue
		}
		label := strings.TrimSpace(o.Label)
		if label == "" {
			label = code
		}
		if code == selected {
			hit = true
		}
		out = append(out, countryOptionView{Code: code, Label: label, Selected: code == selected})
	}
	if !hit && selected != def {

		out = append(out, countryOptionView{Code: selected, Label: selected, Selected: true})
	}
	return out
}

func countryOptionViews(tr translate, countries []sysconfigcontract.CountryOption, submitted string) []countryOptionView {
	selected := orderdto.NormalizeCountryCode(submitted)
	if selected == "" {
		selected = orderdto.NormalizeCountryCode(i18n.GetDefaultCountry())
	}
	out := make([]countryOptionView, 0, len(countries)+2)
	out = append(out, countryOptionView{Label: tr(countryNoneLabelKey, "（不填写）"), Selected: selected == ""})
	hit := false
	for _, o := range countries {
		code := strings.TrimSpace(o.Code)
		if code == "" {
			continue
		}
		label := strings.TrimSpace(o.Label)
		if label == "" {
			label = code
		}
		matched := selected != "" && strings.EqualFold(code, selected)
		hit = hit || matched
		out = append(out, countryOptionView{Code: code, Label: label, Selected: matched})
	}
	if selected != "" && !hit {
		out = append(out, countryOptionView{Code: selected, Label: selected, Selected: true})
	}
	return out
}

func (h *orderCreatePageHandle) candidateViews(c *gin.Context, projectID string,
	skus []*productdto.VariantOptionResp) []SkuOptionResp {
	tr := orderTr(c)
	avail := map[string]int{}
	if h.availability != nil && len(skus) > 0 {
		ids := make([]string, 0, len(skus))
		for _, sku := range skus {
			if sku != nil && sku.VariantID != "" {
				ids = append(ids, sku.VariantID)
			}
		}
		if len(ids) > 0 {

			if quantities, err := h.availability.VariantAvailabilities(c.Request.Context(), projectID, ids); err == nil {
				avail = quantities
			}
		}
	}

	out := make([]SkuOptionResp, 0, len(skus))
	for _, sku := range skus {
		if sku == nil || sku.VariantID == "" {
			continue
		}

		label := fmt.Sprintf("%s · %s · ¥%.2f", sku.ProductName, sku.SKUCode, sku.Price)
		if n, ok := avail[sku.VariantID]; ok {
			label += "（" + fmt.Sprintf(tr(orderenums.OrderNewOptionAvailable, "可用 %s"), strconv.Itoa(n)) + "）"
		}
		out = append(out, SkuOptionResp{ID: sku.VariantID, Label: label})
	}
	return out
}

func orderCreateRowCount(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return orderCreateDefaultRows
	}
	if n > orderCreateMaxRows {
		return orderCreateMaxRows
	}
	return n
}

// orderCreateReqFromForm 把已绑定的表单投影成 service 请求，并收集字段级错误。
// 字段级错误的词条按请求语言取词，所以留在控制器；service 侧的业务校验不重复。
func orderCreateReqFromForm(c *gin.Context, form *orderdto.AdminOrderCreateFormReq) (*orderdto.CreateOrderReq, map[string]string) {
	fieldErrs := map[string]string{}
	tr := orderTr(c)

	if form.CustomerEmail == "" {
		fieldErrs["customerEmail"] = tr(orderenums.ErrCustomerEmailRequired, orderenums.ErrCustomerEmailRequired)
	} else if !strings.Contains(form.CustomerEmail, "@") || len(form.CustomerEmail) < 3 {
		fieldErrs["customerEmail"] = tr(orderenums.ErrCustomerEmailInvalid, orderenums.ErrCustomerEmailInvalid)
	}

	if form.ItemsTooMany {
		fieldErrs["items"] = tr(orderenums.ErrItemLimitExceeded, orderenums.ErrItemLimitExceeded)
	} else if len(form.Items) == 0 {
		fieldErrs["items"] = tr(orderenums.ErrItemsRequired, orderenums.ErrItemsRequired)
	}

	shippingTotal, _ := strconv.ParseInt(form.ShippingTotal, 10, 64)

	billing := form.Billing
	if form.CopyShippingToBilling() {
		billing = form.Shipping
	}

	provision := form.ProvisionAccount()

	requestID := form.RequestID
	if requestID == "" {
		requestID = utils.NewTimeOrderedID()
	}

	return &orderdto.CreateOrderReq{
		ProjectID:             form.ProjectID,
		CustomerEmail:         form.CustomerEmail,
		CustomerName:          form.CustomerName,
		CustomerPhone:         form.CustomerPhone,
		Items:                 form.Items,
		Shipping:              form.Shipping,
		Billing:               billing,
		PaymentMethod:         form.PaymentMethod,
		PaymentMethodTitle:    form.PaymentMethodTitle,
		Currency:              form.Currency,
		ShippingTotal:         shippingTotal,
		CouponCode:            form.CouponCode,
		Remark:                form.Remark,
		AdminNote:             form.AdminNote,
		RequestID:             requestID,
		ProvisionGuestAccount: &provision,
	}, fieldErrs
}

const (
	orderOperatorTypeAdmin = "admin"

	orderFieldEmpty = "—"
)

func toOrderNewItemRows(in []orderCreateItemRow) []OrderNewItemRowResp {
	out := make([]OrderNewItemRowResp, 0, len(in))
	for _, r := range in {
		out = append(out, OrderNewItemRowResp{VariantID: r.VariantID, Quantity: r.Quantity})
	}
	return out
}

func toCountryOptionResps(in []countryOptionView) []CountryOptionResp {
	out := make([]CountryOptionResp, 0, len(in))
	for _, c := range in {
		out = append(out, CountryOptionResp{Code: c.Code, Label: c.Label, Selected: c.Selected})
	}
	return out
}
