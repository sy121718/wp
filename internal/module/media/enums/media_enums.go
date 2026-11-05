package mediaenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	ErrAttachmentNotFound = "ErrAttachmentNotFound"
	ErrUploadFailed       = "ErrUploadFailed"
	ErrUploadEmpty        = "ErrUploadEmpty"

	ErrAttachmentNotImage      = "ErrAttachmentNotImage"
	ErrVariantStorageNotLocal  = "ErrVariantStorageNotLocal"
	ErrDownloadFailed          = "ErrDownloadFailed"
	ErrDownloadEmpty           = "ErrDownloadEmpty"
	ErrDownloadStorageNotLocal = "ErrDownloadStorageNotLocal"

	ErrReplaceFailed        = "ErrReplaceFailed"
	ErrReplaceExtMismatch   = "ErrReplaceExtMismatch"
	ErrAttachmentReferenced = "ErrAttachmentReferenced"

	ErrReplaceStaleNotifyFailed = "ErrReplaceStaleNotifyFailed"
)

// 文件分类（file category）相关的业务错误。
//
// 为什么是 ErrFileCategory* 而不是 ErrCategory*：product 域已经有 ErrCategory* 一族
// （商品分类），词库的 (item_key, lang) 是全局唯一键 —— 用同名值就等于两个模块共享
// 同一句文案的所有权，product 改一次措辞 media 的媒体库页面会跟着变，而且没人知道
// 为什么。media 这边管的是另一张表（file_categories），所以 key 也自成一套、与表名
// 对齐，天然不撞。
//
// 它们此前是 media.go 里的 errors.New("中文串")：既不进词库、也不带状态码，出口只能
// 落到 pkg/response 的第③档当成内部错误。中文串本身是给开发看的内部提示语，不是
// 给用户的成品句 —— 用户文案归词库。
const (
	ErrFileCategoryNameRequired   = "ErrFileCategoryNameRequired"
	ErrFileCategoryNotFound       = "ErrFileCategoryNotFound"
	ErrFileCategoryParentNotFound = "ErrFileCategoryParentNotFound"
	ErrFileCategoryNameDuplicated = "ErrFileCategoryNameDuplicated"
	ErrFileCategoryHasChildren    = "ErrFileCategoryHasChildren"

	// ErrFileCategoryCycle 覆盖「父级不能是自己」与「不能移动到自己的子分类下」两种
	// 情形：前者是环的特例、后者是一般形，同一个失败原因不该有两个 key。
	ErrFileCategoryCycle = "ErrFileCategoryCycle"
)

// ErrMediaCursorInvalid 是列表翻页游标坏了：客户端传了个解析不出来的游标，
// 换一页 / 重新进列表就能过，属于参数问题而不是我们这边出错。
const ErrMediaCursorInvalid = "ErrMediaCursorInvalid"

const (
	MsgSuccess    = "MsgSuccess"
	MsgBadRequest = "MsgBadRequest"
)

const (
	PackageTitle              = "admin.media.package.title"
	PackageDir                = "admin.media.package.dir"
	PackageVariantType        = "admin.media.package.variantType"
	PackageStatus             = "admin.media.package.status"
	PackageVariantUnavailable = "admin.media.package.variantUnavailable"
	PackageRegenHint          = "admin.media.package.regenHint"

	PackageOriginalPathInvalid = "admin.media.package.originalPathInvalid"
	PackageOriginalMissing     = "admin.media.package.originalMissing"

	PackageReadFailed = "admin.media.package.readFailed"

	VariantStatusMissing = "admin.media.variantStatus.missing"

	Heading = "admin.media.heading"
)

// StatusOf 声明每个 media 业务错误对应的 HTTP 状态码。
//
// 为什么状态码写在这里而不是读词库的 http_code：词库是**文案**的事实源，状态码是
// **协议**的事实源。声明在 Go 里，出口只认这一份。
//
//	400 参数不对，改输入再来
//	404 记录不存在（附件 / 分类）
//	409 与既有数据冲突，要先解除引用才能继续
//	500 我们这边出错，用户改什么都没用
//
// 未登记的 key 返回 0，表示「这个 key 没声明状态」，由出口按归口处理 —— 注意
// pkg/response.ErrorStatus 的第二档会把「业务事实但无状态」判成 400，所以凡是
// 语义上不是 400 的业务错误都必须在这里登记，否则会静默降级成 400。
func StatusOf(key string) int {
	switch key {
	case ErrAttachmentNotFound:
		return http.StatusNotFound

	case ErrAttachmentReferenced:
		return http.StatusConflict

	case ErrUploadEmpty, ErrAttachmentNotImage,
		ErrVariantStorageNotLocal, ErrDownloadEmpty, ErrDownloadStorageNotLocal,
		ErrReplaceExtMismatch, ErrFileCategoryNameRequired, ErrFileCategoryCycle:
		return http.StatusBadRequest

	case ErrMediaCursorInvalid:
		return http.StatusBadRequest

	// 文件分类「不存在」与附件「不存在」同类：引用了库里没有的行，换一个 id 就能过。
	case ErrFileCategoryNotFound, ErrFileCategoryParentNotFound:
		return http.StatusNotFound

	// 与库里既有数据冲突：同名同父、或分类下仍有子级。重试同一个请求永远不会成功，
	// 要先改数据（改名 / 先搬走子级），所以是 409 而不是 400。
	case ErrFileCategoryNameDuplicated, ErrFileCategoryHasChildren:
		return http.StatusConflict

	// 这三个的成因都是存储 / IO 失败（见 media.go 里包在它们外面的 err.Error()），
	// 用户改输入改不了任何东西，所以是 500 而不是 400。
	case ErrUploadFailed, ErrDownloadFailed, ErrReplaceFailed, ErrReplaceStaleNotifyFailed:
		return http.StatusInternalServerError
	}
	return 0
}

// Err 构造业务错误，并把声明里的状态码一起带上。
//
// 状态码是错误事实的一部分，不写在调用点 —— 之前 handler 用
// response.ErrorAuto(c, http.StatusNotFound, "media", err) 把状态码钉在分支上，
// 同一个 scene 下 404/400/500 混着来。现在状态在声明处给，出口对它只调
// response.ErrorStatusAuto。
func Err(key string) error {
	err := facing.New(key)
	if status := StatusOf(key); status != 0 {
		return facing.WithStatus(err, status)
	}
	return err
}

// ErrDetail 在业务错误后面挂一段**对外明细**（Tail），同时保住声明状态。
//
// 为什么不用 fmt.Errorf("%s: %w", key, err)：那种写法在 pkg/facing 里会走纯文本
// 分支 —— FactOf 的 Fallback 会退成 key 本身，StatusOf 更是直接认不出来（包装层
// 没有实现 Source/Statused），于是 ErrorStatus 落到「业务事实但无状态 → 400」。
// 声明成 409/500 的错误一旦这样包装就会静默退回 400，正是本轮要消灭的那种漂移。
func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	base := fmt.Errorf("%w: %s", Err(key), detail)
	if status := StatusOf(key); status != 0 {
		return facing.WithStatus(base, status)
	}
	return base
}
