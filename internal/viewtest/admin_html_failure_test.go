package viewtest

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CloudyKit/jet/v6"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"
	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/view"
)

func failureEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../templates", true)
	engine.Use(view.FailureMiddleware(view.RenderFailure))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	return engine
}

func TestHTMLFailureBeforeSecurityMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name       string
		middleware gin.HandlerFunc
		status     int
	}{
		{"auth", builtin.SessionAuthMiddleware(), http.StatusUnauthorized},
		{"csrf", builtin.CSRFMiddleware(), http.StatusForbidden},
		{"body", builtin.RequestBodyLimitMiddleware(2, 2), http.StatusRequestEntityTooLarge},
		{"panic", builtin.Recovery(), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := failureEngine()
			engine.Use(tc.middleware)
			engine.POST("/ui/:id", view.HTMLFragment, func(c *gin.Context) {
				if tc.name == "panic" {
					panic("SQLSTATE secret")
				}
				t.Fatal("提前拒绝后不应进入业务处理器")
			})
			req := httptest.NewRequest(http.MethodPost, "/ui/123", strings.NewReader("body"))
			req.Header.Set("Accept", "application/json")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			body := rec.Body.String()
			if rec.Code != tc.status || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, `data-ui-error`) {
				t.Fatalf("%s 失败协议错误：%d %s", tc.name, rec.Code, body)
			}
			for _, leak := range []string{"<html", "<form", "SQLSTATE", "secret", "HX-Redirect"} {
				if strings.Contains(body, leak) {
					t.Fatalf("局部错误泄漏或夹带整页：%s", body)
				}
			}
		})
	}
}

func TestUnknownLengthBodyLimitFailureProtocol(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		enums.ErrRequestEntityTooLarge: {"zh-CN": "请求内容超过大小限制"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	for _, protocol := range []string{"fragment", "page", "json"} {
		for _, encoding := range []string{"form", "json", "multipart"} {
			for _, oversized := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/oversized=%t", protocol, encoding, oversized), func(t *testing.T) {
					engine := failureEngine()
					engine.Use(builtin.RequestBodyLimitMiddleware(512, 512))
					reached := false
					handlers := []gin.HandlerFunc{}
					if protocol == "fragment" {
						handlers = append(handlers, view.HTMLFragment)
					} else if protocol == "page" {
						handlers = append(handlers, view.HTMLPage)
					}
					handlers = append(handlers, func(c *gin.Context) {
						var input struct {
							Title string `form:"title" json:"title"`
						}
						var binder binding.Binding = binding.FormPost
						if encoding == "json" {
							binder = binding.JSON
						} else if encoding == "multipart" {
							binder = binding.FormMultipart
						}
						if err := c.ShouldBindWith(&input, binder); err != nil {
							err = fmt.Errorf("private-binding-context: %w", err)
							if protocol == "json" {
								response.ErrorStatusAuto(c, "body-limit", err)
							} else {
								view.FailScene(c, "body-limit", err)
							}
							return
						}
						reached = true
						c.Status(http.StatusNoContent)
					})
					engine.POST("/write", handlers...)
					title := "ok"
					if oversized {
						title = strings.Repeat("x", 2048)
					}
					body, contentType := "title="+title, "application/x-www-form-urlencoded"
					if encoding == "json" {
						body, contentType = `{"title":"`+title+`"}`, "application/json"
					} else if encoding == "multipart" {
						var buf bytes.Buffer
						writer := multipart.NewWriter(&buf)
						if err := writer.WriteField("title", title); err != nil {
							t.Fatal(err)
						}
						if err := writer.Close(); err != nil {
							t.Fatal(err)
						}
						body, contentType = buf.String(), writer.FormDataContentType()
					}
					req := httptest.NewRequest(http.MethodPost, "/write", strings.NewReader(body))
					req.ContentLength = -1
					req.Header.Set("Content-Type", contentType)
					req.Header.Set("Accept", "application/json")
					if protocol == "json" {
						req.Header.Set("Accept", "text/html")
						req.Header.Set("HX-Request", "true")
					}
					rec := httptest.NewRecorder()
					engine.ServeHTTP(rec, req)
					if !oversized {
						if rec.Code != http.StatusNoContent || !reached {
							t.Fatalf("限额内的原生绑定失败：%d %s", rec.Code, rec.Body.String())
						}
						return
					}
					out := rec.Body.String()
					if rec.Code != http.StatusRequestEntityTooLarge || reached || !strings.Contains(out, "请求内容超过大小限制") {
						t.Fatalf("未知长度超限未归口为413：%d %s reached=%t", rec.Code, out, reached)
					}
					wantType := "text/html"
					if protocol == "json" {
						wantType = "application/json"
					} else if !strings.Contains(out, "data-ui-error") || (protocol == "page") != strings.Contains(out, "<!DOCTYPE html>") {
						t.Fatalf("HTML 失败没有遵循页面/片段职责：%s", out)
					}
					if !strings.HasPrefix(rec.Header().Get("Content-Type"), wantType) {
						t.Fatalf("客户端头改变响应协议：%s", rec.Header().Get("Content-Type"))
					}
					for _, leak := range []string{"private-binding-context", "http: request body too large", enums.ErrRequestEntityTooLarge} {
						if strings.Contains(out, leak) {
							t.Fatalf("请求体错误泄漏：%s", out)
						}
					}
				})
			}
		}
	}
}

