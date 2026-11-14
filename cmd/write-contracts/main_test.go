package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeSources(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, source := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestRoutesUseMethodPathAndRealHandler 不再按 /api 前缀或资源名猜处理器。
func TestRoutesUseMethodPathAndRealHandler(t *testing.T) {
	raw := `DECLARED POST /api/ignored permission
HANDLER GET /admin/items example.Read
HANDLER POST /admin/items example.(*Handle).Save-fm
HANDLER DELETE /admin/items example.(*Handle).Delete-fm
HANDLER POST /analytics/collect example.Collect
`
	routes, err := parseRoutes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 || routes[0].Method != "DELETE" || routes[1].Path != "/admin/items" || routes[2].Path != "/analytics/collect" {
		t.Fatalf("方法、非 API 路径或处理器丢失：%+v", routes)
	}
	for _, invalid := range []string{"DECLARED POST /api/ignored permission", "HANDLER POST /x", raw + "HANDLER POST /admin/items other.Handler\n"} {
		if _, err := parseRoutes(invalid); err == nil {
			t.Fatalf("缺失、损坏或重复处理器表必须报错：%s", invalid)
		}
	}
}

func fixtureIndex(t *testing.T) sourceIndex {
	t.Helper()
	root := writeSources(t, map[string]string{
		"dto/request.go": "package othername\ntype Request struct { ID string `json:\"jsonId\" form:\"id\" binding:\"required\"`; Amount string `json:\"amount\" form:\"amountYuan\"`; HiddenJSON string `json:\"-\" form:\"visibleForm\"`; HiddenForm string `json:\"visibleJSON\" form:\"-\"`; DefaultName string; private string }\n",
		"http/handle.go": `package examplehttp
import (
 request "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
type Other struct{}
type Local struct { Value string }
func (h *Handle) Save(c *gin.Context) { var req request.Request; c.ShouldBindWith(&req, wire.FormPost) }
func (h *Handle) JSON(c *gin.Context) { req := &request.Request{}; c.ShouldBindJSON(req) }
func (h *Handle) Multipart(c *gin.Context) { req := request.Request{}; c.ShouldBindWith(&req, wire.FormMultipart) }
func (h *Other) Save(c *gin.Context) { var req Local; c.ShouldBindJSON(&req) }
func (h *Handle) Auto(c *gin.Context) { var req request.Request; c.ShouldBind(&req) }
func (h *Handle) Query(c *gin.Context) { var req request.Request; c.ShouldBindQuery(&req) }
func (h *Handle) URI(c *gin.Context) { var req request.Request; c.ShouldBindUri(&req) }
func (h *Handle) Unknown(c *gin.Context) { var req request.Request; c.ShouldBindWith(&req, wire.Form) }
func (h *Handle) Mixed(c *gin.Context) { var a request.Request; var b Local; c.ShouldBindJSON(&a); c.ShouldBindWith(&b, wire.FormPost) }
func (h *Handle) Callback(c *gin.Context) { consume(func() { var req request.Request; c.ShouldBindJSON(&req) }) }
func Direct(c *gin.Context) { var req Local; c.ShouldBindJSON(&req) }
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	return index
}

// TestExplicitBinderSelectsWireTags 同一 DTO 的字段表必须由每个端点的真实 binder 决定。
func TestExplicitBinderSelectsWireTags(t *testing.T) {
	index := fixtureIndex(t)
	routes := []route{
		{Method: "POST", Path: "/same", Handler: "example/internal/http.(*Handle).Save-fm"},
		{Method: "PUT", Path: "/same", Handler: "example/internal/http.(*Handle).JSON-fm"},
		{Method: "POST", Path: "/upload", Handler: "example/internal/http.(*Handle).Multipart-fm"},
	}
	got := buildContracts(routes, index)
	formFields := []fieldSchema{{Name: "id", Required: true}, {Name: "amountYuan"}, {Name: "visibleForm"}, {Name: "DefaultName"}}
	jsonFields := []fieldSchema{{Name: "jsonId", Required: true}, {Name: "amount"}, {Name: "visibleJSON"}, {Name: "DefaultName"}}
	for _, tc := range []struct {
		key      string
		protocol string
		fields   []fieldSchema
	}{
		{"POST /same", "form", formFields}, {"PUT /same", "json", jsonFields}, {"POST /upload", "multipart", formFields},
	} {
		ep := got[tc.key]
		if ep.Reason != "" || ep.Protocol != tc.protocol || !reflect.DeepEqual(ep.Fields, tc.fields) || ep.DTO != "example/internal/dto.Request" {
			t.Fatalf("%s 请求契约错误：%+v", tc.key, ep)
		}
	}
}

func TestRealReceiverIdentityCannotBeReplacedBySameMethodName(t *testing.T) {
	index := fixtureIndex(t)
	got := buildContracts([]route{
		{Method: "POST", Path: "/unexpected", Handler: "example/internal/http.(*Other).Save-fm"},
		{Method: "POST", Path: "/direct", Handler: "example/internal/http.Direct"},
	}, index)
	for _, key := range []string{"POST /unexpected", "POST /direct"} {
		if ep := got[key]; ep.Protocol != "json" || ep.DTO != "example/internal/http.Local" || len(ep.Fields) != 1 || ep.Fields[0].Name != "Value" {
			t.Fatalf("同名方法不得错配，直接函数也必须识别：%+v", ep)
		}
	}
}

func TestUnknownInputsRemainVisibleInsteadOfGuessing(t *testing.T) {
	index := fixtureIndex(t)
	// Query 走显式 ShouldBindQuery：协议是确定的 query（字段取 form tag），
	// 不再属于「无法确认」。其余形状仍必须如实未检查。
	for _, name := range []string{"Auto", "URI", "Unknown", "Mixed", "Callback", "Missing", "Save.func1"} {
		t.Run(name, func(t *testing.T) {
			r := route{Method: "POST", Path: "/x", Handler: "example/internal/http.(*Handle)." + name + "-fm"}
			ep := buildContracts([]route{r}, index)["POST /x"]
			if ep.Protocol != "unknown" || ep.Reason == "" || len(ep.Fields) != 0 {
				t.Fatalf("无法确认的请求必须明确未检查：%+v", ep)
			}
		})
	}
	t.Run("Query", func(t *testing.T) {
		ep := buildContracts([]route{{Method: "POST", Path: "/x",
			Handler: "example/internal/http.(*Handle).Query-fm"}}, index)["POST /x"]
		if ep.Protocol != "query" || ep.Reason != "" || ep.DTO != "example/internal/dto.Request" {
			t.Fatalf("显式 ShouldBindQuery 应确定 query 协议：%+v", ep)
		}
	})
}

func TestEmbeddedDTOFieldsAndRequiredSemantics(t *testing.T) {
	root := writeSources(t, map[string]string{
		"a.go": "package testdto\ntype Apply struct { *Rule; Note string `json:\"note\" form:\"note\" binding:\"required\"` }; type Forced struct { *Rule `binding:\"required\"` }; type Named struct { Rule `json:\"nested\" form:\"ignoredOnEmbedded\"` }\n",
		"z.go": "package testdto\ntype Rule struct { Project string `json:\"project\" form:\"projectId\" binding:\"required\"`; IDs []string `json:\"ids\" form:\"ids\" binding:\"dive,required\"`; Conditional int `json:\"conditional\" form:\"conditional\" binding:\"required_with=Project\"`; Secret string `json:\"-\" form:\"-\"` }\n",
	})
	index, err := scanSources(root, "example/dto")
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"json", "form", "multipart"} {
		fields, err := dtoFields("example/dto.Apply", protocol, index.Types, map[string]bool{})
		if err != nil || len(fields) != 4 || fields[0].Required || fields[1].Required || fields[2].Required || !fields[3].Required {
			t.Fatalf("指针可选、条件或元素必填处理错误：%+v err=%v", fields, err)
		}
		fields, err = dtoFields("example/dto.Forced", protocol, index.Types, map[string]bool{})
		if err != nil || !fields[0].Required {
			t.Fatalf("必填嵌入不得丢失：%+v err=%v", fields, err)
		}
		fields, err = dtoFields("example/dto.Named", protocol, index.Types, map[string]bool{})
		if err != nil || (protocol == "json" && (len(fields) != 1 || fields[0].Name != "nested")) || (protocol != "json" && (len(fields) != 3 || fields[0].Name != "projectId")) {
			t.Fatalf("具名 JSON 嵌入与 Gin form 展开不应混淆：%+v err=%v", fields, err)
		}
	}
}

