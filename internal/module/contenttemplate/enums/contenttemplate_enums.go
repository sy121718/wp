package contenttemplateenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	ErrDraftVersionConflict = "ErrDraftVersionConflict"
	MsgCreateSuccess        = "MsgCreateSuccess"
	MsgUpdateSuccess        = "MsgUpdateSuccess"
	MsgListSuccess          = "MsgListSuccess"
	MsgDetailSuccess        = "MsgDetailSuccess"

	ErrInvalidParam = "ErrInvalidParam"
	ErrNotFound     = "ErrNotFound"

	ErrTemplateInUse = "ErrTemplateInUse"

	ErrStructureTemplateInUse = "ErrStructureTemplateInUse"
	ErrInvalidType            = "ErrInvalidType"
	ErrDataInvalid            = "ErrDataInvalid"

	ErrFieldBindingInvalid = "ErrFieldBindingInvalid"

	ErrProjectRequired = "ErrProjectRequired"

	ErrProjectNotFound = "ErrProjectNotFound"
)

var errFallbacks = map[string]string{
	ErrDraftVersionConflict:   "草稿版本已更新，请刷新后重试",
	ErrInvalidParam:           "参数不合法：请检查名称、类型与文档后重试。",
	ErrNotFound:               "模板不存在，可能已被删除。",
	ErrTemplateInUse:          "这套模板仍被自动发布实例引用，不能删除。",
	ErrStructureTemplateInUse: "这张结构模板仍被其它模板绑定为结构槽位，先到那里解绑再删。",
	ErrInvalidType:            "不支持的内容类型。",
	ErrDataInvalid:            "模板文档格式非法：请回到工作台重新保存后再试。",
	ErrFieldBindingInvalid:    "模板里的字段绑定越界：只能用本模板数据源内的字段。",
	ErrProjectRequired:        "请先选择站点工程。",
	ErrProjectNotFound:        "选择的站点工程不存在。",
}

func Err(key string) error {
	err := facing.New(key, errFallbacks[key])
	if key == ErrNotFound {
		return facing.WithStatus(err, http.StatusNotFound)
	}
	return err
}

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

func FallbackFor(key string) string {
	return errFallbacks[key]
}

const (
	ImpactLoadFailed = "admin.content.templates.impact.loadFailed"

	Heading = "admin.content.templates.heading"
)

func DraftVersionConflict() error { return facing.WithStatus(Err(ErrDraftVersionConflict), 409) }
