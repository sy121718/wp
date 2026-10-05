package projectmcp

// project_tools.go — 站点工程的可调用工具。
//
// 为什么必须有这一个：其它模块的工具（orders_* / product_* / media_* / content_*）
// 全部以 projectId 为必填入参，而在**概览页**这类跨工程的页面上，用户看不到
// 也说不清当前有哪些工程 —— 实测模型会这样收场：「按口径该用 order_find 按单号找，
// 但这两个工具都必须带 projectId，你现在在 /admin，我拿不到当前是哪个工程，不能代填」。
// 回答本身没错，但用户要的答案没拿到，而缺的只是一份「站点里有哪些工程」的清单。
//
// 只读：依赖收窄到 ProjectLister（一条 List），手里没有创建 / 删除 / 改配置的能力。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/permission"
)

// ProjectLister 工具层需要的最小工程能力（一条只读方法）。
type ProjectLister interface {
	List(ctx context.Context) (res []projectdto.ProjectResp, err error)
}

// Tools 返回工程模块的工具集。
func Tools(projects ProjectLister) ([]mcp.Tool, error) {
	if projects == nil {
		return nil, errors.New("projectmcp: 站点工程依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{siteProjects(projects)}, nil
}

// siteProjectsArgs 无入参（保留结构体是为了与其它工具同形，便于将来加筛选）。
type siteProjectsArgs struct{}

func siteProjects(projects ProjectLister) mcp.Tool {
	return mcp.New("site_projects", "站点工程清单",
		"列出这个后台下的全部站点工程（id 与名称）。\n"+
			"**几乎所有按工程查数的工具都需要 projectId，而这个工具是拿到它的入口**："+
			"用户在概览页 / 仪表盘这类看不出工程的页面上提问时，先调它，\n"+
			"再用工程名与用户说的店名对上号（只有一个工程时那就是它，不必反问用户）。\n"+
			"返回的 id 直接用作其它工具的 projectId 参数。",
		permission.ProjectList,
		mcp.Object("站点工程清单（无参数）", map[string]mcp.Schema{}, []string{}...),
		func(ctx context.Context, _ siteProjectsArgs) (mcp.Result, error) {
			list, err := projects.List(ctx)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: siteProjectsText(list), Data: list}, nil
		})
}

// siteProjectsText 把工程清单写成模型能直接引用的几句。
func siteProjectsText(list []projectdto.ProjectResp) string {
	if len(list) == 0 {
		return "站点里还没有建工程。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "站点共有 %d 个工程：\n", len(list))
	for i, p := range list {
		// 域名写在名字后面（settings 里的 siteUrl 之类）：用户说的往往是店名或域名，
		// 两个都给出，模型才能把用户口中的那一个对上号。
		if host := projectHost(p); host != "" {
			fmt.Fprintf(&b, "%d. %s（id %s，域名 %s）\n", i+1, p.Name, p.ID, host)
			continue
		}
		fmt.Fprintf(&b, "%d. %s（id %s）\n", i+1, p.Name, p.ID)
	}
	if len(list) == 1 {
		b.WriteString("只有一个工程 —— 用户没有指明是哪个时，就用它。")
	} else {
		b.WriteString("多个工程时不要替用户选：用工程名 / 域名与他说的对上号，对不上就问一句。")
	}
	return strings.TrimRight(b.String(), "\n")
}

// projectHost 从工程的 settings JSON 里取站点域名（取不到回空串）。
//
// settings 是 json.RawMessage，字段名由工程模块自己定 —— 这里刻意**宽容地**试几个
// 常见键而不做严格结构体绑定：这个工具的价值是「给出一个能把工程与用户口中的店对上号
// 的标识」，键名换了不该让整个工具报错（那会让所有按工程查数的工具连锁失效）。
func projectHost(p projectdto.ProjectResp) string {
	if len(p.Settings) == 0 {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal(p.Settings, &raw); err != nil {
		return ""
	}
	for _, key := range []string{"domain", "siteUrl", "site_url", "url", "homeUrl"} {
		if v, ok := raw[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
