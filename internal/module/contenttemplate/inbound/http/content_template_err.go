package contenttemplatehttp

import (
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	view "go_wp/pkg/view"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
)

const contentTemplateErrScene = "content_template"

var contentTemplateErrInternalFallback = adminui.InternalMsg().Fallback

type contentTemplateMsg = facing.Msg

var contentTemplateFacingFallbacks = map[string]string{
	contenttemplateenums.ErrInvalidParam: "参数不完整，请检查工程、模板名与实体类型。",
	contenttemplateenums.ErrNotFound:     "这套模板不存在，可能已被删除。",

	contenttemplateenums.ErrTemplateInUse:          "这套模板仍被页面或自动发布实例引用，不能删除（引用方见列表的「引用」列）。",
	contenttemplateenums.ErrStructureTemplateInUse: "这套结构模板仍被其它模板绑定为页眉 / 页脚，不能删除（受影响的模板已记入服务日志）。",
	contenttemplateenums.ErrInvalidType:            "不支持的内容类型。",
	contenttemplateenums.ErrDataInvalid:            "模板文档格式非法：请回到工作台重新保存后再试。",
	contenttemplateenums.ErrFieldBindingInvalid:    "模板里的字段绑定越界：请回到工作台改用本模板数据源内的字段。",
	contenttemplateenums.ErrProjectRequired:        "站点里有多个工程，请显式选择这套模板所属的工程。",
	contenttemplateenums.ErrProjectNotFound:        "选择的站点工程不存在，请刷新后重试。",
}

func contentTemplateFacingFallback(key string) string {
	return contentTemplateFacingFallbacks[strings.TrimSpace(key)]
}

func contentTemplateErrMsg(c *gin.Context, err error) contentTemplateMsg {
	if err == nil {
		return contentTemplateMsg{}
	}
	fact := adminui.PageErrorFact(c, contentTemplateErrScene, err)
	if !fact.Business {

		return contentTemplateMsg{Key: fact.Key, Fallback: fact.Fallback}
	}
	fallback := contentTemplateFacingFallback(fact.Key)
	if fallback == "" {
		fallback = fact.Fallback
	}
	return contentTemplateMsg{Key: fact.Key, Fallback: fallback}
}

func contentTemplateErrText(c *gin.Context, err error) string {
	return response.MsgText(c, contentTemplateErrMsg(c, err))
}

func contentTemplateInternalText(c *gin.Context, err error) string {
	if err == nil {
		return response.MsgText(c, adminui.InternalMsg())
	}
	return adminui.ErrorFactText(c, adminui.PageErrorFact(c, contentTemplateErrScene, err))
}

func sampleErrText(c *gin.Context, hint string, err error) string {
	if h := strings.TrimSpace(hint); h != "" {
		return h
	}
	if err == nil {
		return ""
	}
	return contentTemplateInternalText(c, err)
}

const (
	contentTemplateMissingIDText      = "缺少模板 id"
	contentTemplateNotFoundText       = "模板不存在"
	contentTemplateSampleMissingText  = "缺少预览样例实体"
	contentTemplateHintProductNoMod   = "商品模块未装配，无法自动选取预览样例"
	contentTemplateHintContentNoMod   = "内容模块未装配，无法自动选取预览样例"
	contentTemplateHintNoProduct      = "工程内还没有商品，无法预览商品详情模板"
	contentTemplateHintNoArticle      = "工程内还没有文章，无法预览文章详情模板"
	contentTemplateHintEntityTypeMiss = "暂不支持该实体类型的预览样例自动选取（本页只支持商品与文章）"

	contentTemplateActivateDoneText = "已切换生效模板，引用它的页面与实例会重新构建。"

	contentTemplateImpactUnavailableText = "引用反查能力未装配：本页无法列出引用这套模板的页面与实例，删除前请人工确认。"
)

var (
	contentTemplateMissingIDMsg     = contentTemplateMsg{Fallback: contentTemplateMissingIDText}
	contentTemplateNotFoundMsg      = contentTemplateMsg{Fallback: contentTemplateNotFoundText}
	contentTemplateSampleMissingMsg = contentTemplateMsg{Fallback: contentTemplateSampleMissingText}
)

const (
	contentTemplateImpactLoadFailedKey      = contenttemplateenums.ImpactLoadFailed
	contentTemplateImpactLoadFailedFallback = "本次没取到引用面（工程列表未读到），删除前请人工确认。"
)

func contentTemplateImpactLoadFailedText(c *gin.Context) string {
	return response.MsgText(c, contentTemplateImpactLoadFailedMsg)
}

var contentTemplateImpactLoadFailedMsg = contentTemplateMsg{
	Key: contentTemplateImpactLoadFailedKey, Fallback: contentTemplateImpactLoadFailedFallback,
}

const contentTemplateImpactUnparsableTemplate = "有 %d 份文档无法解析，影响面可能不完整（这些文档仍可能引用本模板）。"

func contentTemplateJump(c *gin.Context, ok bool, msg contentTemplateMsg) {
	// 回跳由发起页面声明；这里只交结果事实。
	if ok {
		view.SuccessWith(c, view.SuccessFact{Key: msg.Key, Fallback: msg.Fallback, Args: msg.Args})
		return
	}
	view.FailMessage(c, contentTemplateErrScene, msg.Key, msg.Fallback, msg.Args...)
}
