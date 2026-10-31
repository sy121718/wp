package feature

import (
	"bytes"
	"fmt"
	"go_wp/internal/adminui"
	json "go_wp/pkg/jsonx"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

type orderMutationCapture struct {
	mu     sync.Mutex
	body   string
	status int
	seen   bool
}

func (cap *orderMutationCapture) add(body string, status int) {
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if !cap.seen {
		cap.body, cap.status, cap.seen = body, status, true
		return
	}
	cap.body, cap.status = body, status
}

func (cap *orderMutationCapture) last() (string, int, bool) {
	cap.mu.Lock()
	defer cap.mu.Unlock()
	return cap.body, cap.status, cap.seen
}

func TestOrderJSONWriteChainCatchesMissingTemplateAttributes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机没有 node：反证同样要求真实基座 write-base.js，跳过而不是用桩冒充")
	}
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	_, variantID := f.addProduct(t, "反证商品", 15, 30)
	mkOrder := func(email string) uint64 {
		req := f.createBaseReq(variantID, 1)
		req.CustomerEmail = email
		res, cerr := f.orders.CreateOrder(t.Context(), req)
		if cerr != nil {
			t.Fatalf("造单失败（%s）：%v", email, cerr)
		}
		return res.ID
	}

	cases := []struct {
		name     string
		mutate   func(html string) string
		api      string
		mode     string
		fields   orderChainFields
		orderIDs []uint64

		target string

		owns func(t *testing.T, where, body string, status int, statusNow, target string)
	}{
		{
			name: "M1-drop-bulk-extra",
			mutate: func(html string) string {
				return strings.Replace(html, ` data-write-extra="toStatus,remark"`, "", 1)
			},
			api:  "/api/order/status",
			mode: "bulk",
			owns: func(t *testing.T, where, body string, status int, statusNow, target string) {
				if strings.Contains(body, `"toStatus"`) {
					t.Fatalf("%s：少了 data-write-extra 时请求体里仍有 toStatus（基座从别处捡到了？）：%s", where, body)
				}

				if status != http.StatusBadRequest {
					t.Fatalf("%s：缺 toStatus 应被形状档拒成 400，实际 %d（body=%s）", where, status, body)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orderID := mkOrder(tc.name + "@example.com")
			cap := &orderMutationCapture{}
			h := orderhttp.NewHandle(f.orders)
			listPage := orderhttp.NewOrderPageHandle(f.orders, projects, nil)

			engine, cleanup, berr := support.SetupTestBootstrap(support.BootstrapOptions{
				ConfigPath:     support.NewComponentTestConfig(t),
				GinMode:        gin.TestMode,
				InitComponents: true,
				RouteRegistrar: func(e *gin.Engine) {
					e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
					e.Use(func(c *gin.Context) {
						body, _ := io.ReadAll(c.Request.Body)
						c.Request.Body = io.NopCloser(bytes.NewReader(body))
						c.Next()
						if c.Request.Method == http.MethodPost {
							cap.add(string(body), c.Writer.Status())
						}
					})
					e.GET("/admin/orders", adminui.PageSlot(orderhttp.OrderListPageTemplate), listPage.OrdersPage)
					e.POST("/api/order/status", h.ChangeStatus)
				},
			})
			if berr != nil {
				t.Fatalf("组件初始化失败: %v", berr)
			}
			defer cleanup()
			server := httptest.NewServer(engine)
			defer server.Close()

			detail := ""
			if tc.mode == "create" {
				detail = "&orderId=" + strconv.FormatUint(orderID, 10)
			}
			pageRes, perr := http.Get(fmt.Sprintf("%s/admin/orders?project=%s%s", server.URL, f.projectID, detail))
			if perr != nil {
				t.Fatalf("取列表页失败: %v", perr)
			}
			raw, _ := io.ReadAll(pageRes.Body)
			pageRes.Body.Close()
			if pageRes.StatusCode != http.StatusOK {
				t.Fatalf("列表页应 200，实际 %d", pageRes.StatusCode)
			}
			html := tc.mutate(string(raw))

			var formAttrs, submitterAttrs map[string]string
			var fields orderChainFields
			if tc.mode == "bulk" {
				form := orderChainFormSlice(t, html, `data-write-url="/api/order/status"`+` data-write="bulk"`)
				formAttrs = couponAttrsOf(form[:strings.Index(form, ">")+1])
				submitterAttrs = orderChainButtonAttrs(t, html, tc.api, tc.mode)

				target := orderChainSelectOptions(t, form, "toStatus")[0]
				if tc.name == "M2-number-toStatus" {
					target = "1"
				}
				fields = orderChainFields{
					{Name: "ids", Value: strconv.FormatUint(orderID, 10), Checked: true, Type: "checkbox"},
					{Name: "toStatus", Value: target, Tag: "select"},
					{Name: "remark", Value: "反证"},
				}
				tc.target = target
			} else {
				form := orderChainFormSlice(t, html, `data-write-url="/api/order/status"`+` data-write="create"`)
				formAttrs = couponAttrsOf(form[:strings.Index(form, ">")+1])
				for _, tag := range couponButtonRe.FindAllString(form, -1) {
					if couponAttrOf(tag, "type") == "submit" {
						submitterAttrs = couponAttrsOf(tag)
						break
					}
				}
				target := orderChainSelectOptions(t, form, "toStatus")[0]
				fields = orderChainFields{
					{Name: "orderId", Value: strconv.FormatUint(orderID, 10)},
					{Name: "toStatus", Value: target, Tag: "select"},
					{Name: "remark", Value: ""},
				}
				tc.target = target
			}

			fixturePath := filepath.Join(t.TempDir(), "fixture.json")
			rawFixture, _ := json.Marshal(map[string]any{
				"uiDir": filepath.Join("..", "..", "..", "..", "internal", "templates", "static", "js", "ui"),
				"base":  server.URL,
				"csrf":  "CHAIN-TOKEN",
				"cases": []map[string]any{{
					"name":           tc.name,
					"formAttrs":      formAttrs,
					"submitterAttrs": submitterAttrs,
					"fields":         fields,
				}},
			})
			if werr := os.WriteFile(fixturePath, rawFixture, 0o600); werr != nil {
				t.Fatalf("写 fixture 失败: %v", werr)
			}
			out, rerr := exec.Command(node, "testdata/coupon_json_write_chain_driver.cjs", fixturePath).Output()
			if rerr != nil {
				t.Fatalf("驱动脚本失败: %v\n%s", rerr, string(out))
			}
			if strings.Contains(string(out), `"error"`) {
				t.Fatalf("驱动报错（改坏的属性可能让基座提前抛异常，这也是被抓住的一种形态）：%s", string(out))
			}
			body, status, seen := cap.last()
			if !seen {

				return
			}
			tc.owns(t, tc.name, body, status, f.orderStatusOf(t, orderID), tc.target)
		})
	}
}
