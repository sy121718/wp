package aihttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

// aiWriteBadInput 把原生 binder 的失败交给统一失败出口：状态 400，文案由 Jet 取词。
func aiWriteBadInput(c *gin.Context) {
	aiWriteFail(c, aienums.Err(aienums.ErrInvalidParam))
}

// aiWriteFail 是 ai 写端点的 HTML 失败出口：状态码取错误声明表（与 JSON 出口同源），
// 文案由失败模板取词。非业务错误仍由 response.ErrorStatus 归口成 500。
func aiWriteFail(c *gin.Context, err error) {
	status := response.ErrorStatus(err)
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		if declared := aienums.StatusOf(fact.Key); declared != 0 {
			status = declared
		}
	}
	pageview.FailStatus(c, status, aiErrScene, err)
}

// aiFragmentData 给写回片段补齐请求级取词与 CSRF。
func aiFragmentData(c *gin.Context, data gin.H) gin.H {
	return adminui.Prepare(c, data)
}

// aiWriteResult 渲染一条操作结论片段；句子只在 Jet 里按词条取。
func aiWriteResult(c *gin.Context, key string) {
	c.HTML(http.StatusOK, "admin/ai/ai_write_result.html", aiFragmentData(c, gin.H{
		"Key": key,
	}))
}

// aiProviderCardData 用服务端的供应商事实组装一张卡片；整页与局部写回共用同一份 Jet。
func aiProviderCardData(c *gin.Context, p aidto.Provider) gin.H {
	base := adminui.Prepare(c, gin.H{})
	csrf, _ := base["csrf_token"].(string)
	return adminui.ViewMap(cardData(aidto.ProviderCardBase{
		CSRFToken: csrf, T: base["t"], PermSet: base["PermSet"], Buttons: base["Buttons"],
	}, p, aiservice.BuiltinBaseURL(p.ProviderKey)))
}

// providerCardModels 组装模型目录区需要的字段（与 provider_card.html include 它时同形）。
func providerCardModels(c *gin.Context, p aidto.Provider) gin.H {
	card := aiProviderCardData(c, p)
	return gin.H{
		"Provider":        card["Provider"],
		"Rows":            card["Rows"],
		"ProtocolOptions": card["ProtocolOptions"],
		"DefaultBaseURL":  card["DefaultBaseURL"],
		"Buttons":         card["Buttons"],
		"csrf_token":      card["csrf_token"],
		"t":               card["t"],
		"PermSet":         card["PermSet"],
	}
}

// aiProviderCardName 是供应商卡片的整卡片段。
const aiProviderCardName = "admin/ai/provider_card.html"
