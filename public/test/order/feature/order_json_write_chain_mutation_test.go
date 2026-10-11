package feature

// order_json_write_chain_mutation_test.go — 链式证据的**反证**：把模板里那几处关键声明逐个
// 改坏，链必须当场断（判据不是空转）。
//
// 为什么单独立一条而不是写在主用例里：主用例的判据是「属性齐全 ⇒ 请求体类型正确 ⇒ 200」，
// 它天然无法证明「少一个属性会被抓到」—— 这正是 S1 踩过的坑（静态检查全绿、请求却是字符串
// status）。所以这里在**内存里**把渲染出来的 HTML 改坏（不动共享树上的模板文件），再把同一套
// 驱动跑一遍，断言服务端的回执确实变成了对应的失败形态。三条反证各自钉一个属性：
//
//	M1 drop-bulk-extra    批量流转按钮少了 data-write-extra → toStatus 不在请求体里 → **400**（形状档）；
//	M2 number-toStatus    给 toStatus 加上 data-write-number（伪契约）→ 体里是**数字** → 400；
//	M3 drop-detail-number 详情表单少了 data-write-number="orderId" → orderId 变字符串 → 400。
//
// 三条都靠**服务端状态码 + 请求体类型**判定，不看前端的文案（文案会随语言与实现变）。

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// orderMutationCapture 与主用例同形的最小记录器（独立一份，避免两条用例共享可变状态）。
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

// TestOrderJSONWriteChainCatchesMissingTemplateAttributes 三条模板反证：各自必须被链抓住。
func TestOrderJSONWriteChainCatchesMissingTemplateAttributes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机没有 node：反证同样要求真实基座 json_write.js，跳过而不是用桩冒充")
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
		// target：这条反证里驱动发出去的目标状态（bulk 用，M2 刻意用非法数字目标）。
		target string
		// owns：这条反证要证明的正是**主用例的成功判据会变红**。
		// 判据有两半：服务端 200 **且** 库里状态真的变成了目标值 ——
		// 只判 200 会漏掉最阴的一种：字段没发出去时 handler 不报错，而是「0 个成功、N 个被跳过」，
		// 服务端 200 而状态原地不动（用户看到的就是「一条都没做」，然后反复重试）。
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
				// 缺 toStatus 属**形状档** → 400 且零 service 调用（lead 裁定 2，见
				// order_bulk_write.go:orderStatusBulk 的口径说明）。
				//
				// 这条变异原先是「200 + 全 skipped」的静默档：服务端装成功、库里一动不动，
				// 只能靠「主用例顺带断言了 DB」才抓得到 —— 客户端漏字段被伪装成成功。
				// 收敛为显式 400 后，同一个变异红得更硬（不再依赖 DB 那一半判据）。
				if status != http.StatusBadRequest {
					t.Fatalf("%s：缺 toStatus 应被形状档拒成 400，实际 %d（body=%s）", where, status, body)
				}
			},
		},
		{
			name: "M2-number-toStatus",
			mutate: func(html string) string {
				return strings.Replace(html,
					`data-write-extra="toStatus,remark"`,
					`data-write-extra="toStatus,remark" data-write-number="toStatus"`, 1)
			},
			api:  "/api/order/status",
			mode: "bulk",
			owns: func(t *testing.T, where, body string, status int, statusNow, target string) {
				// 伪契约的形态：体里 toStatus 变成**数字**，而 handler 只认字符串 → 400。
				if status == http.StatusOK {
					t.Fatalf("%s：把 toStatus 声明成数字字段后服务端仍 200（伪契约没被拒）：%s", where, body)
				}
			},
		},
		{
			name: "M3-drop-detail-number",
			mutate: func(html string) string {
				return strings.Replace(html, ` data-write-number="orderId"`, "", 1)
			},
			api:  "/api/order/status",
			mode: "create",
			owns: func(t *testing.T, where, body string, status int, statusNow, target string) {
				// orderId 变字符串 → 绑定/解析失败 → 400。
				if status == http.StatusOK {
					t.Fatalf("%s：少了 data-write-number=\"orderId\" 时服务端仍 200（orderId 变字符串也该被拒）：%s", where, body)
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
					e.GET("/admin/orders", listPage.OrdersPage)
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
				form := orderChainFormSlice(t, html, `data-write-api="/api/order/status"`+` data-write-mode="bulk"`)
				formAttrs = couponAttrsOf(form[:strings.Index(form, ">")+1])
				submitterAttrs = orderChainButtonAttrs(t, html, tc.api, tc.mode)
				// M2 用「数字目标」把伪契约暴露出来：把 toStatus 写成 paid 的话基座照发字符串、
				// 服务端拒得一样（400），但那样就分不清「被拒是因为形状」还是「因为状态非法」。
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
				form := orderChainFormSlice(t, html, `data-write-api="/api/order/status"`+` data-write-mode="create"`)
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
				// 属性被改坏后基座一个请求都没发（例如按钮端的端点声明被连带改掉）——同样算抓住。
				return
			}
			tc.owns(t, tc.name, body, status, f.orderStatusOf(t, orderID), tc.target)
		})
	}
}
