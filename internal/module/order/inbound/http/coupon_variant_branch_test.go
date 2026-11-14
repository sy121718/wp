package orderhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
)

// 优惠码端点按「有没有 ids 字段」在入口分叉成两种输入形状。
//
// 契约里给它们登记了 variants。此前只用 `orderBulkFormIDs` + 手工 ShouldBindWith
// 证明「能识别出哪一支」—— 那证明不了请求真的会进入对应处理器方法。
// 这里用 spy 包住 service，从 HTTP 请求真的走一遍 `h.UpdateCoupon` /
// `h.DeleteCoupon`，断言**被调用的是哪一个 service 方法**：
// 单条走 UpdateCoupon / DeleteCoupon，带 ids 走 SetCouponsStatus / DeleteCoupons。

// couponSpy 记录被调用的 service 方法；其余方法由嵌入的真实实现在需要时提供。
// 这里只覆盖本用例关心的四个入口，其它方法保持 panic 以免误用。
type couponSpy struct {
	ordercontract.OrderService

	updateCalls []*orderdto.CouponSaveReq
	deleteCalls []uint64
	bulkStatus  []*orderdto.CouponBulkStatusReq
	bulkDeletes []*orderdto.CouponBulkDeleteReq
	updateErr   error
	deleteErr   error
	statusErr   error
	bulkDelErr  error
}

func (s *couponSpy) UpdateCoupon(_ context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error) {
	s.updateCalls = append(s.updateCalls, req)
	return &orderdto.CouponResp{ID: req.ID}, s.updateErr
}

func (s *couponSpy) DeleteCoupon(_ context.Context, id uint64) error {
	s.deleteCalls = append(s.deleteCalls, id)
	return s.deleteErr
}

func (s *couponSpy) SetCouponsStatus(_ context.Context, req *orderdto.CouponBulkStatusReq) (int64, error) {
	s.bulkStatus = append(s.bulkStatus, req)
	return int64(len(req.IDs)), s.statusErr
}

func (s *couponSpy) DeleteCoupons(_ context.Context, req *orderdto.CouponBulkDeleteReq) (int64, error) {
	s.bulkDeletes = append(s.bulkDeletes, req)
	return int64(len(req.IDs)), s.bulkDelErr
}

// couponWriteEngine 装配真实处理器链：成功/失败出口与生产同形（HTML 片段）。
//
// 成功与失败都必须接上：只接失败出口时，成功分支会因为「成功出口未装配」
// 退化成 500，测试会把接线缺失误报成业务失败。
func couponWriteEngine(spy *couponSpy) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewHandle(spy)
	r := gin.New()
	r.Use(pageview.FailureMiddleware(func(c *gin.Context, status int, _ string, _ error) {
		c.Status(status)
	}))
	r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	// 整行保存成功会渲染带导航链接的片段，需要 HTML 渲染器。
	r.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	r.POST("/api/order/coupon/update", pageview.HTMLFragment, h.UpdateCoupon)
	r.POST("/api/order/coupon/delete", pageview.HTMLFragment, h.DeleteCoupon)
	return r
}

func postCouponForm(r *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(rec, req)
	return rec
}

func TestCouponDeleteSplitsOnIDsField(t *testing.T) {
	t.Run("带 ids 真的进入批量删除", func(t *testing.T) {
		spy := &couponSpy{}
		rec := postCouponForm(couponWriteEngine(spy), "/api/order/coupon/delete", url.Values{
			"ids": {"1", "2"}, "projectId": {"p-1"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("批量删除应成功，状态码 %d：%s", rec.Code, rec.Body.String())
		}
		if len(spy.bulkDeletes) != 1 {
			t.Fatalf("应调用一次 DeleteCoupons，实际 %d 次", len(spy.bulkDeletes))
		}
		if got := len(spy.bulkDeletes[0].IDs); got != 2 {
			t.Fatalf("批量删除应带 2 个 id，实际 %d", got)
		}
		if len(spy.deleteCalls) != 0 {
			t.Fatalf("批量分支不该调用单条 DeleteCoupon，实际 %v", spy.deleteCalls)
		}
	})

	t.Run("不带 ids 真的进入单条删除", func(t *testing.T) {
		spy := &couponSpy{}
		rec := postCouponForm(couponWriteEngine(spy), "/api/order/coupon/delete", url.Values{
			"id": {"7"}, "projectId": {"p-1"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("单条删除应成功，状态码 %d：%s", rec.Code, rec.Body.String())
		}
		if len(spy.deleteCalls) != 1 || spy.deleteCalls[0] != 7 {
			t.Fatalf("应调用一次 DeleteCoupon(7)，实际 %v", spy.deleteCalls)
		}
		if len(spy.bulkDeletes) != 0 {
			t.Fatalf("单条分支不该调用 DeleteCoupons，实际 %v", spy.bulkDeletes)
		}
	})
}

func TestCouponUpdateSplitsOnIDsField(t *testing.T) {
	t.Run("带 ids 真的进入批量启停", func(t *testing.T) {
		spy := &couponSpy{}
		rec := postCouponForm(couponWriteEngine(spy), "/api/order/coupon/update", url.Values{
			"ids": {"1"}, "status": {"1"}, "projectId": {"p-1"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("批量启停应成功，状态码 %d：%s", rec.Code, rec.Body.String())
		}
		if len(spy.bulkStatus) != 1 {
			t.Fatalf("应调用一次 SetCouponsStatus，实际 %d 次", len(spy.bulkStatus))
		}
		if got := spy.bulkStatus[0].Status; got != 1 {
			t.Fatalf("批量启停目标状态应为 1，实际 %d", got)
		}
		if len(spy.updateCalls) != 0 {
			t.Fatalf("批量分支不该调用整行 UpdateCoupon，实际 %v", spy.updateCalls)
		}
	})

	t.Run("不带 ids 真的进入整行保存", func(t *testing.T) {
		spy := &couponSpy{}
		rec := postCouponForm(couponWriteEngine(spy), "/api/order/coupon/update", url.Values{
			"id": {"7"}, "code": {"SAVE"}, "name": {"满减"}, "discountType": {"percent"},
			"discountValue": {"10"}, "projectId": {"p-1"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("整行保存应成功，状态码 %d：%s", rec.Code, rec.Body.String())
		}
		if len(spy.updateCalls) != 1 || spy.updateCalls[0].ID != 7 {
			t.Fatalf("应调用一次 UpdateCoupon(id=7)，实际 %+v", spy.updateCalls)
		}
		if len(spy.bulkStatus) != 0 {
			t.Fatalf("整行分支不该调用 SetCouponsStatus，实际 %v", spy.bulkStatus)
		}
	})
}

// 带 ids 但 status 非法：批量分支自己拒绝，不能落回整行保存。
func TestCouponUpdateBulkWithBadStatusRejects(t *testing.T) {
	spy := &couponSpy{}
	rec := postCouponForm(couponWriteEngine(spy), "/api/order/coupon/update", url.Values{
		"ids": {"1"}, "status": {"yes"}, "projectId": {"p-1"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法目标状态应 400，实际 %d", rec.Code)
	}
	if len(spy.bulkStatus) != 0 || len(spy.updateCalls) != 0 {
		t.Fatalf("非法输入不该触达任何 service 方法：bulk=%v update=%v", spy.bulkStatus, spy.updateCalls)
	}
}
