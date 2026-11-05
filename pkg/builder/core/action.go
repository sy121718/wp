package core

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/url"
	"strings"
)

type ActionKind string

const (
	ActionPage         ActionKind = "page"
	ActionPresentation ActionKind = "presentation"
	ActionLocal        ActionKind = "local"
	ActionInstance     ActionKind = "instance"
	ActionExternal     ActionKind = "external"
)

type ActionTarget struct {
	Kind       ActionKind `json:"kind"`
	PageID     string     `json:"pageId,omitempty"`
	InstanceID string     `json:"instanceId,omitempty"`
	NodeID     string     `json:"nodeId,omitempty"`
	PartKey    string     `json:"partKey,omitempty"`
	URL        string     `json:"url,omitempty"`
	NewTab     bool       `json:"newTab,omitzero"`
}

func actionFields(kind ActionKind) []string {
	switch kind {
	case ActionPage:
		return []string{"pageId"}
	case ActionPresentation:
		return []string{"instanceId"}
	case ActionLocal:
		return []string{"partKey"}
	case ActionInstance:
		return []string{"nodeId", "partKey"}
	case ActionExternal:
		return []string{"url", "newTab"}
	default:
		return nil
	}
}

func (a *ActionTarget) UnmarshalJSON(data []byte) error {
	type target ActionTarget
	var decoded target
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	allowed := map[string]bool{"kind": true}
	for _, key := range actionFields(decoded.Kind) {
		allowed[key] = true
	}
	for key := range fields {
		if !allowed[key] {
			return fmt.Errorf("动作目标字段%s不适用于类型%s", key, decoded.Kind)
		}
	}
	*a = ActionTarget(decoded)
	return nil
}

func ValidateActionTarget(a ActionTarget) error {
	fields := actionFields(a.Kind)
	if fields == nil {
		return fmt.Errorf("未知动作类型%s", a.Kind)
	}
	allowed := map[string]bool{}
	for _, key := range fields {
		allowed[key] = true
	}
	values := map[string]string{"pageId": a.PageID, "instanceId": a.InstanceID, "nodeId": a.NodeID, "partKey": a.PartKey, "url": a.URL}
	for key, value := range values {
		if strings.TrimSpace(value) != "" && !allowed[key] {
			return fmt.Errorf("动作目标字段%s不适用于类型%s", key, a.Kind)
		}
	}
	if a.NewTab && !allowed["newTab"] {
		return fmt.Errorf("动作类型%s不能设置newTab", a.Kind)
	}
	for _, key := range fields {
		if key != "newTab" && strings.TrimSpace(values[key]) == "" {
			return fmt.Errorf("%w: 动作%s缺少%s", ErrIncompleteNode, a.Kind, key)
		}
	}
	if a.Kind == ActionExternal {
		u, err := url.Parse(a.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return fmt.Errorf("动作外链必须为完整的HTTP或HTTPS地址")
		}
	}
	if allowed["partKey"] && !validActionKey(a.PartKey) {
		return fmt.Errorf("动作partKey必须为已声明的区域名")
	}
	return nil
}

func validActionKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for i, c := range key {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '-' || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

type ActionSpec struct {
	Key              string
	AllowedKinds     []ActionKind
	TargetCapability string
}

type ActionsProvider interface {
	Actions() []ActionSpec
	RequiredActions() []string
}

type PartSpec struct {
	Key          string
	Capabilities []string
}
type PartsProvider interface{ Parts() []PartSpec }

type ResolvedAction struct {
	Kind       ActionKind
	URL        string
	NewTab     bool
	InstanceID string
	PartKey    string
}
