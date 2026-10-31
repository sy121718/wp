package ordercontract

type CheckoutFieldType string

const (
	CheckoutFieldText CheckoutFieldType = "text"

	CheckoutFieldEmail CheckoutFieldType = "email"

	CheckoutFieldTel CheckoutFieldType = "tel"

	CheckoutFieldTextarea CheckoutFieldType = "textarea"

	CheckoutFieldCountry CheckoutFieldType = "country"
)

const (
	CheckoutGroupContact = "contact"

	CheckoutGroupShipping = "shipping"

	CheckoutGroupBilling = "billing"

	CheckoutGroupSystem = "system"
)

type CheckoutField struct {
	Key string

	Label string

	Type CheckoutFieldType

	Group string

	Required bool

	Removable bool

	DefaultOn bool
}

func (f CheckoutField) InForm() bool { return f.Group != CheckoutGroupSystem }

var checkoutFields = []CheckoutField{

	{Key: "email", Label: "邮箱", Type: CheckoutFieldEmail, Group: CheckoutGroupContact, Required: true, DefaultOn: true},
	{Key: "name", Label: "姓名", Type: CheckoutFieldText, Group: CheckoutGroupContact, Required: true, DefaultOn: true},
	{Key: "phone", Label: "电话", Type: CheckoutFieldTel, Group: CheckoutGroupContact, Required: true, DefaultOn: true},

	{Key: "shipName", Label: "收货人", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true},
	{Key: "shipPhone", Label: "收货电话", Type: CheckoutFieldTel, Group: CheckoutGroupShipping, Removable: true},

	{Key: "country", Label: "国家/地区", Type: CheckoutFieldCountry, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	{Key: "province", Label: "省/州", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	{Key: "city", Label: "城市", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	{Key: "district", Label: "区/县", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},

	{Key: "address", Label: "详细地址", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Required: true, DefaultOn: true},
	{Key: "zip", Label: "邮编", Type: CheckoutFieldText, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},
	{Key: "remark", Label: "订单备注", Type: CheckoutFieldTextarea, Group: CheckoutGroupShipping, Removable: true, DefaultOn: true},

	{Key: "billName", Label: "账单姓名", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billPhone", Label: "账单电话", Type: CheckoutFieldTel, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billCountry", Label: "账单国家/地区", Type: CheckoutFieldCountry, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billProvince", Label: "账单省/州", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billCity", Label: "账单城市", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billDistrict", Label: "账单区/县", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billAddress", Label: "账单详细地址", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},
	{Key: "billZip", Label: "账单邮编", Type: CheckoutFieldText, Group: CheckoutGroupBilling, Removable: true, DefaultOn: true},

	{Key: "requestId", Label: "请求标识", Type: CheckoutFieldText, Group: CheckoutGroupSystem},

	{Key: "locale", Label: "语言", Type: CheckoutFieldText, Group: CheckoutGroupSystem},
}

func CheckoutFields() []CheckoutField {
	out := make([]CheckoutField, len(checkoutFields))
	copy(out, checkoutFields)
	return out
}

func CheckoutFormFields() []CheckoutField {
	out := make([]CheckoutField, 0, len(checkoutFields))
	for _, f := range checkoutFields {
		if f.InForm() {
			out = append(out, f)
		}
	}
	return out
}

func CheckoutFieldOf(key string) (CheckoutField, bool) {
	for _, f := range checkoutFields {
		if f.Key == key {
			return f, true
		}
	}
	return CheckoutField{}, false
}

type CheckoutFieldSource interface {
	CheckoutFields() []CheckoutField
}

type StaticCheckoutFieldSource struct{}

func (StaticCheckoutFieldSource) CheckoutFields() []CheckoutField { return CheckoutFields() }

var _ CheckoutFieldSource = StaticCheckoutFieldSource{}
