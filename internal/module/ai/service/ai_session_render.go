// ai_session_render.go — 把模型的展示指令（ui_render 的 spec）变成可渲染的视图。
//
// 为什么这一步在**会话层**而不在工具里：工具拿不到调用者身份（`mcp.Runner.Run` 的
// userID 只用于权限判定，不进 handler），而每个数据源都要过一次权限判定 ——
// 权限问的是「这个账号能不能读这张表」，不是「这个工具有没有这个能力」。
// 把取数放在有身份的这一层，权限、审计与结果剪枝都自然复用工具那条链。
//
// 逐块取数时会**再进一次** ToolProvider.Run（每个 source 一次）。这是刻意的：
// 一次 ui_render 里的 N 个积木 = N 次权限判定 + N 次工具执行，
// 而不是「一次性放行整张图」。模型多要一块就多判一次，代价只有一次查库。
package aiservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	aienums "go_wp/internal/module/ai/enums"
	"go_wp/internal/uispec"
	"go_wp/pkg/logger"
)

// renderMetaLimit 工具事件里 render 结构的落库上限（字节）。
//
// 超过就整块不落库（页面退化成只显示正文），而不是截断 —— 半个 JSON 反序列化必然失败，
// 存下去只会把「为什么这块没渲染」变成一个查不出来的问题。
const renderMetaLimit = 64 << 10

// uiSpecResolver 把「已注册工具」适配成 uispec 的数据源。
type uiSpecResolver struct {
	provider ToolProvider
	userID   int64
}

// Resolve 执行一次数据源取数。
//
// 参数直接按工具的 JSON Schema 传（spec 的 params 本来就是「这个数据源的参数」），
// limit 只在 > 0 时补进去 —— 补一个 0 会让「工具自己的默认值」被显式 0 覆盖掉。
func (r *uiSpecResolver) Resolve(ctx context.Context, source string, params map[string]string, limit int) (any, error) {
	if r.provider == nil {
		return nil, errors.New("没有可用的工具能力")
	}
	body := make(map[string]any, len(params)+1)
	for k, v := range params {
		body[k] = v
	}
	if limit > 0 {
		body["limit"] = limit
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	res, err := r.provider.Run(ctx, r.userID, source, string(raw))
	if err != nil {
		return nil, err
	}
	// 业务性失败（越权 / 参数不合法 / 查库失败）在这里是**没有 error 的**：
	// 装配层按契约把它们翻成文本 + 状态码返回。所以这里还要看状态码，
	// 只判 error 会把「你没有权限查订单」当成一次成功取数。
	if res.Status != aienums.ToolCallStatusOK {
		return nil, errors.New(res.Text)
	}
	if res.Data == nil {
		return nil, fmt.Errorf("数据源 %s 没有可渲染的结构", source)
	}
	return res.Data, nil
}

// renderSpec 逐块取数并返回可渲染的视图。
//
// 不返回 error：单块失败不该让整条回答失败（uispec.Execute 的同一取舍），
// 调用方按 len(views) 判断「用户这一轮到底有没有东西看」。
func (s *SessionService) renderSpec(ctx context.Context, sessionID, userID int64, spec *uispec.Spec) []uispec.View {
	views, notes := uispec.Execute(ctx, spec, &uiSpecResolver{provider: s.tools, userID: userID})
	for _, n := range notes {
		if n.Kind == uispec.NoteOK {
			continue
		}
		// 失败原因只进日志：它可能带表名、参数与内部路径，而这段结构最终会进事件的 meta。
		logger.Scene("ai").With("session", sessionID).With("source", n.Source).
			With("kind", n.Kind).Warn("展示积木未渲染：" + n.Msg)
	}
	return views
}
