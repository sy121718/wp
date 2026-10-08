package projecthttp

// project_err.go — 项目域（主题 / 站点设置）**页面** handler 的错误归口。
//
// 形状与 internal/module/admin/inbound/http/admin_err.go 对齐，只有候选来源不同：
// admin 是手写白名单，本域**直接用 service 哨兵**——因为 project service 的哨兵消息
// 本来就是 enums 的 key（`errors.New(projectenums.ErrThemeNotFound)`），
// 于是「判定业务错误」这件事只需要一份表，JSON 出口与页面出口共用。
//
// 三件套在这里落地为：
//   · 白名单 —— service 哨兵（projectErrStatusText 的判定表）；
//   · 归口文案 —— ErrThemeInternal（主题域）/ ErrProjectInternal（站点设置域）；
//   · 结构化日志 —— 未命中判定表时记原文（带 path），响应只出归口文案。
//
// **读侧（?err= / ?ok= / ?locales_saved=1 的受控文案集合与判定）已整批删除**：
// 写动作的结论由 shell.RenderJump 渲染整页提示（见 project_jump.go），不再经查询参数
// 回带，那套「证明这条提示出自本仓」的判定（projectErrTexts / projectPageErrText）随之
// 不需要了。projectWriteTextKeys 保留下来只作**写侧 key 的登记表**（迁移登记守卫用）。

