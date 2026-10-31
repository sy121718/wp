package orderhttp

import (
	"fmt"
	"go_wp/internal/middleware/builtin"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/backpath"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const (
	orderCreatePageTitle = "代客建单"

	orderCreateDefaultRows = 2

	orderCreateMaxRows = 20

	orderCreateParseLimit = 500
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

func (h *orderCreatePageHandle) OrderCreateSubmit(c *gin.Context) {
	form := orderCreateFormFromPost(c)
	req, fieldErrs := orderCreateReqFromForm(c, form)
	if len(fieldErrs) > 0 {
		h.render(c, form, fieldErrs, "")
		return
	}

	req.IPAddress = c.ClientIP()
	req.UserAgent = c.Request.UserAgent()
	req.CreateBy = builtin.CurrentUserID(c)

	res, err := h.orders.CreateAdminOrder(c.Request.Context(), req)
	if err != nil {

		h.render(c, form, nil, orderFacingError(c, err))
		return
	}

	params := map[string]string{"project": req.ProjectID}
	if res != nil && res.ID != 0 {

		params["orderId"] = strconv.FormatUint(res.ID, 10)
	}

	pageview.AfterWrite(c, backpath.WithParams("/admin/orders", params))
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

func orderCreateFormFromPost(c *gin.Context) orderCreateForm {
	form := orderCreateForm{
		ProjectID:        strings.TrimSpace(c.PostForm("project")),
		CandidateKeyword: strings.TrimSpace(c.PostForm("candidateKeyword")),
		Email:            strings.TrimSpace(c.PostForm("customerEmail")),
		CustomerName:     strings.TrimSpace(c.PostForm("customerName")),
		CustomerPhone:    strings.TrimSpace(c.PostForm("customerPhone")),
		PayMethod:        strings.TrimSpace(c.PostForm("paymentMethod")),
		PayTitle:         strings.TrimSpace(c.PostForm("paymentMethodTitle")),
		Currency:         strings.ToUpper(strings.TrimSpace(c.PostForm("currency"))),
		ShippingTotal:    strings.TrimSpace(c.PostForm("shippingTotal")),
		CouponCode:       strings.TrimSpace(c.PostForm("couponCode")),
		Remark:           strings.TrimSpace(c.PostForm("remark")),
		AdminNote:        strings.TrimSpace(c.PostForm("adminNote")),
		RequestID:        strings.TrimSpace(c.PostForm("requestId")),

		ProvisionAccount: c.PostForm("provisionGuestAccount") != "",
		SameBilling:      c.PostForm("sameBilling") != "",
		Ship: orderdto.OrderAddress{
			Name:     strings.TrimSpace(c.PostForm("shipName")),
			Phone:    strings.TrimSpace(c.PostForm("shipPhone")),
			Country:  orderdto.NormalizeCountryCode(c.PostForm("shipCountry")),
			Province: strings.TrimSpace(c.PostForm("shipProvince")),
			City:     strings.TrimSpace(c.PostForm("shipCity")),
			District: strings.TrimSpace(c.PostForm("shipDistrict")),
			Address:  strings.TrimSpace(c.PostForm("shipAddress")),
			Zip:      strings.TrimSpace(c.PostForm("shipZip")),
		},
		Bill: orderdto.OrderAddress{
			Name:     strings.TrimSpace(c.PostForm("billName")),
			Phone:    strings.TrimSpace(c.PostForm("billPhone")),
			Country:  orderdto.NormalizeCountryCode(c.PostForm("billCountry")),
			Province: strings.TrimSpace(c.PostForm("billProvince")),
			City:     strings.TrimSpace(c.PostForm("billCity")),
			District: strings.TrimSpace(c.PostForm("billDistrict")),
			Address:  strings.TrimSpace(c.PostForm("billAddress")),
			Zip:      strings.TrimSpace(c.PostForm("billZip")),
		},
	}
	form.Items, form.ItemsTooMany = orderCreateItemRowsFromPost(c)
	if form.RequestID == "" {
		form.RequestID = utils.NewTimeOrderedID()
	}
	return form
}

func orderCreateItemRowsFromPost(c *gin.Context) (rows []orderCreateItemRow, tooMany bool) {
	ids := c.PostFormArray("variantId")
	quantities := c.PostFormArray("quantity")
	if len(ids) > orderCreateParseLimit {
		return []orderCreateItemRow{{}}, true
	}
	rows = make([]orderCreateItemRow, 0, len(ids))
	for i, raw := range ids {
		qty := ""
		if i < len(quantities) {
			qty = strings.TrimSpace(quantities[i])
		}
		rows = append(rows, orderCreateItemRow{VariantID: strings.TrimSpace(raw), Quantity: qty})
	}
	if len(rows) == 0 {
		rows = append(rows, orderCreateItemRow{})
	}
	return rows, false
}

func orderCreateReqFromForm(c *gin.Context, form orderCreateForm) (*orderdto.CreateOrderReq, map[string]string) {
	fieldErrs := map[string]string{}
	tr := orderTr(c)

	if form.Email == "" {
		fieldErrs["customerEmail"] = tr(orderenums.ErrCustomerEmailRequired, orderenums.ErrCustomerEmailRequired)
	} else if !strings.Contains(form.Email, "@") || len(form.Email) < 3 {
		fieldErrs["customerEmail"] = tr(orderenums.ErrCustomerEmailInvalid, orderenums.ErrCustomerEmailInvalid)
	}

	items := make([]orderdto.OrderItemReq, 0, len(form.Items))
	for _, row := range form.Items {
		if row.VariantID == "" {

			continue
		}
		qty, _ := strconv.Atoi(row.Quantity)
		items = append(items, orderdto.OrderItemReq{VariantID: row.VariantID, Quantity: qty})
	}
	if form.ItemsTooMany {

		fieldErrs["items"] = tr(orderenums.ErrItemLimitExceeded, orderenums.ErrItemLimitExceeded)
	} else if len(items) == 0 {
		fieldErrs["items"] = tr(orderenums.ErrItemsRequired, orderenums.ErrItemsRequired)
	}

	shippingTotal, _ := strconv.ParseInt(strings.TrimSpace(form.ShippingTotal), 10, 64)

	billing := form.Bill
	if form.SameBilling {
		billing = form.Ship
	}

	provision := form.ProvisionAccount

	return &orderdto.CreateOrderReq{
		ProjectID:             form.ProjectID,
		CustomerEmail:         form.Email,
		CustomerName:          form.CustomerName,
		CustomerPhone:         form.CustomerPhone,
		Items:                 items,
		Shipping:              form.Ship,
		Billing:               billing,
		PaymentMethod:         form.PayMethod,
		PaymentMethodTitle:    form.PayTitle,
		Currency:              form.Currency,
		ShippingTotal:         shippingTotal,
		CouponCode:            form.CouponCode,
		Remark:                form.Remark,
		AdminNote:             form.AdminNote,
		RequestID:             form.RequestID,
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
