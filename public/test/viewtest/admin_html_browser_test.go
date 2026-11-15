package viewtest

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"
	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// 浏览器夹具使用固定测试令牌；不装配生产会话、数据库或外部服务。
type browserCSRFStore struct{}

func (browserCSRFStore) Get(*gin.Context) string         { return "browser-csrf" }
func (browserCSRFStore) Save(*gin.Context, string) error { return nil }

type browserInput struct {
	Title         string                `form:"title" json:"title" binding:"required"`
	Tags          []string              `form:"tags" json:"tags"`
	Enabled       bool                  `form:"enabled" json:"enabled"`
	Unchecked     bool                  `form:"unchecked" json:"unchecked"`
	DisabledValue string                `form:"disabled_value" json:"disabled_value"`
	Quantity      int                   `form:"quantity" json:"quantity"`
	Intent        string                `form:"intent" json:"intent"`
	External      string                `form:"external" json:"external"`
	File          *multipart.FileHeader `form:"file" json:"-"`
	Rows          []struct {
		ID       string `json:"id"`
		Quantity int    `json:"quantity"`
	} `json:"rows"`
}

type browserSaved struct {
	browserInput
	ID       string
	FileName string
	FileBody string
}

func htmlBrowserEngine() *gin.Engine {
	engine := failureEngine()
	engine.GET("/static/js/*name", func(c *gin.Context) {
		source, err := templates.StaticJS(c.Param("name")[1:])
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(source))
	})
	engine.GET("/static/css/ui.css", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(templates.UICSS()))
	})
	engine.GET("/", view.HTMLPage, func(c *gin.Context) {
		text.AttachScope(c)
		c.HTML(http.StatusOK, "testdata/html-ui/page.html", nil)
	})
	engine.GET("/fragment", view.HTMLFragment, func(c *gin.Context) {
		text.AttachScope(c)
		c.HTML(http.StatusOK, "testdata/html-ui/saved.html", browserSaved{browserInput: browserInput{Title: "read"}})
	})
	engine.GET("/drawer", view.HTMLFragment, func(c *gin.Context) {
		text.AttachScope(c)
		c.HTML(http.StatusOK, "testdata/html-ui/drawer.html", nil)
	})
	engine.GET("/denied", view.HTMLFragment, builtin.SessionAuthMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	write := engine.Group("/write", builtin.RequestBodyLimitMiddleware(1024, 4096), builtin.CSRFMiddlewareWith(browserCSRFStore{}))
	for _, encoding := range []string{"form", "upload", "document"} {
		write.POST("/"+encoding, view.HTMLFragment, func(c *gin.Context) {
			var input browserInput
			var binder binding.Binding = binding.FormPost
			if encoding == "upload" {
				binder = binding.FormMultipart
			} else if encoding == "document" {
				binder = binding.JSON
			}
			if err := c.ShouldBindWith(&input, binder); err != nil {
				view.FailStatus(c, http.StatusUnprocessableEntity, "browser-bind", err)
				return
			}
			saved := browserSaved{browserInput: input}
			if encoding == "upload" {
				if input.File == nil {
					view.FailStatus(c, http.StatusUnprocessableEntity, "browser-upload", facing.New("fixture.err.invalid"))
					return
				}
				file, err := input.File.Open()
				if err != nil {
					view.FailScene(c, "browser-upload", err)
					return
				}
				defer file.Close()
				content, err := io.ReadAll(file)
				if err != nil {
					view.FailScene(c, "browser-upload", err)
					return
				}
				saved.FileName, saved.FileBody = input.File.Filename, string(content)
			} else if encoding == "document" {
				if len(input.Rows) != 1 {
					view.FailStatus(c, http.StatusUnprocessableEntity, "browser-document", facing.New("fixture.err.invalid"))
					return
				}
				saved.ID, saved.Quantity = input.Rows[0].ID, input.Rows[0].Quantity
			}
			text.AttachScope(c)
			c.HTML(http.StatusOK, "testdata/html-ui/saved.html", saved)
		})
	}
	write.POST("/reject", view.HTMLFragment, func(c *gin.Context) {
		view.FailStatus(c, http.StatusUnprocessableEntity, "browser-reject", facing.New("fixture.err.invalid"))
	})
	write.POST("/broken", view.HTMLFragment, func(c *gin.Context) {
		text.AttachScope(c)
		c.HTML(http.StatusOK, "testdata/html-ui/broken.html", struct{}{})
	})
	return engine
}

func TestHTMLUIRealBrowser(t *testing.T) {
	if os.Getenv("GOWP_HTML_UI_BROWSER") != "1" {
		t.Skip("设置 GOWP_HTML_UI_BROWSER=1 运行真实 Gin/Jet/Chrome 组合验收")
	}
	i18n.InjectForTest(map[string]map[string]string{
		"fixture.title":                {"zh-CN": "HTML 交互验收", "en-US": "HTML interaction check"},
		"fixture.save":                 {"zh-CN": "保存", "en-US": "Save"},
		"fixture.err.invalid":          {"zh-CN": "输入需要修正", "en-US": "Correct the input"},
		enums.ErrRequestEntityTooLarge: {"zh-CN": "请求内容超过大小限制", "en-US": "Request body is too large"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	server := httptest.NewServer(htmlBrowserEngine())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "../../../internal/templates/testdata/html-ui/browser.cjs")
	cmd.Env = append(os.Environ(), "GOWP_HTML_UI_BASE_URL="+server.URL, "GOWP_PUPPETEER_MODULE="+browserPuppeteerModule())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实浏览器组合验收失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)
}

// browserPuppeteerModule 让 browser.cjs 解析到本机安装的 puppeteer-core：
// 优先 GOWP_PUPPETEER_MODULE，否则回落到桌面端 profile 下的依赖。
func browserPuppeteerModule() string {
	if v := os.Getenv("GOWP_PUPPETEER_MODULE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "puppeteer-core"
	}
	return filepath.Join(home, ".dsh", "profiles", "desktop", "node_modules", "puppeteer-core")
}
