package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/response"
)

// 两个主错误 key 的短语固定在本用例，同时校验迁移 seed 与之一致：
// 只跟读词库会让「seed 写错但两端一致」也绿。
var productBlockedErrorWording = map[string]map[string]string{
	productenums.ErrBulkRejected: {
		"zh-CN": "批量操作被拒绝，请按列出的原因逐条处理后重试",
		"en-US": "The batch operation was rejected. Fix the listed items and try again.",
	},
	productenums.ErrVariantStockDuplicated: {
		"zh-CN": "库存清单里同一仓库同一 SKU 出现了多行，请合并后再保存",
		"en-US": "The same warehouse and SKU appear more than once in the stock list. Merge those rows and save again.",
	},
}

func TestProductBlockedErrorSeedsMatchFixedWording(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	for key, langs := range productBlockedErrorWording {
		for lang, want := range langs {
			var rows []struct {
				Value string `gorm:"column:value"`
			}
			if err := f.db.Table("sys_i18n").
				Select("item_value AS value").
				Where("item_key = ? AND lang = ? AND status = 1", key, lang).
				Scan(&rows).Error; err != nil {
				t.Fatalf("读词条 %s/%s 失败：%v", key, lang, err)
			}
			if len(rows) != 1 || rows[0].Value != want {
				t.Fatalf("%s 的 %s 词条应等于迁移 seed 704 的原文，实际 %v", key, lang, rows)
			}
		}
	}
}

// 走共享 JSON 出口：词库 → ErrorStatusAuto → {code,message}，语言按 Accept-Language。
func TestProductBlockedErrorsRenderBothLanguages(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	loadFeatureWords(t, f.db)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	for key := range productBlockedErrorWording {
		k := key
		engine.GET("/blocked/"+k, func(c *gin.Context) {
			response.ErrorStatusAuto(c, "product-blocked-words", productenums.Err(k))
		})
	}

	for _, tc := range []struct {
		key    string
		status int
	}{
		{key: productenums.ErrBulkRejected, status: http.StatusConflict},
		{key: productenums.ErrVariantStockDuplicated, status: http.StatusBadRequest},
	} {
		for _, lang := range []string{"zh-CN", "en-US"} {
			t.Run(tc.key+"/"+lang, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/blocked/"+tc.key, nil)
				req.Header.Set("Accept-Language", lang)
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)

				if rec.Code != tc.status {
					t.Fatalf("状态码 = %d，want %d（%s）", rec.Code, tc.status, rec.Body.String())
				}
				env := decodeJSONEnvelope(t, rec)
				want := productBlockedErrorWording[tc.key][lang]
				if env.Message != want {
					t.Fatalf("%s 的 %s 文案应精确等于词库那句\n got: %q\nwant: %q", tc.key, lang, env.Message, want)
				}
				if strings.Contains(env.Message, tc.key) {
					t.Fatalf("对外文案不该出现裸 key，实际 %q", env.Message)
				}
				assertNoProductInternalLeak(t, "JSON 回执", env.Message)
			})
		}
	}
}
