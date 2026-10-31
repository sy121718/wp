package artifactenums

import "go_wp/pkg/facing"

const (
	ErrArtifactNotFound = "ErrArtifactNotFound"
	ErrArtifactMismatch = "ErrArtifactMismatch"
	ErrInvalidArtifact  = "ErrInvalidArtifact"

	ErrInvalidParam = "ErrInvalidParam"

	ErrContentRefQueryFailed = "ErrContentRefQueryFailed"

	ErrContentObjectDeleteNotApplied = "ErrContentObjectDeleteNotApplied"
)

const (
	MsgArtifactSaved = "MsgArtifactSaved"
	MsgArtifactFound = "MsgArtifactFound"
)

var errFallbacks = map[string]string{
	ErrArtifactNotFound:              "构建产物不存在",
	ErrArtifactMismatch:              "产物内容与数据库记录不一致",
	ErrInvalidArtifact:               "构建产物不完整",
	ErrInvalidParam:                  "请求参数无效",
	ErrContentRefQueryFailed:         "内容对象引用关系查询失败，为避免误删共享对象已中止本次回收",
	ErrContentObjectDeleteNotApplied: "内容对象删除未生效（删除后复查仍存在），请稍后重试或联系管理员",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
