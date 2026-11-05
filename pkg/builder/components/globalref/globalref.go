package globalref

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.globalref"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("globalref", globalrefTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) PropsSpec() any { return &globalrefProps{} }

type globalrefProps struct {
	BlockID string `json:"blockId" ct:"string,maxlen=64,sec=content,label=全局块 ID"`
}

func decode(node *core.Node) (p globalrefProps, err error) {
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return p, fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if strings.TrimSpace(p.BlockID) == "" {
		return p, fmt.Errorf("节点 %s: blockId 不能为空", node.ID)
	}
	return p, nil
}

func BlockIDOf(node *core.Node) (string, error) {
	p, err := decode(node)
	if err != nil {
		return "", err
	}
	return p.BlockID, nil
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 全局块引用为叶子节点，不允许子节点", node.ID)
	}
	if _, err = decode(node); err != nil {
		return err
	}
	return nil
}

//go:embed globalref.jet
var globalrefTemplate string