func TestMissingDTOCannotProduceAnEmptyVerifiedContract(t *testing.T) {
	index := fixtureIndex(t)
	delete(index.Types, "example/internal/dto.Request")
	ep := buildContracts([]route{{Method: "POST", Path: "/x", Handler: "example/internal/http.(*Handle).Save-fm"}}, index)["POST /x"]
	if ep.Protocol != "unknown" || !strings.Contains(ep.Reason, "请求类型缺失") {
		t.Fatalf("缺失类型不能当作无字段请求：%+v", ep)
	}
}

func TestProductionI18nContractUsesNativeFormPost(t *testing.T) {
	t.Chdir("../..")
	index, err := scanSources("internal", "go_wp/internal")
	if err != nil {
		t.Fatal(err)
	}
	r := route{Method: "POST", Path: "/api/i18n/save", Handler: "go_wp/internal/module/admin/inbound/http.(*adminI18nEntryHandle).I18nEntryWrite-fm"}
	ep := buildContracts([]route{r}, index)["POST /api/i18n/save"]
	want := []fieldSchema{{Name: "action", Required: true}, {Name: "key"}, {Name: "lang"}, {Name: "value"}, {Name: "category"}, {Name: "remark"}, {Name: "ids"}}
	if ep.Protocol != "form" || ep.Reason != "" || !reflect.DeepEqual(ep.Fields, want) {
		t.Fatalf("生产词条表单契约错误：%+v", ep)
	}
}

