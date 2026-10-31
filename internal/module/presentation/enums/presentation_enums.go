package presentationenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	ErrActionTargetInUse = "presentation.err.actionTargetInUse"
	MsgCreateSuccess     = "MsgCreateSuccess"
	MsgRebuildSuccess    = "MsgRebuildSuccess"
	MsgListSuccess       = "MsgListSuccess"
	MsgDetailSuccess     = "MsgDetailSuccess"
	MsgDeleteSuccess     = "MsgDeleteSuccess"

	MsgPreviewSuccess = "MsgPreviewSuccess"

	MsgUpdateURLSuccess = "MsgUpdateURLSuccess"

	ErrInvalidParam  = "ErrInvalidParam"
	ErrNotFound      = "ErrNotFound"
	ErrNoTemplate    = "ErrNoTemplate"
	ErrEntityMissing = "ErrEntityMissing"
	ErrBuildFailed   = "ErrBuildFailed"

	ErrProjectRequired = "ErrProjectRequired"

	ErrProjectNotFound = "ErrProjectNotFound"

	ErrRegistryMissing = "ErrRegistryMissing"

	ErrTemplateTypeMismatch = "ErrTemplateTypeMismatch"

	ErrInvalidPath = "ErrInvalidPath"

	ErrSamePath = "ErrSamePath"

	ErrPathOccupied = "ErrPathOccupied"

	ErrDetachConfirmRequired = "presentation.err.detachConfirmRequired"

	ErrRollbackTargetMiss = "presentation.err.rollbackTargetMiss"

	ErrRollbackFailed = "presentation.err.rollbackFailed"

	ErrSnapshotMismatch = "presentation.err.snapshotMismatch"

	ErrInputMoved = "presentation.err.inputMoved"
)

var errFallbacks = map[string]string{
	ErrActionTargetInUse:     "这个目标仍被页面或区块引用，请先替换引用后再删除",
	ErrInvalidParam:          "参数不合法：请检查后重试。",
	ErrNotFound:              "实例不存在，可能已被删除。",
	ErrNoTemplate:            "该内容类型没有可用的模板。",
	ErrEntityMissing:         "内容实体不存在。",
	ErrBuildFailed:           "实例构建失败，请稍后重试。",
	ErrProjectRequired:       "请先选择站点工程。",
	ErrProjectNotFound:       "选择的站点工程不存在。",
	ErrRegistryMissing:       "内容实体类型未注册，无法构建。",
	ErrTemplateTypeMismatch:  "这套模板与该内容类型不匹配，请换一套。",
	ErrInvalidPath:           "访问路径不合法：必须以 / 开头，且不含空格与 .. 路径段。",
	ErrSamePath:              "新旧路径相同，没有需要修改的地方。",
	ErrPathOccupied:          "这个路径已被占用，换一个再试。",
	ErrDetachConfirmRequired: "转入独立文档需要确认（会放弃模板同步）。",
	ErrRollbackTargetMiss:    "回滚目标不存在，或产物文件已缺失。",
	ErrRollbackFailed:        "回滚激活失败，线上版本保持不变。",
	ErrSnapshotMismatch:      "目标快照不属于这个实例，不能用来回滚。",
	ErrInputMoved:            "内容刚刚又改过一次，本次发布依据的输入已过期，请重试。",
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
	StatusActive = "active"

	StatusDraft = "draft"
)

func ActionTargetInUse() error { return facing.WithStatus(Err(ErrActionTargetInUse), 409) }

func InputMoved() error { return facing.WithStatus(Err(ErrInputMoved), http.StatusConflict) }