func TestHTMLFailureRateLimitAndPageProtocol(t *testing.T) {
	engine := failureEngine()
	engine.Use(builtin.RequestRateLimitMiddleware(1, time.Hour))
	engine.GET("/page", view.HTMLPage, func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/page", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "<!DOCTYPE html>") || !strings.Contains(rec.Body.String(), "data-ui-error") {
		t.Fatalf("页面限流没有返回完整错误页：%d %s", rec.Code, rec.Body.String())
	}
}

// TestHTMLFailureLanguageAndEscaping 守住两件事：
// 主消息只播报公共一句（编码明细不上屏），以及位置参数按请求语言安全转义且并发不串语言。
func TestHTMLFailureLanguageAndEscaping(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		"product.err.conflict": {"zh-CN": "发生冲突：%s", "en-US": "Conflict occurred: %s"},
		"product.err.encoded":  {"zh-CN": "发生冲突", "en-US": "Conflict occurred"},
		"product.err.detail":   {"zh-CN": "编码 {code} 重复", "en-US": "Duplicate {code}"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	engine := failureEngine()
	engine.GET("/failure", view.HTMLFragment, func(c *gin.Context) {
		view.FailScene(c, "product", facing.WithStatus(facing.New("product.err.conflict|<script>alert(1)</script>"), http.StatusConflict))
	})
	engine.GET("/encoded", view.HTMLFragment, func(c *gin.Context) {
		err := fmt.Errorf("%w: %s", facing.New("product.err.encoded"), i18n.ErrorDetail("product.err.detail", "code", `<script>alert(1)</script>`))
		view.FailScene(c, "product", facing.WithStatus(err, http.StatusConflict))
	})

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		for _, tc := range []struct{ lang, want, other, encoded string }{
			{"zh-CN", "发生冲突：&lt;script&gt;alert(1)&lt;/script&gt;", "Conflict occurred", "编码 <script>alert(1)</script> 重复"},
			{"en-US", "Conflict occurred: &lt;script&gt;alert(1)&lt;/script&gt;", "发生冲突", "Duplicate <script>alert(1)</script>"},
		} {
			wg.Add(1)
			go func(lang, want, other, encoded string) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/failure", nil)
				req.Header.Set("Accept-Language", lang)
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				body := rec.Body.String()
				if rec.Code != http.StatusConflict || !strings.Contains(body, want) || strings.Contains(body, other) || strings.Contains(body, "<script>") || strings.Contains(body, "product.err.") {
					t.Errorf("%s 语言/转义错误：%d %s", lang, rec.Code, body)
				}

				// 编码明细是数据，不是主消息：HTML 不展示它，也不留隐藏 JSON/DOM 解释器。
				rec = httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				encodedBody := rec.Body.String()
				for _, leak := range []string{encoded, "product.err.", "<script>", "data-details", "<script type=\"application/json\">"} {
					if strings.Contains(encodedBody, leak) {
						t.Errorf("%s 编码明细上屏：%q\n%s", lang, leak, encodedBody)
					}
				}
			}(tc.lang, tc.want, tc.other, tc.encoded)
		}
	}
	wg.Wait()
}