// TestBindingRequiresGinContextAndLexicalRequest 不把同名方法或内层遮蔽变量冒充真实请求。
func TestBindingRequiresGinContextAndLexicalRequest(t *testing.T) {
	root := writeSources(t, map[string]string{
		"handle.go": `package handler
import "github.com/gin-gonic/gin"
type Outer struct { ID string }
type Inner struct { Other string }
type Fake struct{}
func (f *Fake) ShouldBindJSON(v any) {}
func Shadow(c *gin.Context) { var req Outer; { var req Inner; consume(req) }; c.ShouldBindJSON(&req) }
func Initializer(c *gin.Context) { var req = Outer{}; c.ShouldBindJSON(&req) }
func NotGin(c *Fake) { var req Outer; c.ShouldBindJSON(&req) }
func ContextShadow(c *gin.Context) { { c := &Fake{}; var req Outer; c.ShouldBindJSON(&req) } }
`,
	})
	index, err := scanSources(root, "example/handler")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Shadow", "Initializer", "NotGin", "ContextShadow"} {
		ep := buildContracts([]route{{Method: "POST", Path: "/x", Handler: "example/handler." + name}}, index)["POST /x"]
		if name == "Shadow" || name == "Initializer" {
			if ep.Reason != "" || ep.DTO != "example/handler.Outer" || len(ep.Fields) != 1 || ep.Fields[0].Name != "ID" {
				t.Fatalf("%s 识别了错误作用域或遗漏具名初始化：%+v", name, ep)
			}
		} else if ep.Protocol != "unknown" || ep.Reason == "" {
			t.Fatalf("%s 的非 Gin 同名方法不得产生确定契约：%+v", name, ep)
		}
	}
}

func TestDuplicateWireFieldsCannotProduceVerifiedContract(t *testing.T) {
	root := writeSources(t, map[string]string{"dto.go": "package dto\ntype A struct { ID string `form:\"id\"` }; type B struct { ID string `form:\"id\"` }; type Request struct { A; B }"})
	index, err := scanSources(root, "example/dto")
	if err != nil {
		t.Fatal(err)
	}
	_, err = dtoFields("example/dto.Request", "form", index.Types, map[string]bool{})
	if err == nil {
		t.Fatal("冲突嵌入字段不能假称已模拟 binder 的覆盖规则")
	}
}

