package builder

import (
	"errors"
	"fmt"
)

var ErrPageDocumentEmpty = errors.New("页面文档为空")

type PageSettingsError struct {
	Err error
}

func (e *PageSettingsError) Error() string {
	if e == nil || e.Err == nil {
		return "页面设置: "
	}
	return "页面设置: " + e.Err.Error()
}

func (e *PageSettingsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type NodeDepthError struct {
	Index int
	Depth int
	Max   int
}

func (e *NodeDepthError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("顶级节点 %d: 组件树深度 %d 超过上限 %d（嵌套失控，请简化结构）", e.Index, e.Depth, e.Max)
}

type NodeInvalidError struct {
	Index int
	Err   error
}

func (e *NodeInvalidError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("顶级节点 %d: %v", e.Index, e.Err)
}

func (e *NodeInvalidError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