func TestHTMLTemplateFailureDoesNotLeakPartialOrRecurse(t *testing.T) {
	for _, brokenErrorTemplate := range []bool{false, true} {
		engine := failureEngine()
		if brokenErrorTemplate {
			loader := jet.NewInMemLoader()
			loader.Set("bad.html", `partial-secret{{.Missing}}`)
			engine.HTMLRender = templates.NewJetHTMLRenderWithSet(jet.NewSet(loader))
		}
		engine.GET("/broken", view.HTMLFragment, func(c *gin.Context) {
			if brokenErrorTemplate {
				c.HTML(http.StatusOK, "bad.html", struct{}{})
				return
			}
			c.HTML(http.StatusOK, "private-missing-template.html", struct{}{})
		})
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/broken", nil))
		body := rec.Body.String()
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("模板失败应为500：%d %s", rec.Code, body)
		}
		for _, leak := range []string{"partial-secret", "private-missing", "bad.html", "Missing", "SQLSTATE"} {
			if strings.Contains(body, leak) {
				t.Fatalf("模板错误泄漏：%s", body)
			}
		}
		if !brokenErrorTemplate && strings.Count(body, "data-ui-error") != 1 {
			t.Fatalf("错误模板重复或遗漏：%s", body)
		}
	}
}

// TestHTMLFailureStatusAndPositionArgs 覆盖绑定错误归口、位置参数及格式损坏时的安全回退。
func TestHTMLFailureStatusAndPositionArgs(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		"sysErrBadParam":  {"en-US": "Invalid input"},
		"uiInternalErr":   {"en-US": "Internal failure"},
		"test.err.limit":  {"en-US": "Maximum %s; received %s"},
		"test.err.broken": {"en-US": "Wrong %d"},
		"test.err.plain":  {"en-US": "Conflict"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	for _, tc := range []struct {
		name   string
		status int
		err    error
		want   string
	}{
		{"binding", 400, errors.New("private binding error SQLSTATE"), "Invalid input"},
		{"internal", 500, errors.New("private database error SQLSTATE"), "Internal failure"},
		{"args", 400, facing.New("test.err.limit|200|201"), "Maximum 200; received 201"},
		// 明文 tail 不上屏：主消息只播报公共一句。
		{"plain-detail", 409, fmt.Errorf("%w: summer-sale", facing.New("test.err.plain")), "Conflict"},
		{"plain-escaping", 409, fmt.Errorf("%w: <script>&", facing.New("test.err.plain")), "Conflict"},
		{"wrapped-diagnostic", 409, fmt.Errorf("private SQLSTATE: %w", facing.New("test.err.plain")), "Conflict"},
		{"missing-plain", 500, fmt.Errorf("%w: private SQLSTATE", facing.New("test.err.absent")), "Internal failure"},
		{"malformed-detail", 409, fmt.Errorf("%w: %s", facing.New("test.err.plain"), i18n.ErrDetailSep), "Conflict"},
		{"escaping", 400, facing.New("test.err.limit|<script>|&\""), "Maximum &lt;script&gt;; received &amp;&#34;"},
		{"missing", 400, facing.New("test.err.absent|200|201"), "Internal failure"},
		{"few-args", 400, facing.New("test.err.limit|200"), "Internal failure"},
		{"broken-format", 400, facing.New("test.err.broken|200"), "Internal failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := failureEngine()
			engine.GET("/failure", view.HTMLFragment, func(c *gin.Context) { view.FailStatus(c, tc.status, "failure", tc.err) })
			req := httptest.NewRequest(http.MethodGet, "/failure", nil)
			req.Header.Set("Accept-Language", "en-US")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			body := rec.Body.String()
			if rec.Code != tc.status || !strings.Contains(body, "<p>"+tc.want+"</p>") || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("状态或受控文案不符：%d %s", rec.Code, body)
			}
			for _, leak := range []string{"test.err.", "private", "SQLSTATE", "%!", "<script>"} {
				if strings.Contains(body, leak) {
					t.Fatalf("错误出口泄漏 %q：%s", leak, body)
				}
			}
		})
	}
}

func TestHTMLFailureDoesNotRewriteStartedStream(t *testing.T) {
	engine := failureEngine()
	engine.GET("/stream", view.HTMLFragment, func(c *gin.Context) {
		c.String(http.StatusOK, "stream")
		view.FailScene(c, "stream", errors.New("late failure"))
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "stream" {
		t.Fatalf("已写流被覆盖：%d %s", rec.Code, rec.Body.String())
	}
}
