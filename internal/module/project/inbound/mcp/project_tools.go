package projectmcp

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/pkg/mcp"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
)

type ProjectLister interface {
	List(ctx context.Context) (res []projectdto.ProjectResp, err error)
}

func Tools(projects ProjectLister) ([]mcp.Tool, error) {
	if projects == nil {
		return nil, errors.New("projectmcp: 站点工程依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{siteProjects(projects)}, nil
}

type siteProjectsArgs struct{}

func siteProjects(projects ProjectLister) mcp.Tool {
	return mcp.New("site_projects", "站点工程清单",
		"列出这个后台下的全部站点工程（id 与名称）。\n"+
			"**几乎所有按工程查数的工具都需要 projectId，而这个工具是拿到它的入口**："+
			"用户在概览页 / 仪表盘这类看不出工程的页面上提问时，先调它，\n"+
			"再用工程名与用户说的店名对上号（只有一个工程时那就是它，不必反问用户）。\n"+
			"返回的 id 直接用作其它工具的 projectId 参数。",
		projectenums.ProjectList,
		mcp.Object("站点工程清单（无参数）", map[string]mcp.Schema{}, []string{}...),
		func(ctx context.Context, _ siteProjectsArgs) (mcp.Result, error) {
			list, err := projects.List(ctx)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: siteProjectsText(list), Data: list}, nil
		})
}

func siteProjectsText(list []projectdto.ProjectResp) string {
	if len(list) == 0 {
		return "站点里还没有建工程。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "站点共有 %d 个工程：\n", len(list))
	for i, p := range list {

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
