package blockmcp

// block_write_tools.go — 全局块的建 / 改 / 删。
//
// 与 page 写工具同一条边界：**不碰块内的 AST**。块的内容由可视化编辑器维护
// （块树、槽位绑定、引用关系），不是一段 JSON 参数能交代清楚的。
// 所以这三个工具只处理块的**元数据**（名称、类型、分类、复用模式）；
// 新建时给一个能被编辑器打开的空块，重命名与改分类走 update。
//
// 两处后果必须写进描述与回执：
//   - global 块被页面引用时，删除默认被拒（ErrBlockInUse）；
//   - Force=true 会强行删除，被引用的页面**退化成没有这个块**并标记为待重建。
//
// UpdateReq 是**整树覆盖**（编辑器语义）：name / kind / document 必须给全，
// category 与 reuseMode 留空表示保留原值。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/mcp"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/internal/permission"
)

// blockKinds 是创建时允许的类型。
//
// 与元数据工具无关的取舍：这里的 kind 决定块在编辑器里的分类，（docs/02-D §4）
// 但**具体有哪些 kind 由 registry 决定**，硬编码一份会与组件清单漂移。
// 所以默认值走服务端（留空 = block），工具只要求调用方说明用途时用 category 表达。
const (
	blockReuseGlobal   = "global"
	blockReuseTemplate = "template"
)

// BlockListArgs 列块。
type BlockListArgs struct {
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind,omitempty"`
	Category  string `json:"category,omitempty"`
	ReuseMode string `json:"reuseMode,omitempty"`
}

type BlockIDArgs struct {
	ProjectID string `json:"projectId"`
	BlockID   string `json:"blockId"`
}

type BlockDeleteArgs struct {
	ProjectID string `json:"projectId"`
	BlockID   string `json:"blockId"`
	// Force 强制删除被引用的块。危险：被引用的页面会退化成没有这个块并标待重建。
	Force bool `json:"force,omitempty"`
}

// BlockCreateArgs 建块（只给元数据）。
type BlockCreateArgs struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Category  string `json:"category,omitempty"`
	ReuseMode string `json:"reuseMode,omitempty"`
}

// BlockUpdateArgs 改块的元数据。
//
// 服务端的 UpdateReq 是整树覆盖：`name` 与 `kind` 必填。
// 工具层先读现状再合并 —— 调用方只说「改个名字」时不该把 kind 抹掉。
type BlockUpdateArgs struct {
	ProjectID string `json:"projectId"`
	BlockID   string `json:"blockId"`
	Name      string `json:"name,omitempty"`
	Category  string `json:"category,omitempty"`
	ReuseMode string `json:"reuseMode,omitempty"`
}

type blockResult struct {
	Text string
	ID   string
	Name string
}

// QueryTools 只读工具。
func QueryTools(reader blockcontract.BlockReader) ([]mcp.Tool, error) {
	if reader == nil {
		return nil, fmt.Errorf("块读取器不能为空")
	}

	list := mcp.New("block_list", "查全局块",
		"列出本站的全局块（页眉、页脚、公告条这类跨页面复用的组件）。\n"+
			"`reuseMode` 有两种：`global` 全站共享（改一次所有引用页都变）、"+
			"`template` 只作为模板供插入时复制一份（改它不影响已插入的页面）。\n"+
			"这两种模式的实际后果完全不同，看到块先确认它是哪一种。",
		permission.BlockList,
		mcp.Object("查全局块", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"kind":      mcp.String("按块类型筛，可省略"),
			"category":  mcp.String("按分类筛，可省略"),
			"reuseMode": mcp.Enum("按复用模式筛，可省略", blockReuseGlobal, blockReuseTemplate),
		}, "projectId"),
		func(ctx context.Context, args BlockListArgs) (mcp.Result, error) {
			projectID := strings.TrimSpace(args.ProjectID)
			if projectID == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 不能为空"}
			}
			res, err := reader.List(ctx, &blockdto.ListReq{
				ProjectID: projectID,
				Kind:      strings.TrimSpace(args.Kind),
				Category:  strings.TrimSpace(args.Category),
				ReuseMode: strings.TrimSpace(args.ReuseMode),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: blockListText(res)}, nil
		})

	get := mcp.New("block_get", "看一个全局块的详情",
		"取回块的元数据与文档大小。**改块之前先跑这一步** —— 更新是整树覆盖，\n"+
			"调用方不说 name 与 kind 时工具会拿它合并回原值，读一次才能保证合并的是最新状态。",
		permission.BlockDetail,
		mcp.Object("取块详情", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"blockId":   mcp.String("块 id（从 block_list 拿）"),
		}, "projectId", "blockId"),
		func(ctx context.Context, args BlockIDArgs) (mcp.Result, error) {
			res, err := reader.Detail(ctx, &blockdto.DetailReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				ID:        strings.TrimSpace(args.BlockID),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: blockDetailText(res)}, nil
		})

	return []mcp.Tool{list, get}, nil
}

