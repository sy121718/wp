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
	for _, name := range []string{"Auto", "Query", "URI", "Unknown", "Mixed", "Callback", "Missing", "Save.func1"} {
		t.Run(name, func(t *testing.T) {
			r := route{Method: "POST", Path: "/x", Handler: "example/internal/http.(*Handle)." + name + "-fm"}
			ep := buildContracts([]route{r}, index)["POST /x"]
			if ep.Protocol != "unknown" || ep.Reason == "" || len(ep.Fields) != 0 {
				t.Fatalf("无法确认的请求必须明确未检查：%+v", ep)
			}
		})
	}
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

	// 只跟一层：二层包装不展开，避免把无关调用链误并进来。
	wrapped := got["POST /wrapped"]
	if wrapped.Protocol != "unknown" || !strings.Contains(wrapped.Reason, "没有显式 body binder") {
		t.Fatalf("二层包装不应被静默展开：%+v", wrapped)
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
