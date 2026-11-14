package orderdto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"
)

// 本文件锁 AdminOrderCreateFormReq 的真实线上形状与装配语义：
// 扁平地址前缀 → OrderAddress、平行数组 → []OrderItemReq、复选框「键存在即为真」。

// bindAdminCreate 走与控制器一致的入口：Gin 原生 FormPost 绑定 + 显式 Normalize，
// 不经过 pkg/binding 的兼容钩子。
func bindAdminCreate(t *testing.T, vals url.Values) (*AdminOrderCreateFormReq, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/orders/create", nil)
	c.Request.PostForm = vals
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var form AdminOrderCreateFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		return &form, err
	}
	form.Normalize()
	return &form, nil
}

func TestAdminOrderCreateFormAssemblesAddresses(t *testing.T) {
	form, err := bindAdminCreate(t, url.Values{
		"shipName":     {"  李四 "},
		"shipPhone":    {"13900000000"},
		"shipCountry":  {"cn"},
		"shipProvince": {"广东省"},
		"shipCity":     {"深圳市"},
		"shipDistrict": {"南山区"},
		"shipAddress":  {"科技园 1 号"},
		"shipZip":      {"518000"},
		"billName":     {"王五"},
		"billCountry":  {"us"},
	})
	if err != nil {
		t.Fatalf("ParseForm 报错: %v", err)
	}
	if form.Shipping.Name != "李四" || form.Shipping.Phone != "13900000000" ||
		form.Shipping.Country != "CN" || form.Shipping.City != "深圳市" {
		t.Errorf("收货地址装配错误: %+v", form.Shipping)
	}
	if form.Billing.Name != "王五" || form.Billing.Country != "US" {
		t.Errorf("账单地址装配错误: %+v", form.Billing)
	}
}

func TestAdminOrderCreateFormPairsItemsInOrder(t *testing.T) {
	form, err := bindAdminCreate(t, url.Values{
		"variantId": {"v2", "v1"},
		"quantity":  {"1", "2"},
	})
	if err != nil {
		t.Fatalf("ParseForm 报错: %v", err)
	}
	if len(form.Items) != 2 {
		t.Fatalf("应收 2 行，实际 %+v（字段 %+v/%+v）", form.Items, form.VariantID, form.Quantity)
	}
	if form.Items[0].VariantID != "v2" || form.Items[0].Quantity != 1 {
		t.Errorf("第一行配对错误: %+v", form.Items[0])
	}
	if form.Items[1].VariantID != "v1" || form.Items[1].Quantity != 2 {
		t.Errorf("第二行配对错误: %+v", form.Items[1])
	}
}

func TestAdminOrderCreateFormSkipsEmptyRows(t *testing.T) {
	form, err := bindAdminCreate(t, url.Values{
		"variantId": {"", "v1"},
		"quantity":  {"5", "3"},
	})
	if err != nil {
		t.Fatalf("ParseForm 报错: %v", err)
	}
	if len(form.Items) != 1 || form.Items[0].VariantID != "v1" || form.Items[0].Quantity != 3 {
		t.Fatalf("空变体行应被跳过，实际 %+v", form.Items)
	}
}

func TestAdminOrderCreateFormCheckboxMeansKeyPresent(t *testing.T) {
	form, err := bindAdminCreate(t, url.Values{
		"provisionGuestAccount": {"1"},
		"sameBilling":           {"1"},
	})
	if err != nil {
		t.Fatalf("ParseForm 报错: %v", err)
	}
	if !form.ProvisionAccount() || !form.CopyShippingToBilling() {
		t.Fatalf("键存在应判为真: %+v", form)
	}
	empty, err := bindAdminCreate(t, url.Values{"customerEmail": {"a@b.c"}})
	if err != nil {
		t.Fatalf("ParseForm 报错: %v", err)
	}
	if empty.ProvisionAccount() || empty.CopyShippingToBilling() {
		t.Fatalf("缺键应判为假: %+v", empty)
	}
}