import (
	"errors"
	"net/http"

	projectenums "go_wp/internal/module/project/enums"
	service "go_wp/internal/module/project/service"
	"go_wp/internal/shell"
	"go_wp/pkg/logger"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// projectErrStatusText 把 service 返回的错误判定成 (HTTP 状态码, i18n key)。
//
// fallbackKey 是本域的**归口文案**，由调用方按域给：主题域 ErrThemeInternal、
// 站点设置域 ErrProjectInternal。归口文案回答的是「哪一处的服务出了问题」——
// 把主题的故障说成「工程服务内部错误」，排障时日志场景与页面提示就对不上了。
//
// 判定用 **service 哨兵**（errors.Is）而不是字符串形态：pkg/response 的 businessErrKey
// 那类形态判定在这里不够用 —— 它只认「像 key 的字符串」，而本域要的是
// 「这句话确实出自本域 enums」。哨兵是编译期的，抄错名字会直接报错。
//
// 返回的 key 一定来自 projectenums（或是调用方给的 fallbackKey），
// 所以调用方可以直接把它交给 r.TranslateMessage —— 页面渲染的是译文，不是裸 key。
//
// 比原先 themeError 的判定集**多认了** ErrThemeProjectRequired 与站点工程域的四个哨兵：
// 它们本来就是业务错误（"需要显式工程作用域" / "站点工程名称不能为空" 等），
// 原先落 default → 500 + 「主题服务内部错误」，等于把用户能自己修的问题说成了系统故障。
func projectErrStatusText(err error, fallbackKey string) (status int, key string) {
	switch {
	case err == nil:
		return http.StatusInternalServerError, fallbackKey

	// —— 主题域 ——
	case errors.Is(err, service.ErrThemeNotFound):
		return http.StatusNotFound, projectenums.ErrThemeNotFound
	case errors.Is(err, service.ErrThemeIsActive):
		return http.StatusBadRequest, projectenums.ErrThemeIsActive
	case errors.Is(err, service.ErrThemeNameRequired):
		return http.StatusBadRequest, projectenums.ErrThemeNameRequired
	case errors.Is(err, service.ErrThemeDuplicateName):
		return http.StatusBadRequest, projectenums.ErrThemeDuplicateName
	case errors.Is(err, service.ErrThemeProjectIDEmpty):
		return http.StatusBadRequest, projectenums.ErrThemeProjectIDEmpty
	case errors.Is(err, service.ErrInvalidThemeSettings):
		return http.StatusBadRequest, projectenums.ErrInvalidThemeSettings
	case errors.Is(err, service.ErrThemeProjectRequired):
		return http.StatusBadRequest, projectenums.ErrProjectRequired

	// —— 站点工程域 ——
	case errors.Is(err, service.ErrProjectNotFound):
		return http.StatusNotFound, projectenums.ErrProjectNotFound
	case errors.Is(err, service.ErrInvalidName):
		return http.StatusBadRequest, projectenums.ErrInvalidName
	case errors.Is(err, service.ErrInvalidSettings):
		return http.StatusBadRequest, projectenums.ErrInvalidSettings
	case errors.Is(err, service.ErrInvalidParam):
		return http.StatusBadRequest, projectenums.ErrInvalidParam
	// 站点语言准入（U1）：词条数为 0 的语言被拒绝启用 —— 用户能自己修（去补词条），
	// 属于预期内的输入问题，不该落 500 冒充系统故障。
	case errors.Is(err, service.ErrLocaleNoTranslations):
		return http.StatusBadRequest, projectenums.ErrLocaleNoTranslations

	default:
		// 基础设施故障（连接池 / 约束冲突 / 驱动原文）：key 落归口文案，原文只进日志。
		return http.StatusInternalServerError, fallbackKey
	}
}

// projectErrParam 页面路径的错误文案归口：返回**当前语言的译文**，供整页提示渲染。
//
// 为什么必须翻译：页面上的提示是直接渲染的文本（shell.RenderJump 的 Msg 不进
// response 的 translate）。不翻的话运营看到的是裸 key（ErrThemeInternal）而不是
// 「主题服务内部错误」。
//
// 判据与 themeError 的 default 分支同源：**只有落到 500 才是「预期外的故障」**，
// 业务错误（缺参 / 不存在 / 被引用拒绝）是预期内的用户输入问题，不污染错误日志。
func projectErrParam(c *gin.Context, scene, fallbackKey string, err error) string {
	status, key := projectErrStatusText(err, fallbackKey)
	if err != nil && status == http.StatusInternalServerError {
		logger.Scene(scene).With("user_id", shell.CurrentUserID(c)).
			With("path", c.Request.URL.Path).Error(err, "项目域页面操作失败")
	}
	return r.TranslateMessage(c, key)
}

// projectWriteTextKeys 写侧会渲染到页面上的全部文案 key（**迁移登记守卫的登记表**）。
//
// 写动作的结论改走响应体（shell.RenderJump 的 Msg）之后，不再有「读侧白名单」，
// 但「key 必须在 sys_i18n 里登记」这条硬约束仍在：漏登记的症状是**页面显示裸 key**
// （如 `ErrThemeIDRequired` 直接摆给运营看）——不报错、不记日志，只有人眼能发现。
// 有 project_err_i18n_test.go 的对账用例钉住。
//
// 列的是**文案 key** 而不是「哪一处调用」：同一个 key 可能被多处写侧用到
// （ErrThemeNotFound 在「GET 缺主题」与「POST 主题不存在」两条路上各出现一次）。
var projectWriteTextKeys = []string{
	// service 哨兵判定表的全部产物（projectErrStatusText 的返回值集合）
	projectenums.ErrThemeNotFound,
	projectenums.ErrThemeIsActive,
	projectenums.ErrThemeNameRequired,
	projectenums.ErrThemeDuplicateName,
	projectenums.ErrThemeProjectIDEmpty,
	projectenums.ErrInvalidThemeSettings,
	projectenums.ErrProjectRequired,
	projectenums.ErrProjectNotFound,
	projectenums.ErrLocaleNoTranslations,
	projectenums.ErrInvalidName,
	projectenums.ErrInvalidSettings,
	projectenums.ErrInvalidParam,
	// 两个域的归口文案（default 分支落到哪一个，取决于调用方给的 fallbackKey）
	projectenums.ErrThemeInternal,
	projectenums.ErrProjectInternal,
	// handler 自己的校验文案（不经 service、没有哨兵）
	projectenums.ErrThemeIDRequired,
	projectenums.MsgThemeSettingsInvalid,
	projectenums.MsgThemeSettingsRefreshFailed,
	projectenums.ErrSiteSettingsNameRequired,
	projectenums.ErrGA4IDInvalid,
	projectenums.ErrGSCVerificationInvalid,
	projectenums.ErrNotFoundHTMLTooLong,
	projectenums.ErrLangURLModeInvalid,
	// 站点运费规则的两个字段（单位元的表单值非法：负数 / 非数字 / 超上限）。
	projectenums.ErrShippingBaseFeeInvalid,
	projectenums.ErrShippingFreeThresholdInvalid,
	// 成功回执（整页提示的 OK 分支）
	projectenums.MsgSiteSettingsSaved,
	projectenums.ThemeOKCreated,
	projectenums.ThemeOKActivated,
	projectenums.ThemeOKDeleted,
	siteLocalesSavedKey,
	themeSettingsSavedTextKey,
}