func TestMarshalStableKeepsBothMethods(t *testing.T) {
	contracts := map[string]endpointSchema{
		"POST /x":   {Protocol: "form", Handler: "create", Fields: []fieldSchema{}},
		"DELETE /x": {Protocol: "json", Handler: "delete", Fields: []fieldSchema{}},
	}
	first, err := marshalStable(contracts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := marshalStable(contracts)
	if err != nil || string(first) != string(second) || !strings.Contains(string(first), "POST /x") || !strings.Contains(string(first), "DELETE /x") {
		t.Fatalf("方法不能相互覆盖，产物必须确定：%s err=%v", first, err)
	}
}

// TestDelegatedHandlerInheritsCalleeBinder 锁住「处理器把请求转交给另一个处理器」：
// 输入契约由被委托方决定，不能因为本函数没有 binder 就报未检查。
func TestDelegatedHandlerInheritsCalleeBinder(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\ntype SaveReq struct { ID string `form:\"id\" binding:\"required\"` }\n" +
			"type OtherReq struct { Note string `form:\"note\"` }\n",
		"http/handle.go": `package examplehttp
import (
 req "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
func (h *Handle) Export(c *gin.Context) { h.saveImpl(c) }
func (h *Handle) saveImpl(c *gin.Context) { var in req.SaveReq; c.ShouldBindWith(&in, wire.FormPost) }
func (h *Handle) Ambiguous(c *gin.Context) { h.saveImpl(c); h.other(c) }
func (h *Handle) other(c *gin.Context) { var in req.OtherReq; c.ShouldBindWith(&in, wire.FormPost) }
func (h *Handle) Wrapped(c *gin.Context) { h.nested(c) }
func (h *Handle) nested(c *gin.Context) { h.deeper(c) }
func (h *Handle) deeper(c *gin.Context) { var in req.OtherReq; c.ShouldBindWith(&in, wire.FormPost) }
func (h *Handle) NoCallee(c *gin.Context) { passed := c; _ = passed }
func (h *Handle) WithHelpers(c *gin.Context) { h.saveImpl(c); h.notify(c) }
func (h *Handle) notify(c *gin.Context) { _ = c.Request.URL.Path }
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	got := buildContracts([]route{
		{Method: "POST", Path: "/export", Handler: "example/internal/http.(*Handle).Export-fm"},
		{Method: "POST", Path: "/ambiguous", Handler: "example/internal/http.(*Handle).Ambiguous-fm"},
		{Method: "POST", Path: "/wrapped", Handler: "example/internal/http.(*Handle).Wrapped-fm"},
		{Method: "POST", Path: "/with-helpers", Handler: "example/internal/http.(*Handle).WithHelpers-fm"},
	}, index)

	export := got["POST /export"]
	if export.Reason != "" || export.Protocol != "form" || export.DTO != "example/internal/dto.SaveReq" ||
		!reflect.DeepEqual(export.Fields, []fieldSchema{{Name: "id", Required: true}}) {
		t.Fatalf("委托端点应继承被委托方的 binder 契约：%+v", export)
	}

	ambiguous := got["POST /ambiguous"]
	if ambiguous.Protocol != "unknown" || !strings.Contains(ambiguous.Reason, "多个处理器") {
		t.Fatalf("多条委托必须如实报未检查：%+v", ambiguous)
	}

	// 二层包装沿链展开（有界深度）：处理器 → bindX → bindXInto 是常见三层，
	// 只跟一层会把这类端点误报成「没有 binder」。
	wrapped := got["POST /wrapped"]
	if wrapped.Reason != "" || wrapped.Protocol != "form" || wrapped.DTO != "example/internal/dto.OtherReq" {
		t.Fatalf("二层包装应沿委托链取到真实 binder：%+v", wrapped)
	}

	// 处理器同时委托「绑定」与「无绑定的辅助函数」时，输入契约由带 binder 的那条决定；
	// 辅助函数不是第二个输入形状，不该让端点降级成未检查。
	withHelpers := got["POST /with-helpers"]
	if withHelpers.Reason != "" || withHelpers.Protocol != "form" ||
		withHelpers.DTO != "example/internal/dto.SaveReq" {
		t.Fatalf("只有一条委托带 binder 时应直接采用：%+v", withHelpers)
	}
}

// 泛型写骨架：请求类型由调用方实参决定，不能因为骨架体里看到的是类型参数就报 unknown。
// 两种写法都要认：显式类型实参 f[XReq](...) 与回调推断 f(func(ctx, r *XReq) ...)。
func TestGenericSkeletonResolvesCallerRequestType(t *testing.T) {
	src := `package examplehttp
import (
 "context"
 req "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
func runJSON[T any](c *gin.Context, call func(context.Context, *T) (any, error)) {
 var in T
 if err := c.ShouldBindJSON(&in); err != nil { return }
 call(c.Request.Context(), &in)
}
func runForm[T any](c *gin.Context, call func(context.Context, *T) (any, error)) {
 var in T
 if err := c.ShouldBindWith(&in, wire.FormPost); err != nil { return }
 call(c.Request.Context(), &in)
}
type Handle struct{}
func (h *Handle) Explicit(c *gin.Context) {
 runJSON[req.SaveReq](c, func(ctx context.Context, r *req.SaveReq) (any, error) { return nil, nil })
}
func (h *Handle) Inferred(c *gin.Context) {
 runJSON(c, func(ctx context.Context, r *req.SaveReq) (any, error) { return nil, nil })
}
func (h *Handle) Form(c *gin.Context) {
 runForm(c, func(ctx context.Context, r *req.RateReq) (any, error) { return nil, nil })
}
`
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\ntype SaveReq struct { ID string `json:\"id\" binding:\"required\"` }\n" +
			"type RateReq struct { Score int `form:\"score\" binding:\"required\"` }\n",
		"http/handle.go": src,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	got := buildContracts([]route{
		{Method: "POST", Path: "/explicit", Handler: "example/internal/http.(*Handle).Explicit-fm"},
		{Method: "POST", Path: "/inferred", Handler: "example/internal/http.(*Handle).Inferred-fm"},
		{Method: "POST", Path: "/form", Handler: "example/internal/http.(*Handle).Form-fm"},
	}, index)
	if got["POST /explicit"].Protocol != "json" {
		t.Fatalf("显式泛型调用应解析成 json：%+v", got["POST /explicit"])
	}
	if got["POST /inferred"].Protocol != "json" || got["POST /inferred"].DTO != "example/internal/dto.SaveReq" {
		t.Fatalf("推断泛型调用应解析成调用方的请求类型：%+v", got["POST /inferred"])
	}
	if got["POST /form"].Protocol != "form" {
		t.Fatalf("表单骨架应保持 form：%+v", got["POST /form"])
	}
}

// 显式转换函数作为骨架实参时，字段契约属主是**表单类型**，不是转换后的 service 请求类型。
func TestGenericSkeletonPrefersConversionFormType(t *testing.T) {
	src := `package examplehttp
import (
 "context"
 req "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
func runWrite[F any, R any](c *gin.Context,
 build func(*F) (R, error),
 call func(context.Context, R) (any, error),
 success func(*gin.Context)) {
 var form F
 if err := c.ShouldBindWith(&form, wire.FormPost); err != nil { return }
 r, err := build(&form)
 if err != nil { return }
 if _, err := call(c.Request.Context(), r); err != nil { return }
 success(c)
}
type Handle struct{}
func (h *Handle) Create(c *gin.Context) {
 runWrite(c, (*req.SaveForm).CreateReq,
  func(ctx context.Context, r *req.SaveReq) (any, error) { return nil, nil },
  func(*gin.Context) {})
}
`
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\n" +
			"type SaveForm struct { Name string `form:\"name\"`; Amount string `form:\"amountYuan\"` }\n" +
			"func (f *SaveForm) CreateReq() (*SaveReq, error) { return &SaveReq{}, nil }\n" +
			"type SaveReq struct { Name string `json:\"name\"`; Amount int64 `json:\"amount\"` }\n",
		"http/handle.go": src,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	got := buildContracts([]route{
		{Method: "POST", Path: "/create", Handler: "example/internal/http.(*Handle).Create-fm"},
	}, index)
	create := got["POST /create"]
	if create.Reason != "" || create.Protocol != "form" {
		t.Fatalf("转换函数形态应解析成表单协议：%+v", create)
	}
	if create.DTO != "example/internal/dto.SaveForm" {
		t.Fatalf("字段契约应取自表单类型 SaveForm，实际 %q", create.DTO)
	}
	names := map[string]bool{}
	for _, f := range create.Fields {
		names[f.Name] = true
	}
	if !names["amountYuan"] || names["amount"] {
		t.Fatalf("表单字段应是 amountYuan（不是转换后请求的 amount）：%+v", create.Fields)
	}
}

// pkg/binding 的 Form/JSON 是兼容入口（按 Content-Type 在 JSON 与表单间选择），
// 本身不声明唯一协议 —— 必须如实报 unknown。声明协议的端点用显式入口。
func TestCompatBindersStayUnchecked(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\n" +
			"type SaveReq struct { ID string `form:\"id\" json:\"id\" binding:\"required\"` }\n",
		"http/handle.go": `package examplehttp
import (
 req "example/internal/dto"
 wire "go_wp/pkg/binding"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
func (h *Handle) Compat(c *gin.Context) { wire.JSON[req.SaveReq](c) }
func (h *Handle) CompatForm(c *gin.Context) { wire.Form[req.SaveReq](c) }
func (h *Handle) Explicit(c *gin.Context) { wire.ParseJSON[req.SaveReq](c) }
func (h *Handle) ExplicitForm(c *gin.Context) { wire.ParseForm[req.SaveReq](c) }
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	got := buildContracts([]route{
		{Method: "POST", Path: "/compat", Handler: "example/internal/http.(*Handle).Compat-fm"},
		{Method: "POST", Path: "/compat-form", Handler: "example/internal/http.(*Handle).CompatForm-fm"},
		{Method: "POST", Path: "/explicit", Handler: "example/internal/http.(*Handle).Explicit-fm"},
		{Method: "POST", Path: "/explicit-form", Handler: "example/internal/http.(*Handle).ExplicitForm-fm"},
	}, index)
	for _, key := range []string{"POST /compat", "POST /compat-form"} {
		if ep := got[key]; ep.Protocol != "unknown" || ep.Reason == "" {
			t.Fatalf("%s 走兼容入口必须如实报未检查：%+v", key, ep)
		}
	}
	for key, want := range map[string]string{"POST /explicit": "json", "POST /explicit-form": "form"} {
		if ep := got[key]; ep.Protocol != want || ep.DTO != "example/internal/dto.SaveReq" {
			t.Fatalf("%s 走显式入口应确定协议 %s：%+v", key, want, ep)
		}
	}
}

// 一条明确表单绑定分支 + 一条无法确定的委托分支，整体必须报未检查：
// 过滤掉未知分支再报「唯一的 form」等于把欠账藏进绿灯。
func TestDelegationWithUnknownBranchIsNotReportedAsResolved(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\ntype SaveReq struct { ID string `form:\"id\"` }\n",
		"http/handle.go": `package examplehttp
import (
 req "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
func (h *Handle) Export(c *gin.Context) { h.save(c); h.loose(c) }
func (h *Handle) save(c *gin.Context) { var in req.SaveReq; c.ShouldBindWith(&in, wire.FormPost) }
func (h *Handle) loose(c *gin.Context) { var in req.SaveReq; c.ShouldBind(&in) }
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	ep := buildContracts([]route{
		{Method: "POST", Path: "/export", Handler: "example/internal/http.(*Handle).Export-fm"},
	}, index)["POST /export"]
	if ep.Protocol != "unknown" || ep.Reason == "" {
		t.Fatalf("含未知委托分支的端点必须报未检查：%+v", ep)
	}
}

// 泛型骨架有输入 F 与输出 R：字段契约取 binder 真正绑定的 F，不能按「回调第一个指针形参」猜。
func TestGenericSkeletonUsesBoundTypeParamNotFirstCallbackArg(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\n" +
			"type UpdateForm struct { Name string `form:\"name\"` }\n" +
			"type UpdateResp struct { Name string `json:\"name\"` }\n",
		"http/handle.go": `package examplehttp
import (
 "context"
 req "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
func runWrite[F any, R any](c *gin.Context,
 call func(context.Context, *R) (any, error),
 success func(*gin.Context)) {
 var form F
 if err := c.ShouldBindWith(&form, wire.FormPost); err != nil { return }
 _ = form
 success(c)
}
type Handle struct{}
func (h *Handle) Update(c *gin.Context) {
 runWrite[req.UpdateForm, req.UpdateResp](c,
  func(ctx context.Context, r *req.UpdateResp) (any, error) { return nil, nil },
  func(*gin.Context) {})
}
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	ep := buildContracts([]route{
		{Method: "POST", Path: "/update", Handler: "example/internal/http.(*Handle).Update-fm"},
	}, index)["POST /update"]
	if ep.Reason != "" || ep.Protocol != "form" {
		t.Fatalf("双类型参数骨架应解析成表单：%+v", ep)
	}
	if ep.DTO != "example/internal/dto.UpdateForm" {
		t.Fatalf("应取被绑定的输入类型 UpdateForm，实际 %q", ep.DTO)
	}
	if len(ep.Fields) != 1 || ep.Fields[0].Name != "name" {
		t.Fatalf("字段表应来自输入类型：%+v", ep.Fields)
	}
}

// 入口先按请求字段分叉（有 ids 走批量、否则单条）时，同一 METHOD+PATH 有两个输入形状：
// 主契约是单条 DTO，变体是批量 DTO。只有两条分支都能确定解析且 DTO 不同才产出变体。
func TestEntryForkProducesVariantContract(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\n" +
			"type SingleReq struct { ID string `form:\"id\"` }\n" +
			"type BulkReq struct { IDs []string `form:\"ids\"`; Status string `form:\"status\"` }\n",
		"http/handle.go": `package examplehttp
import (
 req "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
func probeIDs(c *gin.Context) []string { return c.Request.PostForm["ids"] }
type Handle struct{}
func (h *Handle) Update(c *gin.Context) {
	if probeIDs(c) != nil {
		h.bulk(c)
		return
	}
	var in req.SingleReq
	if err := c.ShouldBindWith(&in, wire.FormPost); err != nil { return }
}
func (h *Handle) bulk(c *gin.Context) {
	var in req.BulkReq
	if err := c.ShouldBindWith(&in, wire.FormPost); err != nil { return }
}
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	ep := buildContracts([]route{
		{Method: "POST", Path: "/update", Handler: "example/internal/http.(*Handle).Update-fm"},
	}, index)["POST /update"]
	if ep.Reason != "" || ep.Protocol != "form" || ep.DTO != "example/internal/dto.SingleReq" {
		t.Fatalf("主契约应是单条 DTO：%+v", ep)
	}
	if len(ep.Variants) != 1 {
		t.Fatalf("分叉入口应产出一条变体：%+v", ep.Variants)
	}
	v := ep.Variants[0]
	if v.DTO != "example/internal/dto.BulkReq" || v.Protocol != "form" {
		t.Fatalf("变体应是批量 DTO：%+v", v)
	}
	names := map[string]bool{}
	for _, f := range v.Fields {
		names[f.Name] = true
	}
	if !names["ids"] || !names["status"] {
		t.Fatalf("变体字段应来自批量 DTO：%+v", v.Fields)
	}
}

// auditContract 用最小的 dto/http 夹具解析一条写端点契约，供一组反例共用。
func auditContract(t *testing.T, body string) endpointSchema {
	t.Helper()
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\ntype Input struct { ID string `form:\"id\" json:\"id\"` }\ntype Output struct { Label string `form:\"label\" json:\"label\"` }\n",
		"http/handle.go": "package examplehttp\nimport (req \"example/internal/dto\"; wire \"github.com/gin-gonic/gin/binding\"; \"github.com/gin-gonic/gin\")\ntype Handle struct{}\n" + body,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	return buildContracts([]route{{Method: "POST", Path: "/save", Handler: "example/internal/http.(*Handle).Save-fm"}}, index)["POST /save"]
}

// 本地有确定的 binder，但另一条分支委托给了未知协议：整条链仍属未检查。
func TestAuditLocalBinderMustNotHideUnknownDelegate(t *testing.T) {
	ep := auditContract(t, `
func (h *Handle) Save(c *gin.Context) {
 if c.GetBool("legacy") { h.loose(c); return }
 var in req.Input
 _ = c.ShouldBindWith(&in, wire.FormPost)
}
func (h *Handle) loose(c *gin.Context) { var in req.Input; _ = c.ShouldBind(&in) }
`)
	if ep.Protocol != "unknown" || ep.Reason == "" {
		t.Fatalf("本地明确 binder 不得掩盖未知委托分支：%+v", ep)
	}
}

// 未知协议藏在下沉一层委托里，同样不能被上层的确定 binder 洗白。
func TestAuditNestedUnknownDelegateMustSurvive(t *testing.T) {
	ep := auditContract(t, `
func (h *Handle) Save(c *gin.Context) {
 if c.GetBool("legacy") { h.bridge(c); return }
 h.known(c)
}
func (h *Handle) bridge(c *gin.Context) { h.loose(c) }
func (h *Handle) loose(c *gin.Context) { var in req.Input; _ = c.ShouldBind(&in) }
func (h *Handle) known(c *gin.Context) { var in req.Input; _ = c.ShouldBindWith(&in, wire.FormPost) }
`)
	if ep.Protocol != "unknown" || ep.Reason == "" {
		t.Fatalf("多层委托不得丢弃 unknown：%+v", ep)
	}
}

// 骨架里先有一个无关的泛型调用 warm[R]()，不能把它当成 binder 绑定的类型参数。
func TestAuditUnrelatedGenericCallMustNotChooseDTO(t *testing.T) {
	ep := auditContract(t, `
func warm[T any]() {}
func runWrite[F any, R any](c *gin.Context, prepare func(*R), save func(*F)) {
 warm[R]()
 var in F
 if err := c.ShouldBindWith(&in, wire.FormPost); err != nil { return }
 save(&in)
}
func (h *Handle) Save(c *gin.Context) {
 runWrite[req.Input, req.Output](c, func(*req.Output){}, func(*req.Input){})
}
`)
	if ep.Protocol != "unknown" && ep.DTO != "example/internal/dto.Input" {
		t.Fatalf("无关泛型调用不得替代实际 binder 的类型参数：%+v", ep)
	}
}

// 泛型骨架里两条分支各自 binder 不同协议，不能只取第一个 binder 就当已确定。
func TestAuditGenericBranchesMustNotChooseFirstProtocol(t *testing.T) {
	ep := auditContract(t, `
func bindJSON[T any](c *gin.Context) { var in T; _ = c.ShouldBindJSON(&in) }
func bindForm[T any](c *gin.Context) { var in T; _ = c.ShouldBindWith(&in, wire.FormPost) }
func runWrite[T any](c *gin.Context) {
 if c.GetBool("json") { bindJSON[T](c) } else { bindForm[T](c) }
}
func (h *Handle) Save(c *gin.Context) { runWrite[req.Input](c) }
`)
	if ep.Protocol != "unknown" || ep.Reason == "" {
		t.Fatalf("泛型多协议分支不得只取第一个 binder：%+v", ep)
	}
}

// 没有分叉的 handler 不得凭空产出变体。
func TestPlainHandlerProducesNoVariant(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\ntype SaveReq struct { ID string `form:\"id\"` }\n",
		"http/handle.go": `package examplehttp
import (
 req "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
func (h *Handle) Save(c *gin.Context) {
	var in req.SaveReq
	if err := c.ShouldBindWith(&in, wire.FormPost); err != nil { return }
}
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	ep := buildContracts([]route{
		{Method: "POST", Path: "/save", Handler: "example/internal/http.(*Handle).Save-fm"},
	}, index)["POST /save"]
	if len(ep.Variants) != 0 {
		t.Fatalf("无分叉的处理器不该产出变体：%+v", ep.Variants)
	}
}

// 显式查询串绑定必须给出确定协议与 form tag 字段表。
func TestExplicitQueryBindingDeclaresQueryProtocol(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": "package dto\n" +
			"type ListReq struct { Page int `form:\"page\"` }\n",
		"http/handle.go": `package examplehttp
import (
 req "example/internal/dto"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
func (h *Handle) List(c *gin.Context) {
	var in req.ListReq
	if err := c.ShouldBindQuery(&in); err != nil { return }
}
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	ep := buildContracts([]route{
		{Method: "POST", Path: "/list", Handler: "example/internal/http.(*Handle).List-fm"},
	}, index)["POST /list"]
	if ep.Protocol != "query" || ep.DTO != "example/internal/dto.ListReq" {
		t.Fatalf("显式查询串应确定 query 协议：%+v", ep)
	}
	if len(ep.Fields) != 1 || ep.Fields[0].Name != "page" {
		t.Fatalf("query 端点字段应取 form tag：%+v", ep.Fields)
	}
}

// 完全读不到 body 的处理器只能报 nobody；它必须是正向证明（读 body 就退回）。
func TestNoBinderEvidenceProvesNobodyOrRaw(t *testing.T) {
	root := writeSources(t, map[string]string{
		"http/handle.go": `package examplehttp
import (
 "io"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
func (h *Handle) Logout(c *gin.Context) { c.Status(200) }
func (h *Handle) Callback(c *gin.Context) { _, _ = io.ReadAll(io.LimitReader(c.Request.Body, 1024)) }
func (h *Handle) Manual(c *gin.Context) { _ = c.PostForm("id") }
func (h *Handle) Delegates(c *gin.Context) { h.Logout(c) }
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	got := buildContracts([]route{
		{Method: "POST", Path: "/logout", Handler: "example/internal/http.(*Handle).Logout-fm"},
		{Method: "POST", Path: "/callback", Handler: "example/internal/http.(*Handle).Callback-fm"},
		{Method: "POST", Path: "/manual", Handler: "example/internal/http.(*Handle).Manual-fm"},
		{Method: "POST", Path: "/delegates", Handler: "example/internal/http.(*Handle).Delegates-fm"},
	}, index)
	if ep := got["POST /logout"]; ep.Protocol != "nobody" {
		t.Fatalf("不读 body 的处理器应报 nobody：%+v", ep)
	}
	if ep := got["POST /callback"]; ep.Protocol != "raw" {
		t.Fatalf("读原始字节的处理器应报 raw：%+v", ep)
	}
	if ep := got["POST /manual"]; ep.Protocol != "unknown" || ep.Reason == "" {
		t.Fatalf("手工读表单仍未迁移，必须保持未检查：%+v", ep)
	}
	if ep := got["POST /delegates"]; ep.Protocol != "nobody" {
		t.Fatalf("委托给不读 body 的处理器应报 nobody：%+v", ep)
	}
}

// hookKeyDTO 与 hookKeyRule 用来锁定「BindForm 钩子读取的键必须进字段表」。
func TestBindFormHookKeysJoinContractFields(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": `package req
import "net/url"
type Rule struct { Project string ` + "`json:\"project\" form:\"projectId\" binding:\"required\"`" + ` }
func (r *Rule) BindForm(values url.Values) error { _ = values.Get("ruleKey"); return nil }
type SaveReq struct { Rule; Name string ` + "`json:\"name\" form:\"name\"`" + `; Flags []string ` + "`json:\"-\" form:\"-\"`" + ` }
func (r *SaveReq) BindForm(values url.Values) error { r.Flags = flagsFromForm(values); return nil }
func flagsFromForm(values url.Values) []string { return []string{values.Get("flagA"), values.Get("flagB")} }
`,
		"http/handle.go": `package handler
import (
 req "example/internal/dto"
 wire "github.com/gin-gonic/gin/binding"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
func (h *Handle) Form(c *gin.Context) { var form req.SaveReq; c.ShouldBindWith(&form, wire.FormPost) }
func (h *Handle) Multipart(c *gin.Context) { var form req.SaveReq; c.ShouldBindWith(&form, wire.FormMultipart) }
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	got := buildContracts([]route{
		{Method: "POST", Path: "/form", Handler: "example/internal/http.(*Handle).Form-fm"},
		{Method: "POST", Path: "/upload", Handler: "example/internal/http.(*Handle).Multipart-fm"},
	}, index)
	want := []fieldSchema{{Name: "projectId", Required: true}, {Name: "name"}, {Name: "flagA"}, {Name: "flagB"}, {Name: "ruleKey"}}
	for _, key := range []string{"POST /form", "POST /upload"} {
		ep := got[key]
		if ep.Reason != "" || !reflect.DeepEqual(ep.Fields, want) {
			t.Fatalf("%s 钩子读取的键必须并入字段表：%+v", key, ep)
		}
	}
}

// 钩子里的动态键（attr:<id> 之类）不可静态枚举：只有字面量进字段表，不编造。
func TestBindFormHookDynamicKeysAreNotInvented(t *testing.T) {
	root := writeSources(t, map[string]string{
		"dto/request.go": `package req
import (
 "net/url"
 "strings"
)
type SaveReq struct { ID string ` + "`json:\"id\" form:\"id\"`" + ` }
func (r *SaveReq) BindForm(values url.Values) error {
 for key := range values {
  if strings.HasPrefix(key, "attr:") { _ = values[key] }
 }
 return nil
}
`,
		"http/handle.go": `package handler
import (
 req "example/internal/dto"
 "github.com/gin-gonic/gin"
)
type Handle struct{}
func (h *Handle) Save(c *gin.Context) { req := &req.SaveReq{}; _ = req; c.ShouldBindJSON(req) }
`,
	})
	index, err := scanSources(root, "example/internal")
	if err != nil {
		t.Fatal(err)
	}
	// 直接断言提取结果：动态 key 不产生字段。
	if keys := hookFieldKeys("example/internal/dto.SaveReq", index); len(keys) != 0 {
		t.Fatalf("动态键不得被当成字段名：%v", keys)
	}
}