// WriteTools 写工具。
func WriteTools(w blockcontract.BlockWriter, r blockcontract.BlockReader, store mcp.IdempotencyStore) ([]mcp.Tool, error) {
	if w == nil {
		return nil, fmt.Errorf("块写入器不能为空")
	}
	if r == nil {
		return nil, fmt.Errorf("块读取器不能为空")
	}
	if store == nil {
		return nil, fmt.Errorf("幂等存储不能为空")
	}

	create := mcp.NewWrite("block_create", "新建全局块",
		"建一个**空块**（只有名称、分类和复用模式）。块里放什么由可视化编辑器来做 ——\n"+
			"这个工具不接收、也不生成块结构。\n"+
			"`reuseMode` 决定这个块以后怎么被用，建完就很难改（有引用时不能从 global 改成 template）：\n"+
			"* `global`（默认）：全站共享的同一个块，改它所有引用页一起变。页眉、页脚、公告条用这个。\n"+
			"* `template`：只是供人插入时**复制一份**的模板，改它不影响已插入的页面。\n"+
			"大多数「页眉/页脚/公告」的场景是 global。",
		permission.BlockCreate,
		mcp.Object("新建全局块", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"name":      mcp.String("块名称（给人看的，例如「站点页脚」）"),
			"category":  mcp.String("自由分类，默认 general。只能用小写字母、数字、下划线和连字符"),
			"reuseMode": mcp.Enum("复用模式：global 全站共享 / template 插入时复制一份。默认 global",
				blockReuseGlobal, blockReuseTemplate),
		}, "projectId", "name"),
		store,
		func(ctx context.Context, args BlockCreateArgs) (mcp.Result, error) {
			name := strings.TrimSpace(args.Name)
			if name == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "name 不能为空"}
			}
			res, err := w.Create(ctx, &blockdto.CreateReq{
				ProjectID: strings.TrimSpace(args.ProjectID),
				Name:      name,
				Category:  strings.TrimSpace(args.Category),
				ReuseMode: strings.TrimSpace(args.ReuseMode),
				Document:  emptyBlockDocument,
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "块已创建：\n" + blockDetailText(res) +
				"\n**现在还没有内容** —— 去可视化编辑器里放块。\n" +
				reuseModeNote(res.ReuseMode)}, nil
		})

	update := mcp.NewWrite("block_update", "改全局块的元数据",
		"改块的名称、分类或复用模式。**不碰块内的结构** —— 那部分只能在可视化编辑器里改。\n"+
			"调用方不传 name 时工具会先读一遍现状把原值合并回去（服务端的更新是整树覆盖，\n"+
			"缺 name 会被判为非法）。\n"+
			"**复用模式只能从 global 改成 template，且要求当前没有任何页面引用它** —— "+
			"有引用时会报「块正在被使用」。改分类不影响引用。",
		permission.BlockUpdate,
		mcp.Object("改块元数据", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"blockId":   mcp.String("块 id"),
			"name":      mcp.String("新名称，省略 = 不改"),
			"category":  mcp.String("新分类，省略 = 保留原值"),
			"reuseMode": mcp.Enum("新复用模式，省略 = 保留原值", blockReuseGlobal, blockReuseTemplate),
		}, "projectId", "blockId"),
		store,
		func(ctx context.Context, args BlockUpdateArgs) (mcp.Result, error) {
			projectID := strings.TrimSpace(args.ProjectID)
			blockID := strings.TrimSpace(args.BlockID)
			if blockID == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "blockId 不能为空"}
			}
			cur, err := r.Detail(ctx, &blockdto.DetailReq{ProjectID: projectID, ID: blockID})
			if err != nil {
				return mcp.Result{}, err
			}
			req := &blockdto.UpdateReq{
				ID:        blockID,
				Name:      firstNonEmpty(strings.TrimSpace(args.Name), cur.Name),
				Kind:      cur.Kind,
				Category:  strings.TrimSpace(args.Category),
				ReuseMode: strings.TrimSpace(args.ReuseMode),
				Document:  cur.Document,
			}
			if req.Name == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "这个块没有名称，请显式给出 name"}
			}
			res, err := w.Update(ctx, req)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: "块已更新：\n" + blockDetailText(res)}, nil
		})

	del := mcp.NewWrite("block_delete", "删除全局块",
		"删除一个全局块。**默认拒绝删除正被页面引用的块**（服务端会报「块正在被使用」）。\n"+
			"`force=true` 会强行删除，那时**引用它的页面会退化成没有这个块**，并被标记为待重建 ——\n"+
			"前台看到的可能是少了一截的页面，直到重新发布。\n"+
			"被引用时正确做法通常是：先改块的内容（页眉页脚这类块本来就是拿来改的），\n"+
			"或者先解除页面上的引用。只有确认没人用了才删。",
		permission.BlockDelete,
		mcp.Object("删除全局块", map[string]mcp.Schema{
			"projectId": mcp.String("工程 id"),
			"blockId":   mcp.String("要删除的块 id"),
			"force":     mcp.Boolean("true = 即使正被页面引用也强制删除（那些页面会少掉这个块并标待重建）。默认 false"),
		}, "projectId", "blockId"),
		store,
		func(ctx context.Context, args BlockDeleteArgs) (mcp.Result, error) {
			blockID := strings.TrimSpace(args.BlockID)
			if blockID == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "blockId 不能为空"}
			}
			projectID := strings.TrimSpace(args.ProjectID)
			name := ""
			if cur, err := r.Detail(ctx, &blockdto.DetailReq{ProjectID: projectID, ID: blockID}); err == nil {
				name = cur.Name
			}
			_ = projectID
			if err := w.Delete(ctx, &blockdto.DeleteReq{ID: blockID, Force: args.Force}); err != nil {
				return mcp.Result{}, err
			}
			text := fmt.Sprintf("块「%s」（id=%s）已删除。", emptyAsDash(name), blockID)
			if args.Force {
				text += "\n**这是强制删除** —— 之前引用它的页面已经少掉这个块并标记为待重建，需要重新发布才会更新前台。"
			}
			return mcp.Result{Text: text}, nil
		})

	return []mcp.Tool{create, update, del}, nil
}

