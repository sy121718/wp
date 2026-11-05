package feature

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mediahttp "go_wp/internal/module/media/inbound/http"
	mediamodel "go_wp/internal/module/media/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 本文件钉住「状态码由 enums 声明、handler 只调 ErrorStatusAuto」这条契约的真实出口。
//
// 改造前 media_handle.go 每个失败分支各写一个状态码：Detail/Delete/References 一律 404，
// GenerateVariants/Download/DownloadBatch 一律 400，Upload 一律 500 —— 同一 scene 下
// 404/400/500 混着来，分类权在 handler 手里。现在这些分支都改调
// response.ErrorStatusAuto，状态取自 mediaenums.StatusOf，所以下面是**对外契约**的断言：
// 改动任何一个声明都会让这里红，这是有意的。

func newMediaStatusRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db, svc := newMediaService(t)
	handle := mediahttp.NewHandle(svc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	g := router.Group("/api/media")
	{
		// 逐条对齐 media_router.go 的 SetupMediaRoutes：这里去掉权限中间件（测试的
		// 关注点是状态码而不是授权），但**路径与 handler 必须一条不少** —— 漏注册
		// 会让请求打到 Gin 的 NoRoute，拿到一个看起来像业务 404 的「404 page not found」。
		g.GET("/detail", handle.Detail)
		g.POST("/delete", handle.Delete)
		g.GET("/references", handle.References)
		g.GET("/download", handle.Download)
		g.GET("/download/batch", handle.DownloadBatch)
		g.POST("/variants/generate", handle.GenerateVariants)
	}
	return router, db
}

// mediaStatusOf 解出响应体里的 code，与真实 HTTP 状态码一起返回。
func mediaStatusOf(t *testing.T, w *httptest.ResponseRecorder) (status int, code int, message string) {
	t.Helper()
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &payload)
	return w.Code, payload.Code, payload.Message
}

// TestMediaNotFoundReturns404 附件不存在 → 404（而不是 400 或 500）。
func TestMediaNotFoundReturns404(t *testing.T) {
	router, _ := newMediaStatusRouter(t)
	const missing = "999999"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"detail", http.MethodGet, "/api/media/detail?projectId=p1&id=" + missing, ""},
		{"delete", http.MethodPost, "/api/media/delete", `{"projectId":"p1","id":` + missing + `}`},
		{"references", http.MethodGet, "/api/media/references?id=" + missing, ""},
		{"download", http.MethodGet, "/api/media/download?id=" + missing, ""},
		{"variants", http.MethodPost, "/api/media/variants/generate", `{"id":` + missing + `}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			var req *http.Request
			if c.body == "" {
				req = httptest.NewRequest(c.method, c.path, nil)
			} else {
				req = httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(w, req)

			status, code, message := mediaStatusOf(t, w)
			if status != http.StatusNotFound {
				t.Fatalf("%s 附件不存在应 404，实际 status=%d body=%s", c.name, status, w.Body.String())
			}
			if code != http.StatusNotFound {
				t.Errorf("%s 响应体 code 应与 HTTP 状态同值，实际 %d", c.name, code)
			}
			// 出口取词：缺词时给归口文案，不给裸 key、不给内部原文。
			if message == "" {
				t.Errorf("%s 不应是空 message", c.name)
			}
			if strings.Contains(message, "ErrAttachmentNotFound") {
				t.Errorf("%s 把裸 key 当 message 交给前端了：%q", c.name, message)
			}
			if strings.Contains(message, "SQLSTATE") || strings.Contains(message, "gorm") {
				t.Errorf("%s message 里带了内部原文：%q", c.name, message)
			}
		})
	}
}

// TestMediaDeleteReferencedReturns409 仍被引用的附件 → 409，不是 404。
//
// 这是本轮最容易回归的一条：pkg/response.ErrorStatus 的第二档会把「业务事实但无
// 状态」判成 400，所以 ErrAttachmentReferenced 必须在 enums 里显式声明 409。
func TestMediaDeleteReferencedReturns409(t *testing.T) {
	router, db := newMediaStatusRouter(t)

	attID := seedAttachment(t, db, nil, "ref-target.png", "image", "image/png")
	// 直接按附件表真实的版本字段写一处页面引用（extra_info->refs），
	// 走的是创建时同一条 JSON 形状，不做"近似"构造。
	added, aerr := mediamodel.NewAttachmentModel(db).AddRef(t.Context(), attID,
		mediamodel.AttachmentRef{Kind: "page", ID: "page-1", Title: "首页"})
	if aerr != nil {
		t.Fatalf("写入引用失败：%v", aerr)
	}
	if !added {
		t.Fatalf("写入引用失败：AddRef 没有落到 extra_info->refs")
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/media/delete",
		strings.NewReader(`{"projectId":"p1","id":`+strconv.FormatUint(attID, 10)+`}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	status, code, message := mediaStatusOf(t, w)
	if status != http.StatusConflict {
		t.Fatalf("被引用的附件删除应 409，实际 status=%d body=%s", status, w.Body.String())
	}
	if code != http.StatusConflict {
		t.Errorf("响应体 code 应为 409，实际 %d", code)
	}
	if strings.Contains(message, "ErrAttachmentReferenced") {
		t.Errorf("把裸 key 当 message：%q", message)
	}
	// 附件仍在：409 是拒绝，不是半途删掉。
	var n int64
	if err := db.Model(&mediamodel.AttachmentEntity{}).Where("id = ?", attID).Count(&n).Error; err != nil {
		t.Fatalf("回查附件失败: %v", err)
	}
	if n != 1 {
		t.Errorf("409 之后附件应仍存在，实际 %d 行", n)
	}
}

// TestMediaParamErrorsStay400 参数不合法仍然是 400 —— 改造不能把这一类也带走。
func TestMediaParamErrorsStay400(t *testing.T) {
	router, _ := newMediaStatusRouter(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"detail 缺 id", http.MethodGet, "/api/media/detail", ""},
		{"delete 缺 id", http.MethodPost, "/api/media/delete", `{"projectId":"p1"}`},
		{"references 非数字 id", http.MethodGet, "/api/media/references?id=abc", ""},
		{"download 空 ids", http.MethodGet, "/api/media/download/batch?ids=", ""},
		{"variants 缺 id", http.MethodPost, "/api/media/variants/generate", `{}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			var req *http.Request
			if c.body == "" {
				req = httptest.NewRequest(c.method, c.path, nil)
			} else {
				req = httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(w, req)
			status, code, _ := mediaStatusOf(t, w)
			if status != http.StatusBadRequest {
				t.Fatalf("%s 应 400，实际 status=%d body=%s", c.name, status, w.Body.String())
			}
			if code != http.StatusBadRequest {
				t.Errorf("%s 响应体 code 应为 400，实际 %d", c.name, code)
			}
		})
	}
}
