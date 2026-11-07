package blockdto

import (
	"net/url"
	"unicode/utf8"

	blockenums "go_wp/internal/module/block/enums"
	json "go_wp/pkg/jsonx"
)

type CreateReq struct {
	ProjectID string          `json:"projectId" form:"projectId" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Kind      string          `json:"kind" form:"kind"`
	Category  string          `json:"category" form:"category"`
	ReuseMode string          `json:"reuseMode" form:"reuseMode"`
	Document  json.RawMessage `json:"document" form:"-" binding:"excluded_with=PresetKey"`

	PresetKey     string `json:"presetKey,omitempty" form:"presetKey" binding:"required_with=PresetVersion,max=100"`
	PresetVersion int    `json:"presetVersion,omitempty" form:"presetVersion" binding:"required_with=PresetKey,excluded_without=PresetKey,omitempty,min=1,max=2147483647"`
}

func (r *CreateReq) Validate() error {
	if r.PresetKey == "" {
		if r.PresetVersion == 0 {
			return nil
		}
	} else if utf8.RuneCountInString(r.PresetKey) <= 100 && r.PresetVersion > 0 && r.PresetVersion <= 2147483647 && len(r.Document) == 0 {
		return nil
	}
	return blockenums.Err(blockenums.ErrBlockPresetInvalid)
}

func (r *CreateReq) BindForm(values url.Values) error {
	if documents, present := values["document"]; present {
		if r.PresetKey != "" || len(documents) != 1 {
			return blockenums.Err(blockenums.ErrBlockPresetInvalid)
		}
		r.Document = json.RawMessage(documents[0])
	}
	return r.Validate()
}

type UpdateReq struct {
	ID        string          `json:"id" form:"id" binding:"required"`
	Name      string          `json:"name" form:"name" binding:"required,max=100"`
	Kind      string          `json:"kind" form:"kind"`
	Category  string          `json:"category" form:"category"`
	ReuseMode string          `json:"reuseMode" form:"reuseMode"`
	Document  json.RawMessage `json:"document" form:"document"`

	ExpectedVersion uint64 `json:"expectedVersion" form:"expectedVersion" binding:"required,min=1,max=9223372036854775807"`
}

type SaveContentReq struct {
	ID       string          `json:"id" form:"id" binding:"required"`
	Name     string          `json:"name" form:"name"`
	Document json.RawMessage `json:"document" form:"document"`

	ExpectedVersion uint64 `json:"expectedVersion" form:"expectedVersion" binding:"required,min=1,max=9223372036854775807"`
}

type DetailReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        string `form:"id" json:"id" binding:"required"`
}

type ListReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required"`
	Kind      string `json:"kind" form:"kind"`
	Category  string `json:"category" form:"category"`
	ReuseMode string `json:"reuseMode" form:"reuseMode"`
}

type DeleteReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

type BulkDeleteReq struct {
	IDs []string `json:"ids" form:"ids" binding:"required,min=1,dive,required"`
}

type CloneReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}

// 页面表单只声明普通控件能表达的字段；文档内容走复杂编辑器的明确 JSON 输入。
// 字段名与 blocks.html / 工作台预设对话框的控件一一对应。

type CreateForm struct {
	ProjectID     string `form:"projectId" binding:"required"`
	Name          string `form:"name" binding:"required,max=100"`
	Kind          string `form:"kind"`
	Category      string `form:"category"`
	ReuseMode     string `form:"reuseMode"`
	PresetKey     string `form:"presetKey" binding:"required_with=PresetVersion,max=100"`
	PresetVersion string `form:"presetVersion" binding:"required_with=PresetKey"`
}

type DeleteForm struct {
	ID string `form:"id" binding:"required"`
}

type BulkDeleteForm struct {
	IDs []string `form:"ids"`
}