// emptyBlockDocument 空块文档。
//
// 形状与页面文档同源（builder.Page）：`settings` 是对象、`root` 是数组。
// 写成 `{"root":{...}}` 会在解析阶段失败，而工具侧只看得到「执行失败」四个字。
var emptyBlockDocument = json.RawMessage(`{"settings":{},"root":[]}`)

func blockListText(res []blockdto.BlockResp) string {
	if len(res) == 0 {
		return "本站还没有全局块。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个块：\n", len(res))
	for i := range res {
		blk := &res[i]
		fmt.Fprintf(&b, "%d. id=%s「%s」· 类型 %s · 分类 %s · %s\n",
			i+1, blk.ID, emptyAsDash(blk.Name), emptyAsDash(blk.Kind),
			emptyAsDash(blk.Category), reuseModeText(blk.ReuseMode))
	}
	return b.String()
}

func blockDetailText(blk *blockdto.BlockResp) string {
	if blk == nil {
		return "没有这个块。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "* 名称：%s\n", emptyAsDash(blk.Name))
	fmt.Fprintf(&b, "* id=%s，工程 %s\n", blk.ID, blk.ProjectID)
	fmt.Fprintf(&b, "* 类型：%s，分类：%s\n", emptyAsDash(blk.Kind), emptyAsDash(blk.Category))
	fmt.Fprintf(&b, "* %s\n", reuseModeText(blk.ReuseMode))
	fmt.Fprintf(&b, "* 文档：%d 字节（结构由编辑器维护）\n", len(blk.Document))
	fmt.Fprintf(&b, "* 更新于 %s", blk.UpdatedAt.String())
	return b.String()
}

// reuseModeText 复用模式的后果 —— 这两个词光看名字分不出区别。
func reuseModeText(mode string) string {
	switch strings.TrimSpace(mode) {
	case blockReuseGlobal:
		return "复用模式 global（全站共享同一个块，改它所有引用页一起变）"
	case blockReuseTemplate:
		return "复用模式 template（插入时复制一份，改它不影响已插入的页面）"
	}
	return "复用模式 " + emptyAsDash(mode)
}

func reuseModeNote(mode string) string {
	if strings.TrimSpace(mode) == blockReuseTemplate {
		return "它是 template 模式：别的页面插入时会复制一份，之后改这个块不会影响那些页面。"
	}
	return "它是 global 模式：**改这个块的内容，所有引用它的页面都会跟着变**，改完记得重新发布。"
}

func emptyAsDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
