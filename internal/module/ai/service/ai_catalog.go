package aiservice

// 三条口径：
//
//	· **内置清单是随版本发布的静态预设**（按 provider_key 索引，数据在 catalog_data*.go）：内置的是「这家供应商大致有哪些模型」，
//	  不是权威清单 —— 所以「恢复默认模型」是显式动作，缺清单时明确报错而不是静默清空。
//	· **目录形状由代码白名单约束**：只认 config_data.models（元素形状见 dto.ModelEntry），
//	  解析失败一律回退（跳过坏行 / 回退空目录），写回时保留 config_data 的其它键。
//	· **拉取走 SSRF 防护口径**（ssrf.go），地址每次出站都重新校验。

// 页面上的「获取可用模型」是**两段动作**：先把候选拿出来给人看（本文件），
// 人勾完再写库（inbound 侧组装 SaveModelsReq）。拆开的原因是可撤销性 ——
// 旧行为是点一下就把整份 `/models` 并进目录，用户没有反悔的机会，
// 也没有机会把「这个中转站塞进来的一百个无关模型」挡在门外。

// 数据来源：dsh 内置的 pi-ai 供应商预置包
//
//	@earendil-works/pi-ai/dist/providers/data/*.json（.manifest.json schemaVersion 3，
//	generatedAt 2026-09-22T19:31:44.346Z）：41 家供应商、1495 个模型。
//
// 文件分工：
//
//	catalog_data.go             —— 本文件：类型 + 供应商表（展示名、协议端点、模型清单引用）
//	catalog_data_models.go      —— 中小型供应商的模型清单
//	catalog_data_models_bulk.go —— 大目录供应商（聚合平台 / 托管平台，30 个模型以上）的模型清单
//	ai_model_catalog.go         —— 只留读取与合并逻辑，不再硬编码目录数据
//
// 模型 id / 展示名 / 上下文窗口 / 最大输出 token / 输入类型逐字段原样搬运，
// **不做前缀加工**：openrouter 的 deepseek/deepseek-v4.1-flash 与 opencode 的 muse-spark-1.3-contributor
// 都是各家服务端自己的形态，客户端既不添加也不剥离。「用的是哪一家」由 provider_key 这一维承担。
//
// 协议取值：dsh 用连字符串（openai-completions …），本模块用下划线枚举，经 aienums.DSHProtocolAlias
// 翻译。未登记的 dsh 协议（bedrock-converse-stream / azure-openai-responses / google-vertex /
// mistral-conversations / openai-codex-responses / pi-messages）在本模块没有实现 → Protocol 留空，
// 由管理员在界面上自选（禁止静默回落成默认协议）。

// 数据来源：dsh 内置的 pi-ai 供应商预置包
//
//	@earendil-works/pi-ai/dist/providers/data/*.json
//	.manifest.json schemaVersion 3，generatedAt 2026-09-22T19:31:44.346Z
//
// 逐字段搬运：模型 id / 展示名 / 上下文窗口 / 最大输出 token / 输入类型原样保留，
// **不做前缀加工** —— 前缀形态是各家服务端自己要求的（openrouter 的 deepseek/deepseek-v4.1-flash
// 与 opencode 的裸 id 都是原样），客户端既不添加也不剥离。
//
// 不要手改本文件的数据行：改数据要重跑生成脚本（见 ai_model_catalog.go 文件头）。

// 数据来源：dsh 内置的 pi-ai 供应商预置包
//
//	@earendil-works/pi-ai/dist/providers/data/*.json
//	.manifest.json schemaVersion 3，generatedAt 2026-09-22T19:31:44.346Z
//
// 逐字段搬运：模型 id / 展示名 / 上下文窗口 / 最大输出 token / 输入类型原样保留，
// **不做前缀加工** —— 前缀形态是各家服务端自己要求的（openrouter 的 deepseek/deepseek-v4.1-flash
// 与 opencode 的裸 id 都是原样），客户端既不添加也不剥离。
//
// 不要手改本文件的数据行：改数据要重跑生成脚本（见 ai_model_catalog.go 文件头）。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"go_wp/internal/module/ai/dto"
	"go_wp/internal/module/ai/enums"
	"go_wp/internal/module/ai/model"
	"go_wp/pkg/logger"
)

// builtinProvider 代码内置的一家供应商：默认 API 地址 + 默认模型清单。
type builtinProvider struct {
	BaseURL string
	Models  []aidto.ModelEntry
}

// builtinProviders 内置供应商表（恢复默认模型 / API 地址占位「提供商默认」的真源）。
//
// 目录数据不再硬编码在本文件：全部来自 catalog_data*.go（41 家供应商、1495 个模型，
// 逐字段搬自 dsh 的 pi-ai 供应商预置包）。这里只把预设表压成「默认地址 + 默认模型清单」
// 的索引视图；多协议供应商的默认地址与协议取 primaryEndpoint（见 catalog_data.go），
// 协议列未实现的家（amazon-bedrock / azure-openai-responses / google-vertex / mistral /
// openai-codex / radius）地址为空，界面会要求用户自己填。
var builtinProviders = buildBuiltinProviders()

func buildBuiltinProviders() map[string]builtinProvider {
	out := make(map[string]builtinProvider, len(presetProviders))
	for _, p := range presetProviders {
		out[p.Key] = builtinProvider{BaseURL: p.primaryEndpoint().BaseURL, Models: p.Models}
	}
	return out
}

// BuiltinModels 取一家供应商的内置默认模型清单；没有登记 / 清单为空时 ok=false。
//
// 返回**副本**：调用方（含页面渲染）拿到的是可以随便改的切片，改不动内置表。
func BuiltinModels(providerKey string) (models []aidto.ModelEntry, ok bool) {
	p, found := builtinProviders[strings.ToLower(strings.TrimSpace(providerKey))]
	if !found || len(p.Models) == 0 {
		return nil, false
	}
	out := make([]aidto.ModelEntry, 0, len(p.Models))
	for _, m := range p.Models {
		types := make([]string, len(m.InputTypes))
		copy(types, m.InputTypes)
		out = append(out, aidto.ModelEntry{
			ID:              m.ID,
			DisplayName:     m.DisplayName,
			ContextWindow:   m.ContextWindow,
			MaxOutputTokens: m.MaxOutputTokens,
			InputTypes:      types,
		})
	}
	return out, true
}

// BuiltinBaseURL 取一家供应商的内置默认 API 地址；未登记返回空串（页面据此提示「请填写」）。
func BuiltinBaseURL(providerKey string) string {
	p, ok := builtinProviders[strings.ToLower(strings.TrimSpace(providerKey))]
	if !ok {
		return ""
	}
	return p.BaseURL
}

// BuiltinProviderKeys 内置供应商键的字典序列表（页面用来给「供应商标识」输入框做提示）。
func BuiltinProviderKeys() []string {
	keys := make([]string, 0, len(builtinProviders))
	for k := range builtinProviders {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseModels 从 config_data 解析模型目录。
//
// **解析失败一律回退**（返回空目录、跳过坏行），不报错：config_data 是「补充配置」，
// 坏数据不该让整个供应商打不开 —— 管理员在页面上看到空目录即可，改对了就恢复。
func parseModels(cfg aimodel.JSONMap) []aidto.ModelEntry {
	if len(cfg) == 0 {
		return nil
	}
	raw, ok := cfg["models"]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]aidto.ModelEntry, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := jsonString(row["id"])
		if id == "" {
			continue
		}
		entry := aidto.ModelEntry{
			ID:              id,
			DisplayName:     jsonString(row["display_name"]),
			ContextWindow:   jsonInt64(row["context_window"]),
			MaxOutputTokens: jsonInt64(row["max_output_tokens"]),
			InputTypes:      jsonStringSlice(row["input_types"]),
		}
		if entry.DisplayName == "" {
			entry.DisplayName = entry.ID
		}
		entry.InputTypes = normalizeInputTypes(entry.InputTypes)
		out = append(out, entry)
	}
	return out
}

// modelsToConfig 把模型目录写回 config_data：只改 models 键，其余键**原样保留**。
//
// 保留其它键是前向兼容的取舍：将来往 config_data 里加 settings 之类的补充键时，
// 旧版本代码不会把它们抹掉。往这些键里读什么，由代码白名单决定（当前只认 models）。
func modelsToConfig(cfg aimodel.JSONMap, models []aidto.ModelEntry) aimodel.JSONMap {
	out := make(aimodel.JSONMap, len(cfg)+1)
	for k, v := range cfg {
		out[k] = v
	}
	rows := make([]map[string]any, 0, len(models))
	for _, m := range models {
		rows = append(rows, map[string]any{
			"id":                m.ID,
			"display_name":      m.DisplayName,
			"context_window":    m.ContextWindow,
			"max_output_tokens": m.MaxOutputTokens,
			"input_types":       m.InputTypes,
		})
	}
	out["models"] = rows
	return out
}

// normalizeModels 归一模型目录：去空行、trim、ID 去重、输入类型白名单。
//
// 空行（用户点了「+ 添加模型」没填）**丢弃不算错**；两个行填了同一个 ID 才算错
// （静默留一行会让用户以为两个都存上了）。全部为空 = 保存空目录，是合法操作。
func normalizeModels(in []aidto.ModelEntry) ([]aidto.ModelEntry, error) {
	out := make([]aidto.ModelEntry, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, m := range in {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			return nil, errors.New(aienums.ErrModelIDDuplicated)
		}
		seen[id] = struct{}{}
		name := strings.TrimSpace(m.DisplayName)
		if name == "" {
			name = id
		}
		entry := aidto.ModelEntry{
			ID:              id,
			DisplayName:     name,
			ContextWindow:   m.ContextWindow,
			MaxOutputTokens: m.MaxOutputTokens,
			InputTypes:      normalizeInputTypes(m.InputTypes),
		}
		if entry.ContextWindow < 0 {
			entry.ContextWindow = 0
		}
		if entry.MaxOutputTokens < 0 {
			entry.MaxOutputTokens = 0
		}
		out = append(out, entry)
	}
	return out, nil
}

// normalizeInputTypes 输入类型白名单归一：只认 text / image，去重并固定顺序（text 在前）；
// 空集合回退 [text]（一个模型至少得能吃文本，否则它在目录里没有意义）。
func normalizeInputTypes(in []string) []string {
	hasText, hasImage := false, false
	for _, t := range in {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case aienums.InputTypeText:
			hasText = true
		case aienums.InputTypeImage:
			hasImage = true
		}
	}
	if !hasText && !hasImage {
		return []string{aienums.InputTypeText}
	}
	out := make([]string, 0, 2)
	if hasText {
		out = append(out, aienums.InputTypeText)
	}
	if hasImage {
		out = append(out, aienums.InputTypeImage)
	}
	return out
}

// mergeCatalog 把拉取回来的模型 ID 合并进现有目录，返回新目录与本次新增的 ID。
//
// 已存在的行**原样保留**（管理员填的显示名 / 窗口 / 输入类型不能被一次拉取抹掉），
// 新 ID 追加成裸行（显示名 = ID，元数据留空，等管理员补）。
func mergeCatalog(existing []aidto.ModelEntry, fetched []string) (out []aidto.ModelEntry, added []string) {
	out = make([]aidto.ModelEntry, 0, len(existing)+len(fetched))
	seen := make(map[string]struct{}, len(existing)+len(fetched))
	for _, m := range existing {
		if _, dup := seen[m.ID]; dup {
			continue
		}
		seen[m.ID] = struct{}{}
		out = append(out, m)
	}
	added = make([]string, 0, len(fetched))
	for _, raw := range fetched {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, aidto.ModelEntry{
			ID:          id,
			DisplayName: id,
			InputTypes:  []string{aienums.InputTypeText},
		})
		added = append(added, id)
	}
	return out, added
}

// SaveModels 整组保存模型目录（乐观锁保护下的 config_data 写回）。
func (s *Service) SaveModels(ctx context.Context, req *aidto.SaveModelsReq) (res *aidto.Provider, err error) {
	if req == nil || req.ProviderID <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	models, nerr := normalizeModels(req.Models)
	if nerr != nil {
		return nil, nerr
	}
	current, err := s.m.FindByID(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	fields := map[string]any{"config_data": modelsToConfig(current.ConfigData, models)}
	if err = s.updateWithVersion(ctx, req.ProviderID, req.Version, fields, req.UpdateBy); err != nil {
		return nil, err
	}
	return s.GetProvider(ctx, req.ProviderID)
}

// RestoreDefaultModels 用代码内置清单整组替换模型目录。
//
// 没有内置清单时返回 ErrNoBuiltinModels 且**不写库**：静默清空会让管理员以为
// 「这家供应商确实没有默认模型」，而真实原因是「代码里没登记」—— 两者要分开说。
func (s *Service) RestoreDefaultModels(ctx context.Context, req *aidto.ProviderActionReq) (res *aidto.Provider, err error) {
	if req == nil || req.ProviderID <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	current, err := s.m.FindByID(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	models, ok := BuiltinModels(current.ProviderKey)
	if !ok {
		return nil, ErrNoBuiltinModels
	}
	fields := map[string]any{"config_data": modelsToConfig(current.ConfigData, models)}
	if err = s.updateWithVersion(ctx, req.ProviderID, req.Version, fields, req.UpdateBy); err != nil {
		return nil, err
	}
	return s.GetProvider(ctx, req.ProviderID)
}

// FetchAvailableModels 调 {base_url}/models 拉候选，合并进目录后落库。
//
// 行为边界：
//
//	· 地址取「供应商自己填的 base_url」，留空时回退到内置默认地址；两者都没有 → 报错
//	  让用户先填地址（不猜）；
//	· 有密钥就带 Authorization: Bearer（明文只在本次请求内存里）；没有就不带 ——
//	  本地网关 / 内网兼容端点可能不需要密钥；
//	· 拉取失败（网络 / 非 200 / 响应不是 OpenAI 兼容形状）统一回 ErrModelsFetchFailed，
//	  **不落库**（半路失败不该把目录写成半份）；底层原因只进日志，不上页面。
func (s *Service) FetchAvailableModels(ctx context.Context, req *aidto.ProviderActionReq) (res *aidto.FetchModelsResult, err error) {
	if req == nil || req.ProviderID <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	current, err := s.m.FindByID(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	ids, perr := s.probeModelIDs(ctx, current)
	if perr != nil {
		return nil, perr
	}
	merged, added := mergeCatalog(parseModels(current.ConfigData), ids)
	fields := map[string]any{"config_data": modelsToConfig(current.ConfigData, merged)}
	if err = s.updateWithVersion(ctx, req.ProviderID, req.Version, fields, req.UpdateBy); err != nil {
		return nil, err
	}
	provider, err := s.GetProvider(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}
	return &aidto.FetchModelsResult{Provider: provider, Added: added, Fetched: len(ids)}, nil
}

// probeModelIDs 解析出站地址、解密密钥并拉一次候选（**只读，不落库**）。
//
// FetchAvailableModels（拉完直接并库）与 FetchModelCandidates（只把候选交给弹窗）共用它：
// 两条路径对「这家到底能不能拉」必须给出同一个答案，否则会出现「弹窗能拉、确认后失败」
// 或反之 —— 差别只在调用方写不写库。
func (s *Service) probeModelIDs(ctx context.Context, current *aimodel.AIProviderEntity) (ids []string, err error) {
	baseURL := strings.TrimSpace(current.BaseURL)
	if baseURL == "" {
		baseURL = BuiltinBaseURL(current.ProviderKey)
	}
	if baseURL == "" {
		return nil, ErrBaseURLRequired
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	if verr := validateURL(endpoint); verr != nil {
		return nil, verr
	}
	apiKey, derr := s.decryptSecret(current.APIKeyCipher)
	if derr != nil {
		return nil, derr
	}
	return s.fetchModelIDs(ctx, endpoint, apiKey)
}

// fetchModelIDs 发一次出站请求并解析 OpenAI 兼容的模型列表。
func (s *Service) fetchModelIDs(ctx context.Context, endpoint, apiKey string) (ids []string, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, ClientTimeout)
	defer cancel()
	req, rerr := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if rerr != nil {
		// 走到这里只可能是地址串本身不合法（前面已过 validateURL）。
		return nil, ErrURLMalformed
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, derr := s.client.Do(req)
	if derr != nil {
		logger.Scene("ai").With("endpoint", endpoint).Error(derr, "获取可用模型失败：请求未发出")
		return nil, ErrModelsFetchFailed
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		logger.Scene("ai").With("endpoint", endpoint).With("status", resp.StatusCode).
			Error(errors.New(aienums.ErrModelsFetchFailed), "获取可用模型失败：远端非 200")
		return nil, ErrModelsFetchFailed
	}
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, MaxModelsRead))
	if rerr != nil {
		logger.Scene("ai").With("endpoint", endpoint).Error(rerr, "获取可用模型失败：读取响应中断")
		return nil, ErrModelsFetchFailed
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if uerr := json.Unmarshal(body, &payload); uerr != nil {
		logger.Scene("ai").With("endpoint", endpoint).Error(uerr, "获取可用模型失败：响应不是 OpenAI 兼容形状")
		return nil, ErrModelsFetchFailed
	}
	ids = make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// jsonString 从 JSON 解码后的任意值里取字符串（非字符串回空串）。
func jsonString(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// jsonInt64 从 JSON 解码后的任意值里取整数（float64 / json.Number 都能吃）。
func jsonInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
	}
	return 0
}

// jsonStringSlice 从 JSON 解码后的任意值里取字符串切片（逐项 trim、丢空项）。
func jsonStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := jsonString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// FetchModelCandidates 拉 {base_url}/models 的候选列表并原样交回，**不写库**。
//
// 与 FetchAvailableModels 的唯一差别是写不写库：地址解析（自己填的 → 内置默认）、
// SSRF 校验、密钥解密、超时与错误归口都走同一个 probeModelIDs，
// 两条路径必须给出同样的「这家能不能拉」的判断，否则会出现「弹窗拉得到、确定后失败」。
func (s *Service) FetchModelCandidates(ctx context.Context, req *aidto.ProviderActionReq) (res *aidto.ModelCandidates, err error) {
	if req == nil || req.ProviderID <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	current, err := s.m.FindByID(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	ids, perr := s.probeModelIDs(ctx, current)
	if perr != nil {
		return nil, perr
	}
	provider, err := s.GetProvider(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}
	existing := make([]string, 0, len(provider.Models))
	for i := range provider.Models {
		existing = append(existing, provider.Models[i].ID)
	}
	return &aidto.ModelCandidates{
		Provider:   provider,
		Candidates: ids,
		Existing:   existing,
		Fetched:    len(ids),
	}, nil
}

// presetEndpoint 是一家供应商的一个协议端点：同一家的不同协议分组地址可能不同
// （openrouter 的 anthropic-messages 是 https://openrouter.ai/api，openai-completions 是
// https://openrouter.ai/api/v1），所以地址挂在端点上而不是供应商上。
type presetEndpoint struct {
	DSHAPI   string // dsh 的协议字符串原文
	Protocol string // 本模块协议枚举；未登记协议为空串
	BaseURL  string // 该协议分组的默认地址
}

// presetProvider 是一家内置预设供应商。
type presetProvider struct {
	Key         string // provider_key（与 dsh 的文件名一致）
	DisplayName string // 默认展示名（用户可改）
	Endpoints   []presetEndpoint
	Models      []aidto.ModelEntry
}

// 输入类型集合：模型行共享这两个只读切片，BuiltinModels 返回时逐条深拷贝。
var (
	inputTextOnly  = []string{aienums.InputTypeText}
	inputTextImage = []string{aienums.InputTypeText, aienums.InputTypeImage}
)

// primaryEndpoint 返回默认端点：按本模块已实现协议的优先级
// （openai_chat_completions > anthropic_messages > openai_responses > gemini_generate_content）
// 取第一个可选中的协议分组。
//
// 一家都没有已映射协议时（amazon-bedrock / azure-openai-responses / google-vertex / mistral /
// openai-codex / radius）回退到第一个端点、只取它的地址：**协议为空不等于没有地址** ——
// 直接把默认地址一起丢掉，界面上会显示成「这家没有内置默认地址」，用户于是手填一个错地址，
// 而真相只是「这家协议要自己选」。
func (p presetProvider) primaryEndpoint() presetEndpoint {
	for _, want := range []string{
		aienums.ProtocolOpenAIChatCompletions,
		aienums.ProtocolAnthropicMessages,
		aienums.ProtocolOpenAIResponses,
		aienums.ProtocolGeminiGenerateContent,
	} {
		for _, ep := range p.Endpoints {
			if ep.Protocol == want {
				return ep
			}
		}
	}
	for _, ep := range p.Endpoints {
		return presetEndpoint{DSHAPI: ep.DSHAPI, BaseURL: ep.BaseURL}
	}
	return presetEndpoint{}
}

// BuiltinPresetOption 是内置预设暴露给界面的元信息（不含任何密钥）。
type BuiltinPresetOption struct {
	Key         string
	DisplayName string
	BaseURL     string // 默认端点地址（dsh 侧没给地址的家为空，见下）
	Protocol    string // 默认协议（未登记协议时为空，界面要求用户自选）
	ModelCount  int
}

// BuiltinPresets 返回全部内置预设（按 key 字典序），供「第三方模型提供商」下拉使用。
func BuiltinPresets() []BuiltinPresetOption {
	out := make([]BuiltinPresetOption, 0, len(presetProviders))
	for _, p := range presetProviders {
		ep := p.primaryEndpoint()
		out = append(out, BuiltinPresetOption{
			Key:         p.Key,
			DisplayName: p.DisplayName,
			BaseURL:     ep.BaseURL,
			Protocol:    ep.Protocol,
			ModelCount:  len(p.Models),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// BuiltinPreset 按 provider_key 取一家内置预设（key 归一大小写与空白；未登记回 false）。
//
// 页面「第三方模型提供商」提交时用它兜底两处默认值：展示名留空 → 用预设展示名；
// 协议留空 → 用预设协议。未登记协议的家（amazon-bedrock 等）预设协议本身为空，
// 兜底后仍为空、由服务端校验拒绝 —— 不静默回落成默认协议。
func BuiltinPreset(providerKey string) (opt BuiltinPresetOption, ok bool) {
	key := strings.ToLower(strings.TrimSpace(providerKey))
	for _, p := range presetProviders {
		if p.Key != key {
			continue
		}
		ep := p.primaryEndpoint()
		return BuiltinPresetOption{
			Key:         p.Key,
			DisplayName: p.DisplayName,
			BaseURL:     ep.BaseURL,
			Protocol:    ep.Protocol,
			ModelCount:  len(p.Models),
		}, true
	}
	return BuiltinPresetOption{}, false
}

// presetProviders 内置预设表：41 家供应商、1495 个模型（生成数据，勿手改）。
var presetProviders = []presetProvider{
	{
		Key:         "amazon-bedrock",
		DisplayName: "Amazon Bedrock",
		Endpoints: []presetEndpoint{
			// dsh 里同一协议按区域给了两个地址；默认只登记 us-east-1，其它区域由管理员在界面上改。
			{DSHAPI: "bedrock-converse-stream", Protocol: "", BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com"},
		},
		Models: presetModelsAmazonBedrock,
	},
	{
		Key:         "ant-ling",
		DisplayName: "Ant Ling",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.ant-ling.com/v1"},
		},
		Models: presetModelsAntLing,
	},
	{
		Key:         "anthropic",
		DisplayName: "Anthropic",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.anthropic.com"},
		},
		Models: presetModelsAnthropic,
	},
	{
		Key:         "azure-openai-responses",
		DisplayName: "Azure OpenAI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "azure-openai-responses", Protocol: "", BaseURL: ""},
		},
		Models: presetModelsAzureOpenaiResponses,
	},
	{
		Key:         "baseten",
		DisplayName: "Baseten",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://inference.baseten.co/v1"},
		},
		Models: presetModelsBaseten,
	},
	{
		Key:         "cerebras",
		DisplayName: "Cerebras",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.cerebras.ai/v1"},
		},
		Models: presetModelsCerebras,
	},
	{
		Key:         "cloudflare-ai-gateway",
		DisplayName: "Cloudflare AI Gateway",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/compat"},
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"},
		},
		Models: presetModelsCloudflareAiGateway,
	},
	{
		Key:         "cloudflare-workers-ai",
		DisplayName: "Cloudflare Workers AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1"},
		},
		Models: presetModelsCloudflareWorkersAi,
	},
	{
		Key:         "deepseek",
		DisplayName: "DeepSeek",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.deepseek.com"},
		},
		Models: presetModelsDeepseek,
	},
	{
		Key:         "fireworks",
		DisplayName: "Fireworks AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.fireworks.ai/inference"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.fireworks.ai/inference/v1"},
		},
		Models: presetModelsFireworks,
	},
	{
		Key:         "github-copilot",
		DisplayName: "GitHub Copilot",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.individual.githubcopilot.com"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.individual.githubcopilot.com"},
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://api.individual.githubcopilot.com"},
		},
		Models: presetModelsGithubCopilot,
	},
	{
		Key:         "google",
		DisplayName: "Google Gemini",
		Endpoints: []presetEndpoint{
			{DSHAPI: "google-generative-ai", Protocol: aienums.ProtocolGeminiGenerateContent, BaseURL: "https://generativelanguage.googleapis.com/v1beta"},
		},
		Models: presetModelsGoogle,
	},
	{
		Key:         "google-vertex",
		DisplayName: "Google Vertex AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "google-vertex", Protocol: "", BaseURL: "https://{location}-aiplatform.googleapis.com"},
		},
		Models: presetModelsGoogleVertex,
	},
	{
		Key:         "groq",
		DisplayName: "Groq",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.groq.com/openai/v1"},
		},
		Models: presetModelsGroq,
	},
	{
		Key:         "huggingface",
		DisplayName: "Hugging Face",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://router.huggingface.co/v1"},
		},
		Models: presetModelsHuggingface,
	},
	{
		Key:         "kimi-coding",
		DisplayName: "Kimi Coding",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.kimi.com/coding"},
		},
		Models: presetModelsKimiCoding,
	},
	{
		Key:         "meta",
		DisplayName: "Meta",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://api.meta.ai/v1"},
		},
		Models: presetModelsMeta,
	},
	{
		Key:         "minimax",
		DisplayName: "MiniMax",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.minimax.io/anthropic"},
		},
		Models: presetModelsMinimax,
	},
	{
		Key:         "minimax-cn",
		DisplayName: "MiniMax (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.minimaxi.com/anthropic"},
		},
		Models: presetModelsMinimaxCn,
	},
	{
		Key:         "mistral",
		DisplayName: "Mistral AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "mistral-conversations", Protocol: "", BaseURL: "https://api.mistral.ai"},
		},
		Models: presetModelsMistral,
	},
	{
		Key:         "moonshotai",
		DisplayName: "Moonshot AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.moonshot.ai/v1"},
		},
		Models: presetModelsMoonshotai,
	},
	{
		Key:         "moonshotai-cn",
		DisplayName: "Moonshot AI (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.moonshot.cn/v1"},
		},
		Models: presetModelsMoonshotaiCn,
	},
	{
		Key:         "nvidia",
		DisplayName: "NVIDIA",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://integrate.api.nvidia.com/v1"},
		},
		Models: presetModelsNvidia,
	},
	{
		Key:         "openai",
		DisplayName: "OpenAI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://api.openai.com/v1"},
		},
		Models: presetModelsOpenai,
	},
	{
		Key:         "openai-codex",
		DisplayName: "OpenAI Codex",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-codex-responses", Protocol: "", BaseURL: "https://chatgpt.com/backend-api"},
		},
		Models: presetModelsOpenaiCodex,
	},
	{
		Key:         "opencode",
		DisplayName: "OpenCode Zen",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://opencode.ai/zen"},
			{DSHAPI: "google-generative-ai", Protocol: aienums.ProtocolGeminiGenerateContent, BaseURL: "https://opencode.ai/zen/v1"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://opencode.ai/zen/v1"},
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://opencode.ai/zen/v1"},
		},
		Models: presetModelsOpencode,
	},
	{
		Key:         "opencode-go",
		DisplayName: "OpenCode Zen Go",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://opencode.ai/zen/go"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://opencode.ai/zen/go/v1"},
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://opencode.ai/zen/go/v1"},
		},
		Models: presetModelsOpencodeGo,
	},
	{
		Key:         "openrouter",
		DisplayName: "OpenRouter",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://openrouter.ai/api"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://openrouter.ai/api/v1"},
		},
		Models: presetModelsOpenrouter,
	},
	{
		Key:         "qwen-token-plan",
		DisplayName: "Qwen Token Plan",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1"},
		},
		Models: presetModelsQwenTokenPlan,
	},
	{
		Key:         "qwen-token-plan-cn",
		DisplayName: "Qwen Token Plan (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"},
		},
		Models: presetModelsQwenTokenPlanCn,
	},
	{
		Key:         "qwen-token-plan-individual",
		DisplayName: "Qwen Token Plan (Individual)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1"},
		},
		Models: presetModelsQwenTokenPlanIndividual,
	},
	{
		Key:         "radius",
		DisplayName: "Radius",
		Endpoints: []presetEndpoint{
			{DSHAPI: "pi-messages", Protocol: "", BaseURL: "https://radius.pi.dev/v1"},
		},
		Models: presetModelsRadius,
	},
	{
		Key:         "together",
		DisplayName: "Together AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.together.ai/v1"},
		},
		Models: presetModelsTogether,
	},
	{
		Key:         "vercel-ai-gateway",
		DisplayName: "Vercel AI Gateway",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://ai-gateway.vercel.sh"},
		},
		Models: presetModelsVercelAiGateway,
	},
	{
		Key:         "xai",
		DisplayName: "xAI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://api.x.ai/v1"},
		},
		Models: presetModelsXai,
	},
	{
		Key:         "xiaomi",
		DisplayName: "Xiaomi MiMo",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.xiaomimimo.com/v1"},
		},
		Models: presetModelsXiaomi,
	},
	{
		Key:         "xiaomi-token-plan-ams",
		DisplayName: "Xiaomi MiMo Token Plan (AMS)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan-ams.xiaomimimo.com/v1"},
		},
		Models: presetModelsXiaomiTokenPlanAms,
	},
	{
		Key:         "xiaomi-token-plan-cn",
		DisplayName: "Xiaomi MiMo Token Plan (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan-cn.xiaomimimo.com/v1"},
		},
		Models: presetModelsXiaomiTokenPlanCn,
	},
	{
		Key:         "xiaomi-token-plan-sgp",
		DisplayName: "Xiaomi MiMo Token Plan (SGP)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan-sgp.xiaomimimo.com/v1"},
		},
		Models: presetModelsXiaomiTokenPlanSgp,
	},
	{
		Key:         "zai",
		DisplayName: "Z.ai",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.z.ai/api/coding/paas/v4"},
		},
		Models: presetModelsZai,
	},
	{
		Key:         "zai-coding-cn",
		DisplayName: "Z.ai Coding (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4"},
		},
		Models: presetModelsZaiCodingCn,
	},
}

// presetModelsAntLing —— ant-ling（3 个模型）。
var presetModelsAntLing = []aidto.ModelEntry{
	{ID: "Ling-2.6-1T", DisplayName: "Ling 2.6 1T", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "Ling-2.6-flash", DisplayName: "Ling 2.6 Flash", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "Ring-2.6-1T", DisplayName: "Ring 2.6 1T", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
}

// presetModelsAnthropic —— anthropic（15 个模型）。
var presetModelsAnthropic = []aidto.ModelEntry{
	{ID: "claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-fable-5-1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5 (latest)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4-5-20251001", DisplayName: "Claude Haiku 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5 (latest)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-5-20251101", DisplayName: "Claude Opus 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-7", DisplayName: "Claude Opus 4.7", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5 (latest)", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-5-20250929", DisplayName: "Claude Sonnet 4.5", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
}

// presetModelsBaseten —— baseten（21 个模型）。
var presetModelsBaseten = []aidto.ModelEntry{
	{ID: "deepseek-ai/DeepSeek-V4-Flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4.1-Flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.5", DisplayName: "Kimi K2.5", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.6", DisplayName: "Kimi K2.6", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.7-Code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "nvidia/NVIDIA-Nemotron-3-Ultra-550B-A55B", DisplayName: "Nemotron Ultra", ContextWindow: 202800, MaxOutputTokens: 202800, InputTypes: inputTextOnly},
	{ID: "nvidia/Nemotron-120B-A12B", DisplayName: "Nemotron Super", ContextWindow: 202800, MaxOutputTokens: 202800, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-120b", DisplayName: "OpenAI GPT 120B", ContextWindow: 128072, MaxOutputTokens: 128072, InputTypes: inputTextOnly},
	{ID: "thinkingmachines/inkling", DisplayName: "Inkling", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "thinkingmachines/inkling-small", DisplayName: "Inkling Small", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-4.7", DisplayName: "GLM 4.7", ContextWindow: 200000, MaxOutputTokens: 200000, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5", DisplayName: "GLM 5", ContextWindow: 202800, MaxOutputTokens: 202800, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.1", DisplayName: "GLM 5.1", ContextWindow: 202800, MaxOutputTokens: 202800, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.2", DisplayName: "GLM 5.2", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.2-Fast", DisplayName: "GLM 5.2 Fast", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.3", DisplayName: "GLM 5.3", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-5.3-Fast", DisplayName: "GLM 5.3 Fast", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-5.3-Flash", DisplayName: "GLM 5.3 Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsCerebras —— cerebras（2 个模型）。
var presetModelsCerebras = []aidto.ModelEntry{
	{ID: "gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 40960, InputTypes: inputTextOnly},
	{ID: "qwen-3.8-27b", DisplayName: "Qwen3.8 27B", ContextWindow: 65536, MaxOutputTokens: 32768, InputTypes: inputTextImage},
}

// presetModelsCloudflareWorkersAi —— cloudflare-workers-ai（18 个模型）。
var presetModelsCloudflareWorkersAi = []aidto.ModelEntry{
	{ID: "@cf/deepseek-ai/deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "@cf/deepseek-ai/deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "@cf/google/gemma-4-26b-a4b-it", DisplayName: "Gemma 4 26B A4B IT", ContextWindow: 256000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "@cf/ibm-granite/granite-4.0-h-micro", DisplayName: "Granite 4.0 H Micro", ContextWindow: 131000, MaxOutputTokens: 131000, InputTypes: inputTextOnly},
	{ID: "@cf/meta/llama-3.3-70b-instruct-fp8-fast", DisplayName: "Llama 3.3 70B Instruct fp8 Fast", ContextWindow: 24000, MaxOutputTokens: 24000, InputTypes: inputTextOnly},
	{ID: "@cf/meta/llama-4-scout-17b-16e-instruct", DisplayName: "Llama 4 Scout 17B 16E Instruct", ContextWindow: 131000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "@cf/mistralai/mistral-small-3.1-24b-instruct", DisplayName: "Mistral Small 3.1 24B Instruct", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "@cf/moonshotai/kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "@cf/moonshotai/kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "@cf/nvidia/nemotron-3-120b-a12b", DisplayName: "Nemotron 3 Super 120B", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextOnly},
	{ID: "@cf/openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "@cf/openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "@cf/qwen/qwen3-30b-a3b-fp8", DisplayName: "Qwen3 30B A3b fp8", ContextWindow: 32768, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "@cf/qwen/qwen3.8-27b", DisplayName: "Qwen3.8 27B", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "@cf/zai-org/glm-4.7-flash", DisplayName: "GLM-4.7-Flash", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "@cf/zai-org/glm-5.2", DisplayName: "Glm 5.2", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextOnly},
	{ID: "@cf/zai-org/glm-5.3", DisplayName: "Glm 5.3", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "@cf/zai-org/glm-5.3-flash", DisplayName: "Glm 5.3 Flash", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
}

// presetModelsDeepseek —— deepseek（2 个模型）。
var presetModelsDeepseek = []aidto.ModelEntry{
	{ID: "deepseek-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
}

// presetModelsGoogle —— google（22 个模型）。
var presetModelsGoogle = []aidto.ModelEntry{
	{ID: "deep-research-max-preview-04-2026", DisplayName: "Deep Research Max Preview (Apr-21-2026)", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "deep-research-preview-04-2026", DisplayName: "Deep Research Preview (Apr-21-2026)", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-computer-use-preview-10-2025", DisplayName: "Gemini 2.5 Computer Use Preview 10-2025", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash-Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-lite", DisplayName: "Gemini 3.1 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-lite-image", DisplayName: "Nano Banana 2 Lite", ContextWindow: 65536, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-lite-preview", DisplayName: "Gemini 3.1 Flash Lite Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-live-preview", DisplayName: "Gemini 3.1 Flash Live Preview", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro-preview-customtools", DisplayName: "Gemini 3.1 Pro Preview Custom Tools", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash-lite", DisplayName: "Gemini 3.5 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.7-flash", DisplayName: "Gemini 3.7 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-flash-latest", DisplayName: "Gemini Flash Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-flash-lite-latest", DisplayName: "Gemini Flash-Lite Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemma-4-26b-a4b-it", DisplayName: "Gemma 4 26B A4B IT", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gemma-4-31b-it", DisplayName: "Gemma 4 31B IT", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
}

// presetModelsGoogleVertex —— google-vertex（14 个模型）。
var presetModelsGoogleVertex = []aidto.ModelEntry{
	{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash-Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-lite", DisplayName: "Gemini 3.1 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro-preview-customtools", DisplayName: "Gemini 3.1 Pro Preview Custom Tools", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash-lite", DisplayName: "Gemini 3.5 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.7-flash", DisplayName: "Gemini 3.7 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-flash-latest", DisplayName: "Gemini Flash Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-flash-lite-latest", DisplayName: "Gemini Flash-Lite Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
}

// presetModelsGroq —— groq（7 个模型）。
var presetModelsGroq = []aidto.ModelEntry{
	{ID: "llama-3.1-8b-instant", DisplayName: "Llama 3.1 8B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "llama-3.3-70b-versatile", DisplayName: "Llama 3.3 70B", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-safeguard-20b", DisplayName: "Safety GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3.6-27b", DisplayName: "Qwen3.6 27B", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.8-27b", DisplayName: "Qwen3.8 27B", ContextWindow: 131042, MaxOutputTokens: 16384, InputTypes: inputTextImage},
}

// presetModelsKimiCoding —— kimi-coding（4 个模型）。
var presetModelsKimiCoding = []aidto.ModelEntry{
	{ID: "k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "k3-256k", DisplayName: "Kimi K3-256K", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "kimi-for-coding", DisplayName: "kimi-for-coding", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "kimi-for-coding-highspeed", DisplayName: "Kimi For Coding HighSpeed", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
}

// presetModelsMeta —— meta（5 个模型）。
var presetModelsMeta = []aidto.ModelEntry{
	{ID: "muse-spark-1.1", DisplayName: "Muse Spark 1.1", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.2", DisplayName: "Muse Spark 1.2", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.2-contributor", DisplayName: "Muse Spark 1.2 Contributor", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.3", DisplayName: "Muse Spark 1.3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.3-contributor", DisplayName: "Muse Spark 1.3 Contributor", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsMinimax —— minimax（3 个模型）。
var presetModelsMinimax = []aidto.ModelEntry{
	{ID: "MiniMax-M2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMax-M2.7-highspeed", DisplayName: "MiniMax-M2.7-highspeed", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMax-M3", DisplayName: "MiniMax-M3", ContextWindow: 1048576, MaxOutputTokens: 512000, InputTypes: inputTextImage},
}

// presetModelsMinimaxCn —— minimax-cn（3 个模型）。
var presetModelsMinimaxCn = []aidto.ModelEntry{
	{ID: "MiniMax-M2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMax-M2.7-highspeed", DisplayName: "MiniMax-M2.7-highspeed", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMax-M3", DisplayName: "MiniMax-M3", ContextWindow: 1048576, MaxOutputTokens: 512000, InputTypes: inputTextImage},
}

// presetModelsMoonshotai —— moonshotai（4 个模型）。
var presetModelsMoonshotai = []aidto.ModelEntry{
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code-highspeed", DisplayName: "Kimi K2.7 Code HighSpeed", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsMoonshotaiCn —— moonshotai-cn（4 个模型）。
var presetModelsMoonshotaiCn = []aidto.ModelEntry{
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code-highspeed", DisplayName: "Kimi K2.7 Code HighSpeed", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsNvidia —— nvidia（19 个模型）。
var presetModelsNvidia = []aidto.ModelEntry{
	{ID: "google/gemma-3-12b-it", DisplayName: "Gemma 3 12B IT", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "google/gemma-3-4b-it", DisplayName: "Gemma 3 4B IT", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "meta/llama-3.2-11b-vision-instruct", DisplayName: "Llama 3.2 11b Vision Instruct", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "meta/llama-3.2-90b-vision-instruct", DisplayName: "Llama-3.2-90B-Vision-Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "meta/muse-glimmer-30b", DisplayName: "Muse Glimmer 30B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-7b-instruct-v0.3", DisplayName: "Mistral-7B-Instruct-v0.3", ContextWindow: 65536, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "nvidia/cosmos-reason2-8b", DisplayName: "Cosmos Reason2 8B", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "nvidia/llama-3.1-nemotron-70b-instruct", DisplayName: "Llama 3.1 Nemotron 70B Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "nvidia/llama-3.1-nemotron-ultra-253b-v1", DisplayName: "Llama 3.1 Nemotron Ultra 253B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning", DisplayName: "Nemotron 3 Nano Omni", ContextWindow: 256000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "nvidia/nemotron-3-super-120b-a12b", DisplayName: "Nemotron 3 Super", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b", DisplayName: "Nemotron 3 Ultra 550B A55B", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3.5-lightning-30b-a3b", DisplayName: "Nemotron 3.5 Lightning 30B A3B", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "poolside/laguna-xs-2.1", DisplayName: "Laguna XS 2.1", ContextWindow: 262144, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsOpenaiCodex —— openai-codex（8 个模型）。
var presetModelsOpenaiCodex = []aidto.ModelEntry{
	{ID: "gpt-5.3-codex-spark", DisplayName: "GPT-5.3 Codex Spark", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "gpt-5.5", DisplayName: "GPT-5.5", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-luna", DisplayName: "GPT-6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-sol", DisplayName: "GPT-6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
}

// presetModelsOpencodeGo —— opencode-go（30 个模型）。
var presetModelsOpencodeGo = []aidto.ModelEntry{
	{ID: "minimax-m3", DisplayName: "MiniMax-M3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash-vision-exp", DisplayName: "DeepSeek V4 Flash Vision Exp", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro (New)", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "glm-5.1", DisplayName: "GLM-5.1", ContextWindow: 202752, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "hy3", DisplayName: "Hy3", ContextWindow: 256000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "hy4-preview", DisplayName: "Hy4 preview", ContextWindow: 1024000, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "longcat-2.0", DisplayName: "LongCat-2.0", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.5", DisplayName: "MiMo V2.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo V2.5 Pro", ContextWindow: 1048576, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "minimax-m2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.6-plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.7-max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "grok-4.6", DisplayName: "Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "grok-4.7", DisplayName: "Grok 4.7", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "muse-spark-1.2-contributor", DisplayName: "Muse Spark 1.2 Contributor", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.3-contributor", DisplayName: "Muse Spark 1.3 Contributor", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsQwenTokenPlan —— qwen-token-plan（20 个模型）。
var presetModelsQwenTokenPlan = []aidto.ModelEntry{
	{ID: "MiniMax-M2.5", DisplayName: "MiniMax-M2.5", ContextWindow: 196608, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "deepseek-v3.2", DisplayName: "DeepSeek V3.2", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "glm-5", DisplayName: "GLM-5", ContextWindow: 202752, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "glm-5.1", DisplayName: "GLM-5.1", ContextWindow: 202752, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "kimi-k2.5", DisplayName: "Kimi K2.5", ContextWindow: 262144, MaxOutputTokens: 98304, InputTypes: inputTextImage},
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "qwen3.6-flash", DisplayName: "Qwen3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.6-plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.7-max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen3.8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsQwenTokenPlanCn —— qwen-token-plan-cn（20 个模型）。
var presetModelsQwenTokenPlanCn = []aidto.ModelEntry{
	{ID: "MiniMax-M2.5", DisplayName: "MiniMax-M2.5", ContextWindow: 196608, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "deepseek-v3.2", DisplayName: "DeepSeek V3.2", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "glm-5", DisplayName: "GLM-5", ContextWindow: 202752, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "glm-5.1", DisplayName: "GLM-5.1", ContextWindow: 202752, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "kimi-k2.5", DisplayName: "Kimi K2.5", ContextWindow: 262144, MaxOutputTokens: 98304, InputTypes: inputTextImage},
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "qwen3.6-flash", DisplayName: "Qwen3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.6-plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.7-max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen3.8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsQwenTokenPlanIndividual —— qwen-token-plan-individual（9 个模型）。
var presetModelsQwenTokenPlanIndividual = []aidto.ModelEntry{
	{ID: "deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.6-flash", DisplayName: "Qwen3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.7-max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen3.8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsRadius —— radius（30 个模型）。
var presetModelsRadius = []aidto.ModelEntry{
	{ID: "balanced", DisplayName: "Balanced", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "cheap", DisplayName: "Cheap", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-fable-5-1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM 5.2", ContextWindow: 432000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "gpt-5.3-codex", DisplayName: "GPT 5.3 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4", DisplayName: "GPT 5.4", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-mini", DisplayName: "GPT 5.4 Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5", DisplayName: "GPT 5.5", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT 5.6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT 5.6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT 5.6 Terra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT 6 Astra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-luna", DisplayName: "GPT 6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-sol", DisplayName: "GPT 6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "precise", DisplayName: "Precise", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
}

// presetModelsTogether —— together（22 个模型）。
var presetModelsTogether = []aidto.ModelEntry{
	{ID: "MiniMaxAI/MiniMax-M2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 202752, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMaxAI/MiniMax-M3", DisplayName: "MiniMax-M3", ContextWindow: 524288, MaxOutputTokens: 250000, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen2.5-7B-Instruct-Turbo", DisplayName: "Qwen 2.5 7B Instruct Turbo", ContextWindow: 32768, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3.5-9B", DisplayName: "Qwen3.5 9B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.6-Plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 500000, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3.7-Max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 500000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 512000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4.1-Flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "google/gemma-4-31B-it", DisplayName: "Gemma 4 31B Instruct", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "meta-llama/Llama-3.3-70B-Instruct-Turbo", DisplayName: "Llama 3.3 70B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "moonshotai/Kimi-K2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 131000, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.7-Code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "moonshotai/Kimi-K3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b", DisplayName: "Nemotron 3 Ultra 550B A55B", ContextWindow: 512300, MaxOutputTokens: 512300, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "thinkingmachines/Inkling", DisplayName: "Inkling", ContextWindow: 524288, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-5.2", DisplayName: "GLM-5.2", ContextWindow: 512000, MaxOutputTokens: 164000, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.3", DisplayName: "GLM-5.3", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.3-Flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1048575, MaxOutputTokens: 400000, InputTypes: inputTextImage},
}

// presetModelsXai —— xai（4 个模型）。
var presetModelsXai = []aidto.ModelEntry{
	{ID: "grok-4.3", DisplayName: "Grok 4.3", ContextWindow: 1000000, MaxOutputTokens: 30000, InputTypes: inputTextImage},
	{ID: "grok-4.5", DisplayName: "Grok 4.5", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "grok-4.6", DisplayName: "Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "grok-4.7", DisplayName: "Grok 4.7", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
}

// presetModelsXiaomi —— xiaomi（6 个模型）。
var presetModelsXiaomi = []aidto.ModelEntry{
	{ID: "mimo-v2.5", DisplayName: "MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.5-pro-ultraspeed", DisplayName: "MiMo-V2.5-Pro-UltraSpeed", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro-ultraspeed", DisplayName: "MiMo-V2.6-Pro-UltraSpeed", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsXiaomiTokenPlanAms —— xiaomi-token-plan-ams（4 个模型）。
var presetModelsXiaomiTokenPlanAms = []aidto.ModelEntry{
	{ID: "mimo-v2.5", DisplayName: "MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsXiaomiTokenPlanCn —— xiaomi-token-plan-cn（4 个模型）。
var presetModelsXiaomiTokenPlanCn = []aidto.ModelEntry{
	{ID: "mimo-v2.5", DisplayName: "MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsXiaomiTokenPlanSgp —— xiaomi-token-plan-sgp（4 个模型）。
var presetModelsXiaomiTokenPlanSgp = []aidto.ModelEntry{
	{ID: "mimo-v2.5", DisplayName: "MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsZai —— zai（7 个模型）。
var presetModelsZai = []aidto.ModelEntry{
	{ID: "glm-4.7", DisplayName: "GLM-4.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5-turbo", DisplayName: "GLM-5-Turbo", ContextWindow: 200000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.2-highspeed", DisplayName: "GLM-5.2 Highspeed", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "glm-5.3-highspeed", DisplayName: "GLM-5.3 Highspeed", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
}

// presetModelsZaiCodingCn —— zai-coding-cn（4 个模型）。
var presetModelsZaiCodingCn = []aidto.ModelEntry{
	{ID: "glm-4.6v", DisplayName: "GLM-4.6V", ContextWindow: 128000, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "glm-5.3-highspeed", DisplayName: "GLM-5.3 Highspeed", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
}

// presetModelsAmazonBedrock —— amazon-bedrock（165 个模型）。
var presetModelsAmazonBedrock = []aidto.ModelEntry{
	{ID: "amazon.nova-2-lite-v1:0", DisplayName: "Nova 2 Lite", ContextWindow: 1000000, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "amazon.nova-lite-v1:0", DisplayName: "Nova Lite", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "amazon.nova-micro-v1:0", DisplayName: "Nova Micro", ContextWindow: 128000, MaxOutputTokens: 10000, InputTypes: inputTextOnly},
	{ID: "amazon.nova-pro-v1:0", DisplayName: "Nova Pro", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-fable-5-1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-haiku-4-5-20251001-v1:0", DisplayName: "Claude Haiku 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-opus-4-1-20250805-v1:0", DisplayName: "Claude Opus 4.1", ContextWindow: 200000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-opus-4-5-20251101-v1:0", DisplayName: "Claude Opus 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-opus-4-6-v1", DisplayName: "Claude Opus 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-opus-4-7", DisplayName: "Claude Opus 4.7", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-opus-4-8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-opus-5-5", DisplayName: "Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-sonnet-4-5-20250929-v1:0", DisplayName: "Claude Sonnet 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic.claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "apac.amazon.nova-lite-v1:0", DisplayName: "Nova Lite (APAC)", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "apac.amazon.nova-micro-v1:0", DisplayName: "Nova Micro (APAC)", ContextWindow: 128000, MaxOutputTokens: 10000, InputTypes: inputTextOnly},
	{ID: "apac.amazon.nova-pro-v1:0", DisplayName: "Nova Pro (APAC)", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "apac.anthropic.claude-sonnet-4-20250514-v1:0", DisplayName: "Claude Sonnet 4 (APAC)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-haiku-4-5-20251001-v1:0", DisplayName: "Claude Haiku 4.5 (AU)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-opus-4-6-v1", DisplayName: "AU Anthropic Claude Opus 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-opus-4-7", DisplayName: "Claude Opus 4.7 (AU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-opus-4-8", DisplayName: "Claude Opus 4.8 (AU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-opus-5", DisplayName: "Claude Opus 5 (AU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-opus-5-5", DisplayName: "Claude Opus 5.5 (AU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-sonnet-4-5-20250929-v1:0", DisplayName: "Claude Sonnet 4.5 (AU)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-sonnet-4-6", DisplayName: "AU Anthropic Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "au.anthropic.claude-sonnet-5", DisplayName: "Claude Sonnet 5 (AU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "ca.amazon.nova-lite-v1:0", DisplayName: "Nova Lite (CA)", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "deepseek.v3-v1:0", DisplayName: "DeepSeek-V3.1", ContextWindow: 163840, MaxOutputTokens: 81920, InputTypes: inputTextOnly},
	{ID: "deepseek.v3.2", DisplayName: "DeepSeek V3.2", ContextWindow: 163840, MaxOutputTokens: 81920, InputTypes: inputTextOnly},
	{ID: "eu.amazon.nova-2-lite-v1:0", DisplayName: "Nova 2 Lite (EU)", ContextWindow: 1000000, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "eu.amazon.nova-lite-v1:0", DisplayName: "Nova Lite (EU)", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "eu.amazon.nova-micro-v1:0", DisplayName: "Nova Micro (EU)", ContextWindow: 128000, MaxOutputTokens: 10000, InputTypes: inputTextOnly},
	{ID: "eu.amazon.nova-pro-v1:0", DisplayName: "Nova Pro (EU)", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-fable-5", DisplayName: "Claude Fable 5 (EU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-haiku-4-5-20251001-v1:0", DisplayName: "Claude Haiku 4.5 (EU)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-opus-4-5-20251101-v1:0", DisplayName: "Claude Opus 4.5 (EU)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-opus-4-6-v1", DisplayName: "Claude Opus 4.6 (EU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-opus-4-7", DisplayName: "Claude Opus 4.7 (EU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-opus-4-8", DisplayName: "Claude Opus 4.8 (EU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-opus-5", DisplayName: "Claude Opus 5 (EU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-opus-5-5", DisplayName: "Claude Opus 5.5 (EU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-sonnet-4-20250514-v1:0", DisplayName: "Claude Sonnet 4 (EU)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-sonnet-4-5-20250929-v1:0", DisplayName: "Claude Sonnet 4.5 (EU)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6 (EU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "eu.anthropic.claude-sonnet-5", DisplayName: "Claude Sonnet 5 (EU)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "eu.mistral.pixtral-large-2502-v1:0", DisplayName: "Pixtral Large (25.02) (EU)", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "global.amazon.nova-2-lite-v1:0", DisplayName: "Nova 2 Lite (Global)", ContextWindow: 1000000, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-fable-5", DisplayName: "Claude Fable 5 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-fable-5-1", DisplayName: "Claude Fable 5.1 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-haiku-4-5-20251001-v1:0", DisplayName: "Claude Haiku 4.5 (Global)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-opus-4-5-20251101-v1:0", DisplayName: "Claude Opus 4.5 (Global)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-opus-4-6-v1", DisplayName: "Claude Opus 4.6 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-opus-4-7", DisplayName: "Claude Opus 4.7 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-opus-4-8", DisplayName: "Claude Opus 4.8 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-opus-5", DisplayName: "Claude Opus 5 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-opus-5-5", DisplayName: "Claude Opus 5.5 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-sonnet-4-20250514-v1:0", DisplayName: "Claude Sonnet 4 (Global)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-sonnet-4-5-20250929-v1:0", DisplayName: "Claude Sonnet 4.5 (Global)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.anthropic.claude-sonnet-5", DisplayName: "Claude Sonnet 5 (Global)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.openai.gpt-5.6-luna", DisplayName: "GPT-5.6 Luna (Global)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.openai.gpt-5.6-sol", DisplayName: "GPT-5.6 Sol (Global)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.openai.gpt-5.6-terra", DisplayName: "GPT-5.6 Terra (Global)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.openai.gpt-6-astra", DisplayName: "GPT-6 Astra (Global)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "global.xai.grok-4.6", DisplayName: "Grok 4.6 (Global)", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "google.gemma-4-26b-a4b", DisplayName: "Gemma 4 26B A4B IT", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "google.gemma-4-31b", DisplayName: "Gemma 4 31B IT", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "google.gemma-4-e2b", DisplayName: "Gemma 4 E2B IT", ContextWindow: 131072, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "in.openai.gpt-5.6-luna", DisplayName: "GPT-5.6 Luna (India)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "in.openai.gpt-5.6-terra", DisplayName: "GPT-5.6 Terra (India)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "jp.amazon.nova-2-lite-v1:0", DisplayName: "Nova 2 Lite (JP)", ContextWindow: 1000000, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "jp.anthropic.claude-haiku-4-5-20251001-v1:0", DisplayName: "Claude Haiku 4.5 (JP)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "jp.anthropic.claude-opus-4-7", DisplayName: "Claude Opus 4.7 (JP)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "jp.anthropic.claude-opus-4-8", DisplayName: "Claude Opus 4.8 (JP)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "jp.anthropic.claude-opus-5", DisplayName: "Claude Opus 5 (JP)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "jp.anthropic.claude-opus-5-5", DisplayName: "Claude Opus 5.5 (JP)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "jp.anthropic.claude-sonnet-4-5-20250929-v1:0", DisplayName: "Claude Sonnet 4.5 (JP)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "jp.anthropic.claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6 (JP)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "jp.anthropic.claude-sonnet-5", DisplayName: "Claude Sonnet 5 (JP)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "meta.llama3-1-70b-instruct-v1:0", DisplayName: "Llama 3.1 70B Instruct", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "meta.llama3-1-8b-instruct-v1:0", DisplayName: "Llama 3.1 8B Instruct", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "meta.llama3-3-70b-instruct-v1:0", DisplayName: "Llama 3.3 70B Instruct", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "meta.llama4-maverick-17b-instruct-v1:0", DisplayName: "Llama 4 Maverick 17B Instruct", ContextWindow: 1000000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "meta.llama4-scout-17b-instruct-v1:0", DisplayName: "Llama 4 Scout 17B Instruct", ContextWindow: 10000000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "minimax.minimax-m2", DisplayName: "MiniMax-M2", ContextWindow: 204608, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "minimax.minimax-m2.1", DisplayName: "MiniMax-M2.1", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "minimax.minimax-m2.5", DisplayName: "MiniMax-M2.5", ContextWindow: 196608, MaxOutputTokens: 98304, InputTypes: inputTextOnly},
	{ID: "mistral.devstral-2-123b", DisplayName: "Devstral 2 123B", ContextWindow: 256000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "mistral.magistral-small-2509", DisplayName: "Magistral Small 1.2", ContextWindow: 128000, MaxOutputTokens: 40000, InputTypes: inputTextImage},
	{ID: "mistral.ministral-3-14b-instruct", DisplayName: "Ministral 14B 3.0", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "mistral.ministral-3-3b-instruct", DisplayName: "Ministral 3 3B", ContextWindow: 256000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "mistral.ministral-3-8b-instruct", DisplayName: "Ministral 3 8B", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "mistral.mistral-large-3-675b-instruct", DisplayName: "Mistral Large 3", ContextWindow: 256000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "mistral.pixtral-large-2502-v1:0", DisplayName: "Pixtral Large (25.02)", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "mistral.voxtral-mini-3b-2507", DisplayName: "Voxtral Mini 3B 2507", ContextWindow: 32768, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "mistral.voxtral-small-24b-2507", DisplayName: "Voxtral Small 24B 2507", ContextWindow: 32768, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "moonshot.kimi-k2-thinking", DisplayName: "Kimi K2 Thinking", ContextWindow: 262143, MaxOutputTokens: 16000, InputTypes: inputTextOnly},
	{ID: "moonshotai.kimi-k2.5", DisplayName: "Kimi K2.5", ContextWindow: 262143, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "nvidia.nemotron-nano-12b-v2", DisplayName: "NVIDIA Nemotron Nano 12B v2 VL BF16", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "nvidia.nemotron-nano-3-30b", DisplayName: "NVIDIA Nemotron Nano 3 30B", ContextWindow: 262144, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "nvidia.nemotron-nano-9b-v2", DisplayName: "NVIDIA Nemotron Nano 9B v2", ContextWindow: 131072, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "nvidia.nemotron-super-3-120b", DisplayName: "NVIDIA Nemotron 3 Super 120B A12B", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "openai.gpt-5.4", DisplayName: "GPT-5.4", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai.gpt-5.5", DisplayName: "GPT-5.5", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai.gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai.gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai.gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai.gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai.gpt-oss-120b", DisplayName: "gpt-oss-120b", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "openai.gpt-oss-120b-1:0", DisplayName: "gpt-oss-120b", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "openai.gpt-oss-20b", DisplayName: "gpt-oss-20b", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "openai.gpt-oss-20b-1:0", DisplayName: "gpt-oss-20b", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "openai.gpt-oss-safeguard-120b", DisplayName: "GPT OSS Safeguard 120B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "openai.gpt-oss-safeguard-20b", DisplayName: "GPT OSS Safeguard 20B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "qwen.qwen3-235b-a22b-2507-v1:0", DisplayName: "Qwen3 235B-A22B Instruct 2507", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen.qwen3-32b-v1:0", DisplayName: "Qwen3 32B", ContextWindow: 32768, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "qwen.qwen3-coder-30b-a3b-v1:0", DisplayName: "Qwen3-Coder 30B-A3B Instruct", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen.qwen3-coder-480b-a35b-v1:0", DisplayName: "Qwen3-Coder 480B-A35B Instruct", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen.qwen3-coder-next", DisplayName: "Qwen3 Coder Next", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen.qwen3-next-80b-a3b", DisplayName: "Qwen3-Next 80B-A3B Instruct", ContextWindow: 262144, MaxOutputTokens: 262000, InputTypes: inputTextOnly},
	{ID: "qwen.qwen3-vl-235b-a22b", DisplayName: "Qwen3 VL 235B A22B Instruct", ContextWindow: 262144, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "us-gov.openai.gpt-oss-120b-1:0", DisplayName: "gpt-oss-120b (GovCloud)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "us-gov.openai.gpt-oss-20b-1:0", DisplayName: "gpt-oss-20b (GovCloud)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "us.amazon.nova-2-lite-v1:0", DisplayName: "Nova 2 Lite (US)", ContextWindow: 1000000, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "us.amazon.nova-lite-v1:0", DisplayName: "Nova Lite (US)", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "us.amazon.nova-micro-v1:0", DisplayName: "Nova Micro (US)", ContextWindow: 128000, MaxOutputTokens: 10000, InputTypes: inputTextOnly},
	{ID: "us.amazon.nova-premier-v1:0", DisplayName: "Nova Premier (US)", ContextWindow: 1000000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "us.amazon.nova-pro-v1:0", DisplayName: "Nova Pro (US)", ContextWindow: 300000, MaxOutputTokens: 10000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-fable-5", DisplayName: "Claude Fable 5 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-fable-5-1", DisplayName: "Claude Fable 5.1 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-haiku-4-5-20251001-v1:0", DisplayName: "Claude Haiku 4.5 (US)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-opus-4-1-20250805-v1:0", DisplayName: "Claude Opus 4.1 (US)", ContextWindow: 200000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-opus-4-5-20251101-v1:0", DisplayName: "Claude Opus 4.5 (US)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-opus-4-6-v1", DisplayName: "Claude Opus 4.6 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-opus-4-7", DisplayName: "Claude Opus 4.7 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-opus-4-8", DisplayName: "Claude Opus 4.8 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-opus-5", DisplayName: "Claude Opus 5 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-opus-5-5", DisplayName: "Claude Opus 5.5 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-sonnet-4-20250514-v1:0", DisplayName: "Claude Sonnet 4 (US)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-sonnet-4-5-20250929-v1:0", DisplayName: "Claude Sonnet 4.5 (US)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.anthropic.claude-sonnet-5", DisplayName: "Claude Sonnet 5 (US)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.meta.llama3-1-70b-instruct-v1:0", DisplayName: "Llama 3.1 70B Instruct (US)", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "us.meta.llama3-1-8b-instruct-v1:0", DisplayName: "Llama 3.1 8B Instruct (US)", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "us.meta.llama3-3-70b-instruct-v1:0", DisplayName: "Llama 3.3 70B Instruct (US)", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "us.meta.llama4-maverick-17b-instruct-v1:0", DisplayName: "Llama 4 Maverick 17B Instruct (US)", ContextWindow: 1000000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "us.meta.llama4-scout-17b-instruct-v1:0", DisplayName: "Llama 4 Scout 17B Instruct (US)", ContextWindow: 10000000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "us.mistral.pixtral-large-2502-v1:0", DisplayName: "Pixtral Large (25.02) (US)", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "us.openai.gpt-5.6-luna", DisplayName: "GPT-5.6 Luna (US)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.openai.gpt-5.6-sol", DisplayName: "GPT-5.6 Sol (US)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.openai.gpt-5.6-terra", DisplayName: "GPT-5.6 Terra (US)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.openai.gpt-6-astra", DisplayName: "GPT-6 Astra (US)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "us.writer.palmyra-x4-v1:0", DisplayName: "Palmyra X4 (US)", ContextWindow: 122880, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "us.writer.palmyra-x5-v1:0", DisplayName: "Palmyra X5 (US)", ContextWindow: 1040000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "us.xai.grok-4.6", DisplayName: "Grok 4.6 (US)", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "writer.palmyra-x4-v1:0", DisplayName: "Palmyra X4", ContextWindow: 122880, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "writer.palmyra-x5-v1:0", DisplayName: "Palmyra X5", ContextWindow: 1040000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "xai.grok-4.3", DisplayName: "Grok 4.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "xai.grok-4.6", DisplayName: "Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "zai.glm-4.7", DisplayName: "GLM-4.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai.glm-4.7-flash", DisplayName: "GLM-4.7-Flash", ContextWindow: 200000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai.glm-5", DisplayName: "GLM-5", ContextWindow: 202752, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
}

// presetModelsAzureOpenaiResponses —— azure-openai-responses（41 个模型）。
var presetModelsAzureOpenaiResponses = []aidto.ModelEntry{
	{ID: "gpt-4", DisplayName: "GPT-4", ContextWindow: 8192, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "gpt-4-turbo", DisplayName: "GPT-4 Turbo", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "gpt-4.1", DisplayName: "GPT-4.1", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4.1-mini", DisplayName: "GPT-4.1 mini", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4.1-nano", DisplayName: "GPT-4.1 nano", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4o", DisplayName: "GPT-4o", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-4o-2024-05-13", DisplayName: "GPT-4o (2024-05-13)", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "gpt-4o-2024-08-06", DisplayName: "GPT-4o (2024-08-06)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-4o-2024-11-20", DisplayName: "GPT-4o (2024-11-20)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-4o-mini", DisplayName: "GPT-4o mini", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5", DisplayName: "GPT-5", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-chat-latest", DisplayName: "GPT-5 Chat Latest", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5-mini", DisplayName: "GPT-5 Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-nano", DisplayName: "GPT-5 Nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-pro", DisplayName: "GPT-5 Pro", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.1", DisplayName: "GPT-5.1", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.2", DisplayName: "GPT-5.2", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.2-chat-latest", DisplayName: "GPT-5.2 Chat", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5.2-pro", DisplayName: "GPT-5.2 Pro", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.3-chat-latest", DisplayName: "GPT-5.3 Chat (latest)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.3-codex-spark", DisplayName: "GPT-5.3 Codex Spark", ContextWindow: 128000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "gpt-5.4", DisplayName: "GPT-5.4", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-mini", DisplayName: "GPT-5.4 mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-nano", DisplayName: "GPT-5.4 nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-pro", DisplayName: "GPT-5.4 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5", DisplayName: "GPT-5.5", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5-pro", DisplayName: "GPT-5.5 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-luna", DisplayName: "GPT-6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-sol", DisplayName: "GPT-6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-realtime-2.1", DisplayName: "GPT-Realtime-2.1", ContextWindow: 128000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "o1", DisplayName: "o1", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o1-pro", DisplayName: "o1-pro", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o3", DisplayName: "o3", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o3-mini", DisplayName: "o3-mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextOnly},
	{ID: "o3-pro", DisplayName: "o3-pro", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o4-mini", DisplayName: "o4-mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
}

// presetModelsCloudflareAiGateway —— cloudflare-ai-gateway（51 个模型）。
var presetModelsCloudflareAiGateway = []aidto.ModelEntry{
	{ID: "claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-fable-5.1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4.5", DisplayName: "Claude Haiku 4.5 (latest)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4.5", DisplayName: "Claude Opus 4.5 (latest)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4.6", DisplayName: "Claude Opus 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-4.7", DisplayName: "Claude Opus 4.7", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-4.8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4.5", DisplayName: "Claude Sonnet 4.5 (latest)", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4.6", DisplayName: "Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "workers-ai/@cf/deepseek-ai/deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/deepseek-ai/deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/google/gemma-4-26b-a4b-it", DisplayName: "Gemma 4 26B A4B IT", ContextWindow: 256000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "workers-ai/@cf/ibm-granite/granite-4.0-h-micro", DisplayName: "Granite 4.0 H Micro", ContextWindow: 131000, MaxOutputTokens: 131000, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/meta/llama-3.3-70b-instruct-fp8-fast", DisplayName: "Llama 3.3 70B Instruct fp8 Fast", ContextWindow: 24000, MaxOutputTokens: 24000, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/meta/llama-4-scout-17b-16e-instruct", DisplayName: "Llama 4 Scout 17B 16E Instruct", ContextWindow: 131000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "workers-ai/@cf/mistralai/mistral-small-3.1-24b-instruct", DisplayName: "Mistral Small 3.1 24B Instruct", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/moonshotai/kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "workers-ai/@cf/moonshotai/kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "workers-ai/@cf/nvidia/nemotron-3-120b-a12b", DisplayName: "Nemotron 3 Super 120B", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/qwen/qwen3-30b-a3b-fp8", DisplayName: "Qwen3 30B A3b fp8", ContextWindow: 32768, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/qwen/qwen3.8-27b", DisplayName: "Qwen3.8 27B", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "workers-ai/@cf/zai-org/glm-4.7-flash", DisplayName: "GLM-4.7-Flash", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/zai-org/glm-5.2", DisplayName: "Glm 5.2", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/zai-org/glm-5.3", DisplayName: "Glm 5.3", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "workers-ai/@cf/zai-org/glm-5.3-flash", DisplayName: "Glm 5.3 Flash", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "gpt-4.1", DisplayName: "GPT-4.1", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4.1-mini", DisplayName: "GPT-4.1 mini", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4.1-nano", DisplayName: "GPT-4.1 nano", ContextWindow: 1000000, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4o", DisplayName: "GPT-4o", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-4o-mini", DisplayName: "GPT-4o mini", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5", DisplayName: "GPT-5", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-mini", DisplayName: "GPT-5 Mini", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-nano", DisplayName: "GPT-5 Nano", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.1", DisplayName: "GPT-5.1", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4", DisplayName: "GPT-5.4", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-mini", DisplayName: "GPT-5.4 mini", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-nano", DisplayName: "GPT-5.4 nano", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-pro", DisplayName: "GPT-5.4 Pro", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5", DisplayName: "GPT-5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5-pro", DisplayName: "GPT-5.5 Pro", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "o3", DisplayName: "o3", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o3-mini", DisplayName: "o3-mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextOnly},
	{ID: "o4-mini", DisplayName: "o4-mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
}

// presetModelsFireworks —— fireworks（33 个模型）。
var presetModelsFireworks = []aidto.ModelEntry{
	{ID: "accounts/fireworks/models/deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/deepseek-v4-flash-vision-exp", DisplayName: "DeepSeek V4 Flash Vision Exp", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/deepseek-v4p1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/inkling", DisplayName: "Inkling", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/kimi-k2p6", DisplayName: "Kimi K2.6", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/kimi-k2p7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/minimax-m2p7", DisplayName: "MiniMax-M2.7", ContextWindow: 196608, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/minimax-m3", DisplayName: "MiniMax-M3", ContextWindow: 512000, MaxOutputTokens: 512000, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/muse-glimmer-30b", DisplayName: "Muse Glimmer 30B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/nemotron-3-ultra-nvfp4", DisplayName: "Nemotron 3 Ultra 550B A55B", ContextWindow: 262144, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/nemotron-lightning-3p5-30b-a3b", DisplayName: "Nemotron 3.5 Lightning 30B A3B", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/qwen3p7-plus", DisplayName: "Qwen 3.7 Plus", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/qwen3p8-2p4t-a95b", DisplayName: "Qwen3.8 2.4T A95B", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/qwen3p8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/routers/deepseek-flash-latest", DisplayName: "DeepSeek Flash Latest", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/routers/deepseek-pro-latest", DisplayName: "DeepSeek Pro Latest", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/routers/kimi-fast-latest", DisplayName: "Kimi Fast Latest", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/routers/kimi-latest", DisplayName: "Kimi Latest", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/routers/minimax-latest", DisplayName: "MiniMax Latest", ContextWindow: 512000, MaxOutputTokens: 512000, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/routers/qwen-max-latest", DisplayName: "Qwen Max Latest (Qwen3.8 Max)", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/glm-5p2", DisplayName: "GLM 5.2", ContextWindow: 1048575, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/glm-5p3", DisplayName: "GLM 5.3", ContextWindow: 1048573, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/models/glm-5p3-flash", DisplayName: "GLM 5.3 Flash", ContextWindow: 1048573, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/models/kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/routers/glm-5p2-fast", DisplayName: "GLM 5.2 Fast", ContextWindow: 1048575, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/routers/glm-5p3-fast", DisplayName: "GLM 5.3 Fast", ContextWindow: 1048572, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/routers/glm-fast-latest", DisplayName: "GLM 5.3 Fast (Latest)", ContextWindow: 1048572, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/routers/glm-flash-latest", DisplayName: "GLM Flash Latest (GLM 5.3 Flash)", ContextWindow: 1048573, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "accounts/fireworks/routers/glm-latest", DisplayName: "GLM Latest", ContextWindow: 1048573, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "accounts/fireworks/routers/kimi-k3-fast", DisplayName: "Kimi K3 Fast", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsGithubCopilot —— github-copilot（32 个模型）。
var presetModelsGithubCopilot = []aidto.ModelEntry{
	{ID: "claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-fable-5.1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4.5", DisplayName: "Claude Haiku 4.5 (latest)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4.7", DisplayName: "Claude Opus 4.7", ContextWindow: 1000000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "claude-opus-4.8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-5.5", DisplayName: "Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4.6", DisplayName: "Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "gemini-3.7-flash", DisplayName: "Gemini 3.7 Flash", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "gpt-5-mini", DisplayName: "GPT-5 Mini", ContextWindow: 264000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4", DisplayName: "GPT-5.4", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-mini", DisplayName: "GPT-5.4 mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-nano", DisplayName: "GPT-5.4 nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5", DisplayName: "GPT-5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-luna", DisplayName: "GPT-6 Luna", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-sol", DisplayName: "GPT-6 Sol", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "grok-4.5", DisplayName: "Grok 4.5", ContextWindow: 500000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "grok-4.6", DisplayName: "Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "grok-4.7", DisplayName: "Grok 4.7", ContextWindow: 500000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "mai-code-1-flash-picker", DisplayName: "MAI-Code-1-Flash", ContextWindow: 256000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "mai-code-1.1-flash", DisplayName: "MAI-Code-1.1-Flash", ContextWindow: 256000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
}

// presetModelsHuggingface —— huggingface（76 个模型）。
var presetModelsHuggingface = []aidto.ModelEntry{
	{ID: "MiniMaxAI/MiniMax-M2", DisplayName: "MiniMax-M2", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMaxAI/MiniMax-M2.1", DisplayName: "MiniMax-M2.1", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMaxAI/MiniMax-M2.5", DisplayName: "MiniMax-M2.5", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMaxAI/MiniMax-M2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMaxAI/MiniMax-M3", DisplayName: "MiniMax-M3", ContextWindow: 524288, MaxOutputTokens: 512000, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen2.5-Coder-32B-Instruct", DisplayName: "Qwen2.5-Coder-32B-Instruct", ContextWindow: 131072, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-235B-A22B", DisplayName: "Qwen3 235B-A22B", ContextWindow: 40960, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-235B-A22B-Instruct-2507", DisplayName: "Qwen3 235B-A22B Instruct 2507", ContextWindow: 262144, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-235B-A22B-Thinking-2507", DisplayName: "Qwen3-235B-A22B-Thinking-2507", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-30B-A3B", DisplayName: "Qwen3 30B A3B", ContextWindow: 40960, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-32B", DisplayName: "Qwen3 32B", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-Coder-30B-A3B-Instruct", DisplayName: "Qwen3-Coder 30B-A3B Instruct", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-Coder-480B-A35B-Instruct", DisplayName: "Qwen3-Coder-480B-A35B-Instruct", ContextWindow: 262144, MaxOutputTokens: 66536, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-Coder-Next", DisplayName: "Qwen3-Coder-Next", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-Next-80B-A3B-Instruct", DisplayName: "Qwen3-Next-80B-A3B-Instruct", ContextWindow: 262144, MaxOutputTokens: 66536, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-Next-80B-A3B-Thinking", DisplayName: "Qwen3-Next-80B-A3B-Thinking", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3-VL-235B-A22B-Instruct", DisplayName: "Qwen3 VL 235B A22B Instruct", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3-VL-235B-A22B-Thinking", DisplayName: "Qwen3 VL 235B A22B Thinking", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.5-122B-A10B", DisplayName: "Qwen3.5 122B-A10B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.5-27B", DisplayName: "Qwen3.5 27B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.5-35B-A3B", DisplayName: "Qwen3.5 35B-A3B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.5-397B-A17B", DisplayName: "Qwen3.5-397B-A17B", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.5-9B", DisplayName: "Qwen3.5 9B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.6-27B", DisplayName: "Qwen3.6 27B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.6-35B-A3B", DisplayName: "Qwen3.6 35B-A3B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.8-2.4T-A95B", DisplayName: "Qwen3.8 2.4T A95B", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3.8-27B", DisplayName: "Qwen3.8 27B", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "XiaomiMiMo/MiMo-V2-Flash", DisplayName: "MiMo-V2-Flash", ContextWindow: 262144, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "XiaomiMiMo/MiMo-V2.5", DisplayName: "MiMo-V2.5", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "XiaomiMiMo/MiMo-V2.5-Pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-R1", DisplayName: "DeepSeek-R1", ContextWindow: 64000, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-R1-0528", DisplayName: "DeepSeek-R1-0528", ContextWindow: 163840, MaxOutputTokens: 163840, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V3", DisplayName: "DeepSeek-V3", ContextWindow: 64000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V3-0324", DisplayName: "DeepSeek V3 0324", ContextWindow: 163840, MaxOutputTokens: 163840, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V3.1", DisplayName: "DeepSeek-V3.1", ContextWindow: 131072, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V3.2", DisplayName: "DeepSeek-V3.2", ContextWindow: 163840, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Flash-Vision-Exp", DisplayName: "DeepSeek V4 Flash Vision Exp", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "deepseek-ai/DeepSeek-V4-Pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1048576, MaxOutputTokens: 393216, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4.1-Flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "google/gemma-3-12b-it", DisplayName: "Gemma 3 12B IT", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "google/gemma-3-27b-it", DisplayName: "Gemma 3 27B IT", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "google/gemma-3-4b-it", DisplayName: "Gemma 3 4B IT", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "google/gemma-4-26B-A4B-it", DisplayName: "Gemma 4 26B A4B IT", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "google/gemma-4-31B-it", DisplayName: "Gemma 4 31B IT", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "meta-llama/Llama-3.1-8B-Instruct", DisplayName: "Llama-3.1-8B-Instruct", ContextWindow: 131072, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "meta-llama/Llama-3.3-70B-Instruct", DisplayName: "Llama-3.3-70B-Instruct", ContextWindow: 131072, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "moonshotai/Kimi-K2-Instruct", DisplayName: "Kimi-K2-Instruct", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "moonshotai/Kimi-K2-Instruct-0905", DisplayName: "Kimi-K2-Instruct-0905", ContextWindow: 262144, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "moonshotai/Kimi-K2-Thinking", DisplayName: "Kimi-K2-Thinking", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "moonshotai/Kimi-K2.5", DisplayName: "Kimi-K2.5", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.6", DisplayName: "Kimi-K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.7-Code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K3", DisplayName: "Kimi K3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "stepfun-ai/Step-3.5-Flash", DisplayName: "Step 3.5 Flash", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextOnly},
	{ID: "stepfun-ai/Step-3.7-Flash", DisplayName: "Step 3.7 Flash", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "tencent/Hy3", DisplayName: "Hy3", ContextWindow: 262144, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "tencent/Hy4-preview", DisplayName: "Hy4 preview", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "thinkingmachines/Inkling", DisplayName: "Inkling", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "thinkingmachines/Inkling-Small", DisplayName: "Inkling Small", ContextWindow: 524288, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-4.5", DisplayName: "GLM-4.5", ContextWindow: 131072, MaxOutputTokens: 98304, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-4.5-Air", DisplayName: "GLM-4.5-Air", ContextWindow: 131072, MaxOutputTokens: 98304, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-4.5V", DisplayName: "GLM-4.5V", ContextWindow: 65536, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-4.6", DisplayName: "GLM-4.6", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-4.6V-Flash", DisplayName: "GLM-4.6V-Flash", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-4.7", DisplayName: "GLM-4.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-4.7-Flash", DisplayName: "GLM-4.7-Flash", ContextWindow: 200000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5", DisplayName: "GLM-5", ContextWindow: 202752, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.1", DisplayName: "GLM-5.1", ContextWindow: 202752, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.2", DisplayName: "GLM-5.2", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.3", DisplayName: "GLM-5.3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.3-Flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsMistral —— mistral（33 个模型）。
var presetModelsMistral = []aidto.ModelEntry{
	{ID: "codestral-latest", DisplayName: "Codestral (latest)", ContextWindow: 256000, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "devstral-2512", DisplayName: "Devstral 2", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "devstral-latest", DisplayName: "Devstral 2", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "devstral-medium-2507", DisplayName: "Devstral Medium", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "devstral-medium-latest", DisplayName: "Devstral 2 (latest)", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "devstral-small-2505", DisplayName: "Devstral Small 2505", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "devstral-small-2507", DisplayName: "Devstral Small", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "labs-devstral-small-2512", DisplayName: "Devstral Small 2", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "magistral-medium-latest", DisplayName: "Magistral Medium (latest)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "magistral-small", DisplayName: "Magistral Small", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "ministral-3b-latest", DisplayName: "Ministral 3B (latest)", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "ministral-8b-latest", DisplayName: "Ministral 8B (latest)", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "mistral-large-2411", DisplayName: "Mistral Large 2.1", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "mistral-large-2512", DisplayName: "Mistral Large 3", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "mistral-large-latest", DisplayName: "Mistral Large (latest)", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "mistral-medium-2505", DisplayName: "Mistral Medium 3", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mistral-medium-2508", DisplayName: "Mistral Medium 3.1", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "mistral-medium-2604", DisplayName: "Mistral Medium 3.5", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "mistral-medium-3.5", DisplayName: "Mistral Medium 3.5", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "mistral-medium-latest", DisplayName: "Mistral Medium (latest)", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "mistral-nemo", DisplayName: "Mistral Nemo", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "mistral-small-2506", DisplayName: "Mistral Small 3.2", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "mistral-small-2603", DisplayName: "Mistral Small 4", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "mistral-small-latest", DisplayName: "Mistral Small (latest)", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "open-mistral-7b", DisplayName: "Mistral 7B", ContextWindow: 8000, MaxOutputTokens: 8000, InputTypes: inputTextOnly},
	{ID: "open-mistral-nemo", DisplayName: "Open Mistral Nemo", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "open-mixtral-8x22b", DisplayName: "Mixtral 8x22B", ContextWindow: 64000, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "open-mixtral-8x7b", DisplayName: "Mixtral 8x7B", ContextWindow: 32000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "pixtral-12b", DisplayName: "Pixtral 12B", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "pixtral-large-latest", DisplayName: "Pixtral Large (latest)", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "voxtral-small-latest", DisplayName: "Voxtral Small (latest)", ContextWindow: 32000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "zai-glm-5-2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "zai-glm-5-3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
}

// presetModelsOpenai —— openai（41 个模型）。
var presetModelsOpenai = []aidto.ModelEntry{
	{ID: "gpt-4", DisplayName: "GPT-4", ContextWindow: 8192, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "gpt-4-turbo", DisplayName: "GPT-4 Turbo", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "gpt-4.1", DisplayName: "GPT-4.1", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4.1-mini", DisplayName: "GPT-4.1 mini", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4.1-nano", DisplayName: "GPT-4.1 nano", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gpt-4o", DisplayName: "GPT-4o", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-4o-2024-05-13", DisplayName: "GPT-4o (2024-05-13)", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "gpt-4o-2024-08-06", DisplayName: "GPT-4o (2024-08-06)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-4o-2024-11-20", DisplayName: "GPT-4o (2024-11-20)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-4o-mini", DisplayName: "GPT-4o mini", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5", DisplayName: "GPT-5", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-chat-latest", DisplayName: "GPT-5 Chat Latest", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5-mini", DisplayName: "GPT-5 Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-nano", DisplayName: "GPT-5 Nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-pro", DisplayName: "GPT-5 Pro", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.1", DisplayName: "GPT-5.1", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.2", DisplayName: "GPT-5.2", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.2-chat-latest", DisplayName: "GPT-5.2 Chat", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5.2-pro", DisplayName: "GPT-5.2 Pro", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.3-chat-latest", DisplayName: "GPT-5.3 Chat (latest)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.3-codex-spark", DisplayName: "GPT-5.3 Codex Spark", ContextWindow: 128000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "gpt-5.4", DisplayName: "GPT-5.4", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-mini", DisplayName: "GPT-5.4 mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-nano", DisplayName: "GPT-5.4 nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-pro", DisplayName: "GPT-5.4 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5", DisplayName: "GPT-5.5", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5-pro", DisplayName: "GPT-5.5 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-luna", DisplayName: "GPT-6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-sol", DisplayName: "GPT-6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-realtime-2.1", DisplayName: "GPT-Realtime-2.1", ContextWindow: 128000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "o1", DisplayName: "o1", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o1-pro", DisplayName: "o1-pro", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o3", DisplayName: "o3", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o3-mini", DisplayName: "o3-mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextOnly},
	{ID: "o3-pro", DisplayName: "o3-pro", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "o4-mini", DisplayName: "o4-mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
}

// presetModelsOpencode —— opencode（73 个模型）。
var presetModelsOpencode = []aidto.ModelEntry{
	{ID: "claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-fable-5-1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-7", DisplayName: "Claude Opus 4.7", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "qwen3.5-plus", DisplayName: "Qwen3.5 Plus", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.6-plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "gemini-3-flash", DisplayName: "Gemini 3 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro", DisplayName: "Gemini 3.1 Pro Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash-lite", DisplayName: "Gemini 3.5 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.7-flash", DisplayName: "Gemini 3.7 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "big-pickle", DisplayName: "Big Pickle", ContextWindow: 200000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash-vision-exp", DisplayName: "DeepSeek V4 Flash Vision Exp", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "glm-5", DisplayName: "GLM-5", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.1", DisplayName: "GLM-5.1", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "kimi-k2.5", DisplayName: "Kimi K2.5", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "ling-3.0-flash-fin-free", DisplayName: "Ling 3.0 Flash Fin Free", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash-free", DisplayName: "MiMo-V2.6-Flash Free", ContextWindow: 200000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "minimax-m2.5", DisplayName: "MiniMax-M2.5", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "minimax-m2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "minimax-m3", DisplayName: "MiniMax-M3", ContextWindow: 512000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "nemotron-3-ultra-free", DisplayName: "Nemotron 3 Ultra Free", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "nemotron-3.5-lightning-free", DisplayName: "Nemotron 3.5 Lightning Free", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "gpt-5", DisplayName: "GPT-5", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-codex", DisplayName: "GPT-5 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5-nano", DisplayName: "GPT-5 Nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.1", DisplayName: "GPT-5.1", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.1-codex", DisplayName: "GPT-5.1 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.1-codex-max", DisplayName: "GPT-5.1 Codex Max", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.1-codex-mini", DisplayName: "GPT-5.1 Codex Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.2", DisplayName: "GPT-5.2", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.2-codex", DisplayName: "GPT-5.2 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4", DisplayName: "GPT-5.4", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-mini", DisplayName: "GPT-5.4 Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-nano", DisplayName: "GPT-5.4 Nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-pro", DisplayName: "GPT-5.4 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5", DisplayName: "GPT-5.5", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5-pro", DisplayName: "GPT-5.5 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-luna", DisplayName: "GPT-6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-sol", DisplayName: "GPT-6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "grok-4.5", DisplayName: "Grok 4.5", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "grok-4.6", DisplayName: "Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "grok-build-0.1", DisplayName: "Grok Build 0.1", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "muse-spark-1.2", DisplayName: "Muse Spark 1.2", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.2-contributor-free", DisplayName: "Muse Spark 1.2 Free", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.3", DisplayName: "Muse Spark 1.3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.3-contributor-free", DisplayName: "Muse Spark 1.3 Free", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsOpenrouter —— openrouter（386 个模型）。
var presetModelsOpenrouter = []aidto.ModelEntry{
	{ID: "anthropic/claude-3-haiku", DisplayName: "Anthropic: Claude 3 Haiku", ContextWindow: 200000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "anthropic/claude-fable-5", DisplayName: "Anthropic: Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-fable-5.1", DisplayName: "Anthropic: Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-haiku-4.5", DisplayName: "Anthropic: Claude Haiku 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.1", DisplayName: "Anthropic: Claude Opus 4.1", ContextWindow: 200000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.5", DisplayName: "Anthropic: Claude Opus 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.6", DisplayName: "Anthropic: Claude Opus 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.7", DisplayName: "Anthropic: Claude Opus 4.7", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.8", DisplayName: "Anthropic: Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-5", DisplayName: "Anthropic: Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-5.5", DisplayName: "Anthropic: Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-4", DisplayName: "Anthropic: Claude Sonnet 4", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-4.5", DisplayName: "Anthropic: Claude Sonnet 4.5", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-4.6", DisplayName: "Anthropic: Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-5", DisplayName: "Anthropic: Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "aion-labs/aion-2.0", DisplayName: "AionLabs: Aion-2.0", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "aion-labs/aion-3.0", DisplayName: "AionLabs: Aion-3.0", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "aion-labs/aion-3.0-mini", DisplayName: "AionLabs: Aion-3.0-Mini", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "amazon/nova-2-lite-v1", DisplayName: "Amazon: Nova 2 Lite", ContextWindow: 1000000, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "amazon/nova-lite-v1", DisplayName: "Amazon: Nova Lite 1.0", ContextWindow: 300000, MaxOutputTokens: 5120, InputTypes: inputTextImage},
	{ID: "amazon/nova-micro-v1", DisplayName: "Amazon: Nova Micro 1.0", ContextWindow: 128000, MaxOutputTokens: 5120, InputTypes: inputTextOnly},
	{ID: "amazon/nova-premier-v1", DisplayName: "Amazon: Nova Premier 1.0", ContextWindow: 1000000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "amazon/nova-pro-v1", DisplayName: "Amazon: Nova Pro 1.0", ContextWindow: 300000, MaxOutputTokens: 5120, InputTypes: inputTextImage},
	{ID: "anthropic/claude-fable-5.1:batch", DisplayName: "Anthropic: Claude Fable 5.1 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-fable-5:batch", DisplayName: "Anthropic: Claude Fable 5 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-haiku-4.5:batch", DisplayName: "Anthropic: Claude Haiku 4.5 (batch)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.1:batch", DisplayName: "Anthropic: Claude Opus 4.1 (batch)", ContextWindow: 200000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.5:batch", DisplayName: "Anthropic: Claude Opus 4.5 (batch)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.6:batch", DisplayName: "Anthropic: Claude Opus 4.6 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.7:batch", DisplayName: "Anthropic: Claude Opus 4.7 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.8:batch", DisplayName: "Anthropic: Claude Opus 4.8 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-5.5:batch", DisplayName: "Anthropic: Claude Opus 5.5 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-5:batch", DisplayName: "Anthropic: Claude Opus 5 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-4.5:batch", DisplayName: "Anthropic: Claude Sonnet 4.5 (batch)", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-4.6:batch", DisplayName: "Anthropic: Claude Sonnet 4.6 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-5:batch", DisplayName: "Anthropic: Claude Sonnet 5 (batch)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "arcee-ai/trinity-large-thinking", DisplayName: "Arcee AI: Trinity Large Thinking", ContextWindow: 262144, MaxOutputTokens: 80000, InputTypes: inputTextOnly},
	{ID: "auto", DisplayName: "Auto", ContextWindow: 2000000, MaxOutputTokens: 30000, InputTypes: inputTextImage},
	{ID: "bytedance-seed/seed-1.6", DisplayName: "ByteDance Seed: Seed 1.6", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "bytedance-seed/seed-1.6-flash", DisplayName: "ByteDance Seed: Seed 1.6 Flash", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "bytedance-seed/seed-2-1-turbo", DisplayName: "ByteDance Seed: Seed 2.1 Turbo", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "bytedance-seed/seed-2.0-code", DisplayName: "ByteDance Seed: Seed-2.0-Code", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "bytedance-seed/seed-2.0-lite", DisplayName: "ByteDance Seed: Seed-2.0-Lite", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "bytedance-seed/seed-2.0-mini", DisplayName: "ByteDance Seed: Seed-2.0-Mini", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "cohere/command-r-08-2024", DisplayName: "Cohere: Command R (08-2024)", ContextWindow: 128000, MaxOutputTokens: 4000, InputTypes: inputTextOnly},
	{ID: "cohere/command-r-plus-08-2024", DisplayName: "Cohere: Command R+ (08-2024)", ContextWindow: 128000, MaxOutputTokens: 4000, InputTypes: inputTextOnly},
	{ID: "cohere/north-mini-code:free", DisplayName: "Cohere: North Mini Code (free)", ContextWindow: 256000, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-chat", DisplayName: "DeepSeek: DeepSeek V3", ContextWindow: 163840, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-chat-v3-0324", DisplayName: "DeepSeek: DeepSeek V3 0324", ContextWindow: 163840, MaxOutputTokens: 147456, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-chat-v3.1", DisplayName: "DeepSeek: DeepSeek V3.1", ContextWindow: 163840, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-r1", DisplayName: "DeepSeek: R1", ContextWindow: 64000, MaxOutputTokens: 16000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-r1-0528", DisplayName: "DeepSeek: R1 0528", ContextWindow: 163840, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v3.1-terminus", DisplayName: "DeepSeek: DeepSeek V3.1 Terminus", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v3.2", DisplayName: "DeepSeek: DeepSeek V3.2", ContextWindow: 163840, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v3.2-exp", DisplayName: "DeepSeek: DeepSeek V3.2 Exp", ContextWindow: 163840, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4-flash", DisplayName: "DeepSeek: DeepSeek V4 Flash 0423", ContextWindow: 1024000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4-flash-0731", DisplayName: "DeepSeek: DeepSeek V4 Flash 0731", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4-flash-vision-exp", DisplayName: "DeepSeek: DeepSeek V4 Flash Vision Exp", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "deepseek/deepseek-v4-pro", DisplayName: "DeepSeek: DeepSeek V4 Pro 0423", ContextWindow: 1024000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4-pro-0813", DisplayName: "DeepSeek: DeepSeek V4 Pro 0813", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4.1-flash", DisplayName: "DeepSeek: DeepSeek V4.1 Flash", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "deepseek/deepseek-v4.1-flash:batch", DisplayName: "DeepSeek: DeepSeek V4.1 Flash (batch)", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "dots-studio/dots-3-note-preview:free", DisplayName: "Dots Studio: Dots3-Note Preview (free)", ContextWindow: 512000, MaxOutputTokens: 460800, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-flash", DisplayName: "Google: Gemini 2.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-flash-lite", DisplayName: "Google: Gemini 2.5 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-flash-lite:batch", DisplayName: "Google: Gemini 2.5 Flash Lite (batch)", ContextWindow: 1048576, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-flash:batch", DisplayName: "Google: Gemini 2.5 Flash (batch)", ContextWindow: 1048576, MaxOutputTokens: 65535, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-pro", DisplayName: "Google: Gemini 2.5 Pro", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-pro-preview", DisplayName: "Google: Gemini 2.5 Pro Preview 06-05", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-pro:batch", DisplayName: "Google: Gemini 2.5 Pro (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3-flash-preview", DisplayName: "Google: Gemini 3 Flash Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3-flash-preview:batch", DisplayName: "Google: Gemini 3 Flash Preview (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3-pro-image", DisplayName: "Google: Nano Banana Pro (Gemini 3 Pro Image)", ContextWindow: 65536, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "google/gemini-3.1-flash-lite", DisplayName: "Google: Gemini 3.1 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.1-flash-lite-preview", DisplayName: "Google: Gemini 3.1 Flash Lite Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.1-flash-lite:batch", DisplayName: "Google: Gemini 3.1 Flash Lite (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.1-pro-preview", DisplayName: "Google: Gemini 3.1 Pro Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.1-pro-preview-customtools", DisplayName: "Google: Gemini 3.1 Pro Preview Custom Tools", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.1-pro-preview:batch", DisplayName: "Google: Gemini 3.1 Pro Preview (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.5-flash", DisplayName: "Google: Gemini 3.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.5-flash-lite", DisplayName: "Google: Gemini 3.5 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.5-flash-lite:batch", DisplayName: "Google: Gemini 3.5 Flash Lite (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.5-flash:batch", DisplayName: "Google: Gemini 3.5 Flash (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.6-flash", DisplayName: "Google: Gemini 3.6 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.6-flash:batch", DisplayName: "Google: Gemini 3.6 Flash (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.7-flash", DisplayName: "Google: Gemini 3.7 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.7-flash:batch", DisplayName: "Google: Gemini 3.7 Flash (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.8-flash", DisplayName: "Google: Gemini 3.8 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.8-flash:batch", DisplayName: "Google: Gemini 3.8 Flash (batch)", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemma-3-12b-it", DisplayName: "Google: Gemma 3 12B", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "google/gemma-3-27b-it", DisplayName: "Google: Gemma 3 27B", ContextWindow: 131072, MaxOutputTokens: 117964, InputTypes: inputTextImage},
	{ID: "google/gemma-4-26b-a4b-it", DisplayName: "Google: Gemma 4 26B A4B", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "google/gemma-4-26b-a4b-it:free", DisplayName: "Google: Gemma 4 26B A4B  (free)", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "google/gemma-4-31b-it", DisplayName: "Google: Gemma 4 31B", ContextWindow: 262144, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "google/gemma-4-31b-it:free", DisplayName: "Google: Gemma 4 31B (free)", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "ibm-granite/granite-4.2-8b", DisplayName: "IBM: Granite 4.2 8B", ContextWindow: 131072, MaxOutputTokens: 117964, InputTypes: inputTextOnly},
	{ID: "inception/mercury-2", DisplayName: "Inception: Mercury 2", ContextWindow: 128000, MaxOutputTokens: 50000, InputTypes: inputTextOnly},
	{ID: "inception/mercury-2.5", DisplayName: "Inception: Mercury 2.5", ContextWindow: 260000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash", DisplayName: "inclusionAI: Ling 3.0 Flash", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-fin", DisplayName: "inclusionAI: Ling 3.0 Flash Fin", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-fin:free", DisplayName: "inclusionAI: Ling 3.0 Flash Fin (free)", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-sante:free", DisplayName: "inclusionAI: Ling 3.0 Flash Sante (free)", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-vl", DisplayName: "inclusionAI: Ling 3.0 Flash VL", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "inclusionai/ling-3.0-flash-vl:free", DisplayName: "inclusionAI: Ling 3.0 Flash VL (free)", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "kwaipilot/kat-coder-pro-v2.5", DisplayName: "Kwaipilot: KAT-Coder-Pro V2.5", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "liquid/lfm-2.5-2.6b:free", DisplayName: "LiquidAI: LFM2.5-2.6B (free)", ContextWindow: 65536, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "meituan/longcat-2.0", DisplayName: "Meituan: LongCat 2.0", ContextWindow: 1048756, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "meta-llama/llama-3.1-70b-instruct", DisplayName: "Meta: Llama 3.1 70B Instruct", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "meta-llama/llama-3.1-8b-instruct", DisplayName: "Meta: Llama 3.1 8B Instruct", ContextWindow: 131072, MaxOutputTokens: 117964, InputTypes: inputTextOnly},
	{ID: "meta-llama/llama-3.3-70b-instruct", DisplayName: "Meta: Llama 3.3 70B Instruct", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "meta-llama/llama-4-maverick", DisplayName: "Meta: Llama 4 Maverick", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "meta-llama/llama-4-scout", DisplayName: "Meta: Llama 4 Scout", ContextWindow: 327680, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "meta/muse-glimmer-30b", DisplayName: "Meta: Muse Glimmer 30B", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.1", DisplayName: "Meta: Muse Spark 1.1", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.2", DisplayName: "Meta: Muse Spark 1.2", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.2-contributor", DisplayName: "Meta: Muse Spark 1.2 Contributor", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.3", DisplayName: "Meta: Muse Spark 1.3", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.3-contributor", DisplayName: "Meta: Muse Spark 1.3 Contributor", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "minimax/minimax-m1", DisplayName: "MiniMax: MiniMax M1", ContextWindow: 1000000, MaxOutputTokens: 40000, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2", DisplayName: "MiniMax: MiniMax M2", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.1", DisplayName: "MiniMax: MiniMax M2.1", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.5", DisplayName: "MiniMax: MiniMax M2.5", ContextWindow: 200000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.7", DisplayName: "MiniMax: MiniMax M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m3", DisplayName: "MiniMax: MiniMax M3", ContextWindow: 524288, MaxOutputTokens: 512000, InputTypes: inputTextImage},
	{ID: "mistralai/codestral-2508", DisplayName: "Mistral: Codestral 2508", ContextWindow: 256000, MaxOutputTokens: 204800, InputTypes: inputTextOnly},
	{ID: "mistralai/codestral-2508:batch", DisplayName: "Mistral: Codestral 2508 (batch)", ContextWindow: 256000, MaxOutputTokens: 204800, InputTypes: inputTextOnly},
	{ID: "mistralai/devstral-2512", DisplayName: "Mistral: Devstral 2 2512", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextOnly},
	{ID: "mistralai/ministral-14b-2512", DisplayName: "Mistral: Ministral 3 14B 2512", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextImage},
	{ID: "mistralai/ministral-3b-2512", DisplayName: "Mistral: Ministral 3 3B 2512", ContextWindow: 131072, MaxOutputTokens: 104857, InputTypes: inputTextImage},
	{ID: "mistralai/ministral-8b-2512", DisplayName: "Mistral: Ministral 3 8B 2512", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextImage},
	{ID: "mistralai/ministral-8b-2512:batch", DisplayName: "Mistral: Ministral 3 8B 2512 (batch)", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-large", DisplayName: "Mistral Large", ContextWindow: 128000, MaxOutputTokens: 102400, InputTypes: inputTextOnly},
	{ID: "mistralai/mistral-large-2407", DisplayName: "Mistral Large 2407", ContextWindow: 131072, MaxOutputTokens: 104857, InputTypes: inputTextOnly},
	{ID: "mistralai/mistral-large-2512:batch", DisplayName: "Mistral: Mistral Large 3 2512 (batch)", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-medium-3", DisplayName: "Mistral: Mistral Medium 3", ContextWindow: 131072, MaxOutputTokens: 104857, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-medium-3-5", DisplayName: "Mistral: Mistral Medium 3.5", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-medium-3-5:batch", DisplayName: "Mistral: Mistral Medium 3.5 (batch)", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-medium-3.1", DisplayName: "Mistral: Mistral Medium 3.1", ContextWindow: 131072, MaxOutputTokens: 104857, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-medium-3.1:batch", DisplayName: "Mistral: Mistral Medium 3.1 (batch)", ContextWindow: 131072, MaxOutputTokens: 104857, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-nemo", DisplayName: "Mistral: Mistral Nemo", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "mistralai/mistral-saba", DisplayName: "Mistral: Saba", ContextWindow: 32768, MaxOutputTokens: 26214, InputTypes: inputTextOnly},
	{ID: "mistralai/mistral-small-2603", DisplayName: "Mistral: Mistral Small 4", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-small-2603:batch", DisplayName: "Mistral: Mistral Small 4 (batch)", ContextWindow: 262144, MaxOutputTokens: 209715, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-small-3.1-24b-instruct", DisplayName: "Mistral: Mistral Small 3.1 24B", ContextWindow: 128000, MaxOutputTokens: 102400, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-small-3.2-24b-instruct", DisplayName: "Mistral: Mistral Small 3.2 24B", ContextWindow: 256000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "mistralai/mixtral-8x22b-instruct", DisplayName: "Mistral: Mixtral 8x22B Instruct", ContextWindow: 65536, MaxOutputTokens: 52428, InputTypes: inputTextOnly},
	{ID: "mistralai/voxtral-small-24b-2507", DisplayName: "Mistral: Voxtral Small 24B 2507", ContextWindow: 32768, MaxOutputTokens: 26214, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2", DisplayName: "MoonshotAI: Kimi K2 0711", ContextWindow: 131072, MaxOutputTokens: 98304, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2-0905", DisplayName: "MoonshotAI: Kimi K2 0905", ContextWindow: 262144, MaxOutputTokens: 98304, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2-thinking", DisplayName: "MoonshotAI: Kimi K2 Thinking", ContextWindow: 262144, MaxOutputTokens: 98304, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2.5", DisplayName: "MoonshotAI: Kimi K2.5", ContextWindow: 262144, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k2.6", DisplayName: "MoonshotAI: Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k2.7-code", DisplayName: "MoonshotAI: Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k3", DisplayName: "MoonshotAI: Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k3:batch", DisplayName: "MoonshotAI: Kimi K3 (batch)", ContextWindow: 1048576, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "nex-agi/nex-n2.5-mini:free", DisplayName: "Nex AGI: Nex-N2.5-Mini (free)", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "nex-agi/nex-n2.5-pro", DisplayName: "Nex AGI: Nex-N2.5-Pro", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "nex-agi/nex-n2.5-pro:free", DisplayName: "Nex AGI: Nex-N2.5-Pro (free)", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "nvidia/nemotron-3-nano-30b-a3b", DisplayName: "NVIDIA: Nemotron 3 Nano 30B A3B", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free", DisplayName: "NVIDIA: Nemotron 3 Nano Omni (free)", ContextWindow: 256000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "nvidia/nemotron-3-super-120b-a12b", DisplayName: "NVIDIA: Nemotron 3 Super", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-super-120b-a12b:free", DisplayName: "NVIDIA: Nemotron 3 Super (free)", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b", DisplayName: "NVIDIA: Nemotron 3 Ultra", ContextWindow: 202800, MaxOutputTokens: 182520, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b:free", DisplayName: "NVIDIA: Nemotron 3 Ultra (free)", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3.5-lightning", DisplayName: "NVIDIA: Nemotron 3.5 Lightning", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3.5-lightning:free", DisplayName: "NVIDIA: Nemotron 3.5 Lightning (free)", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "openai/gpt-3.5-turbo", DisplayName: "OpenAI: GPT-3.5 Turbo", ContextWindow: 16385, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "openai/gpt-3.5-turbo-0613", DisplayName: "OpenAI: GPT-3.5 Turbo (older v0613)", ContextWindow: 4095, MaxOutputTokens: 3685, InputTypes: inputTextOnly},
	{ID: "openai/gpt-3.5-turbo-16k", DisplayName: "OpenAI: GPT-3.5 Turbo 16k", ContextWindow: 16385, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "openai/gpt-3.5-turbo:batch", DisplayName: "OpenAI: GPT-3.5 Turbo (batch)", ContextWindow: 16385, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "openai/gpt-4", DisplayName: "OpenAI: GPT-4", ContextWindow: 8191, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "openai/gpt-4-turbo", DisplayName: "OpenAI: GPT-4 Turbo", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "openai/gpt-4-turbo:batch", DisplayName: "OpenAI: GPT-4 Turbo (batch)", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1", DisplayName: "OpenAI: GPT-4.1", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-mini", DisplayName: "OpenAI: GPT-4.1 Mini", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-mini:batch", DisplayName: "OpenAI: GPT-4.1 Mini (batch)", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-nano", DisplayName: "OpenAI: GPT-4.1 Nano", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-nano:batch", DisplayName: "OpenAI: GPT-4.1 Nano (batch)", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1:batch", DisplayName: "OpenAI: GPT-4.1 (batch)", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o", DisplayName: "OpenAI: GPT-4o", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-2024-05-13", DisplayName: "OpenAI: GPT-4o (2024-05-13)", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-2024-08-06", DisplayName: "OpenAI: GPT-4o (2024-08-06)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-2024-11-20", DisplayName: "OpenAI: GPT-4o (2024-11-20)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-mini", DisplayName: "OpenAI: GPT-4o-mini", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-mini-2024-07-18", DisplayName: "OpenAI: GPT-4o-mini (2024-07-18)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-mini:batch", DisplayName: "OpenAI: GPT-4o-mini (batch)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o:batch", DisplayName: "OpenAI: GPT-4o (batch)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-5", DisplayName: "OpenAI: GPT-5", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-mini", DisplayName: "OpenAI: GPT-5 Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-mini:batch", DisplayName: "OpenAI: GPT-5 Mini (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-nano", DisplayName: "OpenAI: GPT-5 Nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-nano:batch", DisplayName: "OpenAI: GPT-5 Nano (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-pro", DisplayName: "OpenAI: GPT-5 Pro", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-pro:batch", DisplayName: "OpenAI: GPT-5 Pro (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1", DisplayName: "OpenAI: GPT-5.1", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1-codex", DisplayName: "OpenAI: GPT-5.1-Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1-codex-max", DisplayName: "OpenAI: GPT-5.1-Codex-Max", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1-codex-mini", DisplayName: "OpenAI: GPT-5.1-Codex-Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1:batch", DisplayName: "OpenAI: GPT-5.1 (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2", DisplayName: "OpenAI: GPT-5.2", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2-chat", DisplayName: "OpenAI: GPT-5.2 Chat", ContextWindow: 128000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2-codex", DisplayName: "OpenAI: GPT-5.2-Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2-pro", DisplayName: "OpenAI: GPT-5.2 Pro", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2-pro:batch", DisplayName: "OpenAI: GPT-5.2 Pro (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2:batch", DisplayName: "OpenAI: GPT-5.2 (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.3-codex", DisplayName: "OpenAI: GPT-5.3-Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4", DisplayName: "OpenAI: GPT-5.4", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-mini", DisplayName: "OpenAI: GPT-5.4 Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-mini:batch", DisplayName: "OpenAI: GPT-5.4 Mini (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-nano", DisplayName: "OpenAI: GPT-5.4 Nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-nano:batch", DisplayName: "OpenAI: GPT-5.4 Nano (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-pro", DisplayName: "OpenAI: GPT-5.4 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-pro:batch", DisplayName: "OpenAI: GPT-5.4 Pro (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4:batch", DisplayName: "OpenAI: GPT-5.4 (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.5", DisplayName: "OpenAI: GPT-5.5", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.5-pro", DisplayName: "OpenAI: GPT-5.5 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.5-pro:batch", DisplayName: "OpenAI: GPT-5.5 Pro (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.5:batch", DisplayName: "OpenAI: GPT-5.5 (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-luna", DisplayName: "OpenAI: GPT-5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-luna-pro", DisplayName: "OpenAI: GPT-5.6 Luna Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-luna-pro:batch", DisplayName: "OpenAI: GPT-5.6 Luna Pro (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-luna:batch", DisplayName: "OpenAI: GPT-5.6 Luna (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-sol", DisplayName: "OpenAI: GPT-5.6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-sol-pro", DisplayName: "OpenAI: GPT-5.6 Sol Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-sol-pro:batch", DisplayName: "OpenAI: GPT-5.6 Sol Pro (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-sol:batch", DisplayName: "OpenAI: GPT-5.6 Sol (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-terra", DisplayName: "OpenAI: GPT-5.6 Terra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-terra-pro", DisplayName: "OpenAI: GPT-5.6 Terra Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-terra-pro:batch", DisplayName: "OpenAI: GPT-5.6 Terra Pro (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-terra:batch", DisplayName: "OpenAI: GPT-5.6 Terra (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5:batch", DisplayName: "OpenAI: GPT-5 (batch)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-astra", DisplayName: "OpenAI: GPT-6 Astra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-astra-pro", DisplayName: "OpenAI: GPT-6 Astra Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-astra-pro:batch", DisplayName: "OpenAI: GPT-6 Astra Pro (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-astra:batch", DisplayName: "OpenAI: GPT-6 Astra (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-luna", DisplayName: "OpenAI: GPT-6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-luna-pro", DisplayName: "OpenAI: GPT-6 Luna Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-luna-pro:batch", DisplayName: "OpenAI: GPT-6 Luna Pro (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-luna:batch", DisplayName: "OpenAI: GPT-6 Luna (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-sol", DisplayName: "OpenAI: GPT-6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-sol-pro", DisplayName: "OpenAI: GPT-6 Sol Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-sol-pro:batch", DisplayName: "OpenAI: GPT-6 Sol Pro (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-sol:batch", DisplayName: "OpenAI: GPT-6 Sol (batch)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-audio", DisplayName: "OpenAI: GPT Audio", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "openai/gpt-audio-mini", DisplayName: "OpenAI: GPT Audio Mini", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "openai/gpt-chat-latest", DisplayName: "OpenAI: GPT Chat Latest", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-oss-120b", DisplayName: "OpenAI: gpt-oss-120b", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "OpenAI: gpt-oss-20b", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b:batch", DisplayName: "OpenAI: gpt-oss-20b (batch)", ContextWindow: 131072, MaxOutputTokens: 117964, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-safeguard-20b", DisplayName: "OpenAI: gpt-oss-safeguard-20b", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "openai/o1", DisplayName: "OpenAI: o1", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o3", DisplayName: "OpenAI: o3", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o3-mini", DisplayName: "OpenAI: o3 Mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextOnly},
	{ID: "openai/o3-mini-high", DisplayName: "OpenAI: o3 Mini High", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextOnly},
	{ID: "openai/o3-mini:batch", DisplayName: "OpenAI: o3 Mini (batch)", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextOnly},
	{ID: "openai/o3-pro", DisplayName: "OpenAI: o3 Pro", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o3:batch", DisplayName: "OpenAI: o3 (batch)", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o4-mini", DisplayName: "OpenAI: o4 Mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o4-mini-high", DisplayName: "OpenAI: o4 Mini High", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o4-mini:batch", DisplayName: "OpenAI: o4 Mini (batch)", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openrouter/auto", DisplayName: "Auto Router", ContextWindow: 2000000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "openrouter/auto-beta", DisplayName: "Auto Router (Beta)", ContextWindow: 2000000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "openrouter/free", DisplayName: "Free Models Router", ContextWindow: 200000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "openrouter/fusion", DisplayName: "OpenRouter: Fusion", ContextWindow: 1000000, MaxOutputTokens: 30000, InputTypes: inputTextOnly},
	{ID: "poolside/laguna-s-2.1", DisplayName: "Poolside: Laguna S 2.1", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "poolside/laguna-s-2.1:free", DisplayName: "Poolside: Laguna S 2.1 (free)", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "poolside/laguna-xs-2.1", DisplayName: "Poolside: Laguna XS 2.1", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "poolside/laguna-xs-2.1:free", DisplayName: "Poolside: Laguna XS 2.1 (free)", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "prism-ml/ternary-bonsai-2-27b", DisplayName: "PrismML: Ternary Bonsai 2 27B", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen-2.5-72b-instruct", DisplayName: "Qwen2.5 72B Instruct", ContextWindow: 32768, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "qwen/qwen-2.5-7b-instruct", DisplayName: "Qwen: Qwen2.5 7B Instruct", ContextWindow: 32768, MaxOutputTokens: 29491, InputTypes: inputTextOnly},
	{ID: "qwen/qwen-plus", DisplayName: "Qwen: Qwen-Plus", ContextWindow: 1000000, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "qwen/qwen-plus-2025-07-28", DisplayName: "Qwen: Qwen Plus 0728", ContextWindow: 1000000, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-14b", DisplayName: "Qwen: Qwen3 14B", ContextWindow: 40960, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-235b-a22b", DisplayName: "Qwen: Qwen3 235B A22B", ContextWindow: 131072, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-235b-a22b-2507", DisplayName: "Qwen: Qwen3 235B A22B Instruct 2507", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-235b-a22b-thinking-2507", DisplayName: "Qwen: Qwen3 235B A22B Thinking 2507", ContextWindow: 131072, MaxOutputTokens: 117964, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-30b-a3b", DisplayName: "Qwen: Qwen3 30B A3B", ContextWindow: 40960, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-30b-a3b-instruct-2507", DisplayName: "Qwen: Qwen3 30B A3B Instruct 2507", ContextWindow: 128000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-30b-a3b-thinking-2507", DisplayName: "Qwen: Qwen3 30B A3B Thinking 2507", ContextWindow: 81920, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-32b", DisplayName: "Qwen: Qwen3 32B", ContextWindow: 40960, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-8b", DisplayName: "Qwen: Qwen3 8B", ContextWindow: 131072, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-coder", DisplayName: "Qwen: Qwen3 Coder 480B A35B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-coder-30b-a3b-instruct", DisplayName: "Qwen: Qwen3 Coder 30B A3B Instruct", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-coder-flash", DisplayName: "Qwen: Qwen3 Coder Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-coder-next", DisplayName: "Qwen: Qwen3 Coder Next", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-coder-plus", DisplayName: "Qwen: Qwen3 Coder Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-max", DisplayName: "Qwen: Qwen3 Max", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-max-thinking", DisplayName: "Qwen: Qwen3 Max Thinking", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-next-80b-a3b-instruct", DisplayName: "Qwen: Qwen3 Next 80B A3B Instruct", ContextWindow: 262144, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-next-80b-a3b-thinking", DisplayName: "Qwen: Qwen3 Next 80B A3B Thinking", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3-vl-235b-a22b-instruct", DisplayName: "Qwen: Qwen3 VL 235B A22B Instruct", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen3-vl-235b-a22b-thinking", DisplayName: "Qwen: Qwen3 VL 235B A22B Thinking", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen3-vl-30b-a3b-instruct", DisplayName: "Qwen: Qwen3 VL 30B A3B Instruct", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen3-vl-30b-a3b-thinking", DisplayName: "Qwen: Qwen3 VL 30B A3B Thinking", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen3-vl-32b-instruct", DisplayName: "Qwen: Qwen3 VL 32B Instruct", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen3-vl-8b-instruct", DisplayName: "Qwen: Qwen3 VL 8B Instruct", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen3-vl-8b-thinking", DisplayName: "Qwen: Qwen3 VL 8B Thinking", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.5-122b-a10b", DisplayName: "Qwen: Qwen3.5-122B-A10B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.5-27b", DisplayName: "Qwen: Qwen3.5-27B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.5-35b-a3b", DisplayName: "Qwen: Qwen3.5-35B-A3B", ContextWindow: 256000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.5-397b-a17b", DisplayName: "Qwen: Qwen3.5 397B A17B", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.5-9b", DisplayName: "Qwen: Qwen3.5-9B", ContextWindow: 256000, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.5-flash-02-23", DisplayName: "Qwen: Qwen3.5-Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.5-plus-02-15", DisplayName: "Qwen: Qwen3.5 Plus 2026-02-15", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.5-plus-20260420", DisplayName: "Qwen: Qwen3.5 Plus 2026-04-20", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.6-27b", DisplayName: "Qwen: Qwen3.6 27B", ContextWindow: 262144, MaxOutputTokens: 262140, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.6-35b-a3b", DisplayName: "Qwen: Qwen3.6 35B A3B", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.6-flash", DisplayName: "Qwen: Qwen3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.6-max-preview", DisplayName: "Qwen: Qwen3.6 Max Preview", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3.6-plus", DisplayName: "Qwen: Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.7-flash", DisplayName: "Qwen: Qwen3.7 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.7-max", DisplayName: "Qwen: Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3.7-plus", DisplayName: "Qwen: Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.8-2.4t-a95b", DisplayName: "Qwen: Qwen3.8 2.4T A95B", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3.8-27b", DisplayName: "Qwen: Qwen3.8 27B", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.8-27b:free", DisplayName: "Qwen: Qwen3.8 27B (free)", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.8-flash", DisplayName: "Qwen: Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.8-max-0902", DisplayName: "Qwen: Qwen3.8 Max (0902)", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.8-omni-flash", DisplayName: "Qwen: Qwen3.8 Omni Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "rekaai/reka-edge", DisplayName: "Reka Edge", ContextWindow: 16384, MaxOutputTokens: 14745, InputTypes: inputTextImage},
	{ID: "relace/relace-search", DisplayName: "Relace: Relace Search", ContextWindow: 256000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "sakana/fugu-max", DisplayName: "Sakana: Fugu Max", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "sakana/fugu-ultra", DisplayName: "Sakana: Fugu Ultra", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "sakana/fugu-ultra-v2", DisplayName: "Sakana: Fugu Ultra v2", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "sakana/sakana-namazu", DisplayName: "Sakana: Sakana Namazu", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "sao10k/l3.1-euryale-70b", DisplayName: "Sao10K: Llama 3.1 Euryale 70B v2.2", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "stepfun/step-3.5-flash", DisplayName: "StepFun: Step 3.5 Flash", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "stepfun/step-3.7-flash", DisplayName: "StepFun: Step 3.7 Flash", ContextWindow: 256000, MaxOutputTokens: 230400, InputTypes: inputTextImage},
	{ID: "tencent/hy3", DisplayName: "Tencent: Hy3", ContextWindow: 262144, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "tencent/hy3-preview", DisplayName: "Tencent: Hy3 preview", ContextWindow: 262144, MaxOutputTokens: 235929, InputTypes: inputTextOnly},
	{ID: "tencent/hy4-preview", DisplayName: "Tencent: Hy4 preview", ContextWindow: 1048576, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "thinkingmachines/inkling", DisplayName: "Thinking Machines: Inkling", ContextWindow: 524288, MaxOutputTokens: 471859, InputTypes: inputTextImage},
	{ID: "thinkingmachines/inkling-small", DisplayName: "Thinking Machines: Inkling Small", ContextWindow: 524288, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "thinkingmachines/inkling-small:free", DisplayName: "Thinking Machines: Inkling Small (free)", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "thinkingmachines/inkling:free", DisplayName: "Thinking Machines: Inkling (free)", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "unbiased/pareto", DisplayName: "Pareto", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "upstage/solar-pro-3", DisplayName: "Upstage: Solar Pro 3", ContextWindow: 131072, MaxOutputTokens: 117964, InputTypes: inputTextOnly},
	{ID: "upstage/solar-pro4", DisplayName: "Upstage: Solar Pro 4", ContextWindow: 524288, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "x-ai/grok-4.20", DisplayName: "SpaceXAI: Grok 4.20", ContextWindow: 2000000, MaxOutputTokens: 1800000, InputTypes: inputTextImage},
	{ID: "x-ai/grok-4.3", DisplayName: "SpaceXAI: Grok 4.3", ContextWindow: 1000000, MaxOutputTokens: 900000, InputTypes: inputTextImage},
	{ID: "x-ai/grok-4.3:batch", DisplayName: "SpaceXAI: Grok 4.3 (batch)", ContextWindow: 1000000, MaxOutputTokens: 900000, InputTypes: inputTextImage},
	{ID: "x-ai/grok-4.5", DisplayName: "SpaceXAI: Grok 4.5", ContextWindow: 500000, MaxOutputTokens: 450000, InputTypes: inputTextImage},
	{ID: "x-ai/grok-4.6", DisplayName: "SpaceXAI: Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 450000, InputTypes: inputTextImage},
	{ID: "x-ai/grok-4.7", DisplayName: "SpaceXAI: Grok 4.7", ContextWindow: 500000, MaxOutputTokens: 450000, InputTypes: inputTextImage},
	{ID: "x-ai/grok-build-0.1", DisplayName: "SpaceXAI: Grok Build 0.1", ContextWindow: 256000, MaxOutputTokens: 230400, InputTypes: inputTextImage},
	{ID: "xiaomi/mimo-v2.5", DisplayName: "Xiaomi: MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "xiaomi/mimo-v2.5-pro", DisplayName: "Xiaomi: MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "xiaomi/mimo-v2.6-flash", DisplayName: "Xiaomi: MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "xiaomi/mimo-v2.6-pro", DisplayName: "Xiaomi: MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "xiaomi/mimo-v2.6-pro-ultraspeed", DisplayName: "Xiaomi: MiMo-V2.6-Pro-UltraSpeed", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "z-ai/glm-4.5", DisplayName: "Z.ai: GLM 4.5", ContextWindow: 131072, MaxOutputTokens: 98304, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-4.5-air", DisplayName: "Z.ai: GLM 4.5 Air", ContextWindow: 131072, MaxOutputTokens: 98304, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-4.5v", DisplayName: "Z.ai: GLM 4.5V", ContextWindow: 65536, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "z-ai/glm-4.6", DisplayName: "Z.ai: GLM 4.6", ContextWindow: 198000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-4.6v", DisplayName: "Z.ai: GLM 4.6V", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "z-ai/glm-4.7", DisplayName: "Z.ai: GLM 4.7", ContextWindow: 202752, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-4.7-flash", DisplayName: "Z.ai: GLM 4.7 Flash", ContextWindow: 131072, MaxOutputTokens: 117964, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5", DisplayName: "Z.ai: GLM 5", ContextWindow: 198000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5-turbo", DisplayName: "Z.ai: GLM 5 Turbo", ContextWindow: 202752, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5.1", DisplayName: "Z.ai: GLM 5.1", ContextWindow: 200000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5.2", DisplayName: "Z.ai: GLM 5.2", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5.3", DisplayName: "Z.ai: GLM 5.3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5.3-flash", DisplayName: "Z.ai: GLM 5.3 Flash", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "z-ai/glm-5.3-flash:batch", DisplayName: "Z.ai: GLM 5.3 Flash (batch)", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "z-ai/glm-5.3-flashx", DisplayName: "Z.ai: GLM 5.3 FlashX", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "z-ai/glm-5.3:batch", DisplayName: "Z.ai: GLM 5.3 (batch)", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5v-turbo", DisplayName: "Z.ai: GLM 5V Turbo", ContextWindow: 202752, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "~anthropic/claude-fable-latest", DisplayName: "Anthropic: Claude Fable Latest", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "~anthropic/claude-haiku-latest", DisplayName: "Anthropic: Claude Haiku Latest", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "~anthropic/claude-opus-latest", DisplayName: "Anthropic: Claude Opus Latest", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "~anthropic/claude-sonnet-latest", DisplayName: "Anthropic: Claude Sonnet Latest", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "~deepseek/deepseek-flash-latest", DisplayName: "DeepSeek: DeepSeek Flash Latest", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "~deepseek/deepseek-pro-latest", DisplayName: "DeepSeek: DeepSeek Pro Latest", ContextWindow: 1048576, MaxOutputTokens: 393216, InputTypes: inputTextOnly},
	{ID: "~deepseek/deepseek-v4-flash-latest", DisplayName: "DeepSeek: DeepSeek V4 Flash Latest", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextOnly},
	{ID: "~google/gemini-flash-latest", DisplayName: "Google: Gemini Flash Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "~google/gemini-pro-latest", DisplayName: "Google: Gemini Pro Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "~moonshotai/kimi-latest", DisplayName: "MoonshotAI: Kimi Latest", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "~openai/gpt-astra-latest", DisplayName: "OpenAI: GPT Astra Latest", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "~openai/gpt-luna-latest", DisplayName: "OpenAI: GPT Luna Latest", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "~openai/gpt-mini-latest", DisplayName: "OpenAI: GPT Mini Latest", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "~openai/gpt-sol-latest", DisplayName: "OpenAI: GPT Sol Latest", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "~openai/gpt-terra-latest", DisplayName: "OpenAI: GPT Terra Latest", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "~x-ai/grok-latest", DisplayName: "xAI: Grok Latest", ContextWindow: 500000, MaxOutputTokens: 450000, InputTypes: inputTextImage},
	{ID: "~z-ai/glm-flash-latest", DisplayName: "Z.ai: GLM Flash Latest", ContextWindow: 1048576, MaxOutputTokens: 943718, InputTypes: inputTextImage},
	{ID: "~z-ai/glm-latest", DisplayName: "Z.ai: GLM Latest", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
}

// presetModelsVercelAiGateway —— vercel-ai-gateway（246 个模型）。
var presetModelsVercelAiGateway = []aidto.ModelEntry{
	{ID: "alibaba/qwen-3-14b", DisplayName: "Qwen3-14B", ContextWindow: 40960, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen-3-235b", DisplayName: "Qwen3 235B A22B", ContextWindow: 262144, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen-3-30b", DisplayName: "Qwen3-30B-A3B", ContextWindow: 40960, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen-3-32b", DisplayName: "Qwen 3 32B", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen-3.6-max-preview", DisplayName: "Qwen 3.6 Max Preview", ContextWindow: 240000, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-235b-a22b-thinking", DisplayName: "Qwen3 VL 235B A22B Thinking", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3-coder", DisplayName: "Qwen3 Coder 480B A35B Instruct", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-coder-30b-a3b", DisplayName: "Qwen 3 Coder 30B A3B Instruct", ContextWindow: 262144, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-coder-next", DisplayName: "Qwen3 Coder Next", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-coder-plus", DisplayName: "Qwen3 Coder Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-max", DisplayName: "Qwen3 Max", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-max-preview", DisplayName: "Qwen3 Max Preview", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-max-thinking", DisplayName: "Qwen 3 Max Thinking", ContextWindow: 256000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-next-80b-a3b-instruct", DisplayName: "Qwen3 Next 80B A3B Instruct", ContextWindow: 262114, MaxOutputTokens: 262114, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-next-80b-a3b-thinking", DisplayName: "Qwen3 Next 80B A3B Thinking", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3-vl-235b-a22b-instruct", DisplayName: "Qwen3 VL 235B A22B Instruct", ContextWindow: 131072, MaxOutputTokens: 129024, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3-vl-instruct", DisplayName: "Qwen3 VL 235B A22B Instruct", ContextWindow: 131072, MaxOutputTokens: 129024, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3-vl-thinking", DisplayName: "Qwen3 VL 235B A22B Thinking", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.5-flash", DisplayName: "Qwen 3.5 Flash", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.5-plus", DisplayName: "Qwen 3.5 Plus", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.6-27b", DisplayName: "Qwen 3.6 27B", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.6-plus", DisplayName: "Qwen 3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.7-flash", DisplayName: "Qwen 3.7 Flash", ContextWindow: 991000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.7-max", DisplayName: "Qwen 3.7 Max", ContextWindow: 991000, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "alibaba/qwen3.7-plus", DisplayName: "Qwen 3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.8-2.4t-a95b", DisplayName: "Qwen3.8 2.4T A95B", ContextWindow: 262144, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.8-27b", DisplayName: "Qwen3.8 27B", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.8-flash", DisplayName: "Qwen 3.8 Flash", ContextWindow: 991000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.8-max", DisplayName: "Qwen 3.8 Max", ContextWindow: 262144, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.8-max-0902", DisplayName: "Qwen3.8 Max 0902", ContextWindow: 991000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "alibaba/qwen3.8-omni-flash", DisplayName: "Qwen 3.8 Omni Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "amazon/nova-2-lite", DisplayName: "Nova 2 Lite", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextImage},
	{ID: "amazon/nova-lite", DisplayName: "Nova Lite", ContextWindow: 300000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "amazon/nova-micro", DisplayName: "Nova Micro", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "amazon/nova-pro", DisplayName: "Nova Pro", ContextWindow: 300000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "anthropic/claude-3-haiku", DisplayName: "Claude 3 Haiku", ContextWindow: 200000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "anthropic/claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-fable-5.1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-haiku-4.5", DisplayName: "Claude Haiku 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4", DisplayName: "Claude Opus 4", ContextWindow: 200000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.5", DisplayName: "Claude Opus 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.6", DisplayName: "Claude Opus 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.7", DisplayName: "Claude Opus 4.7", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-4.8-fast", DisplayName: "Claude Opus 4.8 (Fast)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-5-fast", DisplayName: "Claude Opus 5 (Fast)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-5.5", DisplayName: "Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-opus-5.5-fast", DisplayName: "Claude Opus 5.5 (Fast)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-4", DisplayName: "Claude Sonnet 4", ContextWindow: 1000000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-4.5", DisplayName: "Claude Sonnet 4.5", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-4.6", DisplayName: "Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "anthropic/claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "arcee-ai/trinity-large-thinking", DisplayName: "Trinity Large Thinking", ContextWindow: 262100, MaxOutputTokens: 80000, InputTypes: inputTextOnly},
	{ID: "bytedance/seed-1.6", DisplayName: "Seed 1.6", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "bytedance/seed-1.8", DisplayName: "Bytedance Seed 1.8", ContextWindow: 256000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "bytedance/seed-2.1-turbo", DisplayName: "Seed 2.1 Turbo", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "cohere/command-a", DisplayName: "Command A", ContextWindow: 256000, MaxOutputTokens: 8000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-r1", DisplayName: "DeepSeek-R1", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v3.1", DisplayName: "DeepSeek V3.1", ContextWindow: 163840, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v3.1-terminus", DisplayName: "DeepSeek V3.1 Terminus", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v3.2", DisplayName: "DeepSeek V3.2", ContextWindow: 128000, MaxOutputTokens: 8000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v3.2-thinking", DisplayName: "DeepSeek V3.2 Thinking", ContextWindow: 128000, MaxOutputTokens: 8000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4-flash-vision-exp", DisplayName: "DeepSeek V4 Flash Vision Exp", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "deepseek/deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek/deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3-flash", DisplayName: "Gemini 3 Flash", ContextWindow: 1000000, MaxOutputTokens: 65000, InputTypes: inputTextImage},
	{ID: "google/gemini-3.1-flash-lite", DisplayName: "Gemini 3.1 Flash Lite", ContextWindow: 1000000, MaxOutputTokens: 65000, InputTypes: inputTextImage},
	{ID: "google/gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro Preview", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "google/gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "google/gemini-3.5-flash-lite", DisplayName: "Gemini 3.5 Flash Lite", ContextWindow: 1000000, MaxOutputTokens: 65000, InputTypes: inputTextImage},
	{ID: "google/gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "google/gemini-3.7-flash", DisplayName: "Gemini 3.7 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "google/gemma-4-26b-a4b-it", DisplayName: "Google Gemma 4 26B A4B", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "google/gemma-4-31b-it", DisplayName: "Gemma 4 31B IT", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "inception/mercury-2", DisplayName: "Mercury 2", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "inception/mercury-2.5", DisplayName: "Mercury 2.5", ContextWindow: 260000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "inception/mercury-coder-small", DisplayName: "Mercury Coder Small Beta", ContextWindow: 32000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash", DisplayName: "Ling 3.0 Flash", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-fin", DisplayName: "Ling 3.0 Flash Fin", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-fin-free", DisplayName: "Ling 3.0 Flash Fin (Free)", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-sante", DisplayName: "Ling 3.0 Flash Sante", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-sante-free", DisplayName: "Ling 3.0 Flash Sante (Free)", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "inclusionai/ling-3.0-flash-vl", DisplayName: "Ling 3.0 Flash VL", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "inclusionai/ling-3.0-flash-vl-free", DisplayName: "Ling 3.0 Flash VL (Free)", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "interfaze/interfaze-beta", DisplayName: "Interfaze Beta", ContextWindow: 1000000, MaxOutputTokens: 32000, InputTypes: inputTextImage},
	{ID: "meta/llama-3.1-70b", DisplayName: "Llama 3.1 70B Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "meta/llama-3.1-8b", DisplayName: "Llama 3.1 8B Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "meta/llama-3.3-70b", DisplayName: "Llama 3.3 70B Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "meta/llama-4-maverick", DisplayName: "Llama 4 Maverick 17B Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "meta/llama-4-scout", DisplayName: "Llama 4 Scout 17B Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "meta/muse-glimmer-30b", DisplayName: "Muse Glimmer 30B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.1", DisplayName: "Muse Spark 1.1", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.2", DisplayName: "Muse Spark 1.2", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.2-contributor", DisplayName: "Muse Spark 1.2 Contributor", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.3", DisplayName: "Muse Spark 1.3", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "meta/muse-spark-1.3-contributor", DisplayName: "Muse Spark 1.3 Contributor", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
	{ID: "minimax/minimax-m2", DisplayName: "MiniMax M2", ContextWindow: 205000, MaxOutputTokens: 205000, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.1", DisplayName: "MiniMax M2.1", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.1-lightning", DisplayName: "MiniMax M2.1 Lightning", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.5", DisplayName: "MiniMax M2.5", ContextWindow: 204800, MaxOutputTokens: 131000, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.5-highspeed", DisplayName: "MiniMax M2.5 High Speed", ContextWindow: 204800, MaxOutputTokens: 131000, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.7", DisplayName: "MiniMax M2.7", ContextWindow: 204800, MaxOutputTokens: 131000, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m2.7-highspeed", DisplayName: "MiniMax M2.7 High Speed", ContextWindow: 204800, MaxOutputTokens: 131100, InputTypes: inputTextOnly},
	{ID: "minimax/minimax-m3", DisplayName: "MiniMax M3", ContextWindow: 512000, MaxOutputTokens: 512000, InputTypes: inputTextImage},
	{ID: "mistral/codestral", DisplayName: "Mistral Codestral", ContextWindow: 128000, MaxOutputTokens: 4000, InputTypes: inputTextOnly},
	{ID: "mistral/ministral-14b", DisplayName: "Ministral 14B", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "mistral/ministral-3b", DisplayName: "Ministral 3B", ContextWindow: 131072, MaxOutputTokens: 4000, InputTypes: inputTextImage},
	{ID: "mistral/ministral-8b", DisplayName: "Ministral 8B", ContextWindow: 262144, MaxOutputTokens: 4000, InputTypes: inputTextImage},
	{ID: "mistral/mistral-large-3", DisplayName: "Mistral Large 3", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "mistral/mistral-medium-3.5", DisplayName: "Mistral Medium Latest", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "mistral/mistral-nemo", DisplayName: "Mistral Nemo 12B", ContextWindow: 60288, MaxOutputTokens: 16000, InputTypes: inputTextOnly},
	{ID: "mistral/mistral-small", DisplayName: "Mistral Small", ContextWindow: 262144, MaxOutputTokens: 4000, InputTypes: inputTextImage},
	{ID: "mixedbread/toast-1", DisplayName: "Toast 1", ContextWindow: 131000, MaxOutputTokens: 4000, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2", DisplayName: "Kimi K2 Instruct", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2-thinking", DisplayName: "Kimi K2 Thinking", ContextWindow: 216144, MaxOutputTokens: 216144, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2.5", DisplayName: "Kimi K2.5", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 256000, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k2.7-code-highspeed", DisplayName: "Kimi K2.7 Code High Speed", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k3-fast", DisplayName: "Kimi K3 Fast", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "nvidia/nemotron-3-nano-30b-a3b", DisplayName: "Nemotron 3 Nano 30B A3B", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-super-120b-a12b", DisplayName: "NVIDIA Nemotron 3 Super 120B A12B", ContextWindow: 256000, MaxOutputTokens: 32000, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b", DisplayName: "Nemotron 3 Ultra", ContextWindow: 1000000, MaxOutputTokens: 65000, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3.5-lightning", DisplayName: "Nemotron 3.5 Lightning 30B", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-nano-12b-v2-vl", DisplayName: "Nvidia Nemotron Nano 12B V2 VL", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "nvidia/nemotron-nano-9b-v2", DisplayName: "Nvidia Nemotron Nano 9B V2", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "openai/gpt-3.5-turbo", DisplayName: "GPT-3.5 Turbo", ContextWindow: 16385, MaxOutputTokens: 4096, InputTypes: inputTextOnly},
	{ID: "openai/gpt-4-turbo", DisplayName: "GPT-4 Turbo", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1", DisplayName: "GPT-4.1", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-fast", DisplayName: "GPT-4.1 (Fast)", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-mini", DisplayName: "GPT-4.1 mini", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-mini-fast", DisplayName: "GPT-4.1 mini (Fast)", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-nano", DisplayName: "GPT-4.1 nano", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4.1-nano-fast", DisplayName: "GPT-4.1 nano (Fast)", ContextWindow: 1047576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o", DisplayName: "GPT-4o", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-fast", DisplayName: "GPT-4o (Fast)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-mini", DisplayName: "GPT-4o mini", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-4o-mini-fast", DisplayName: "GPT-4o mini (Fast)", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "openai/gpt-5", DisplayName: "GPT-5", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-codex", DisplayName: "GPT-5-Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-fast", DisplayName: "GPT-5 (Fast)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-mini", DisplayName: "GPT-5 mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-mini-fast", DisplayName: "GPT-5 mini (Fast)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-nano", DisplayName: "GPT-5 nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5-pro", DisplayName: "GPT-5 pro", ContextWindow: 400000, MaxOutputTokens: 272000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1-codex", DisplayName: "GPT-5.1-Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1-codex-max", DisplayName: "GPT 5.1 Codex Max", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1-codex-mini", DisplayName: "GPT 5.1 Codex Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1-thinking", DisplayName: "GPT 5.1 Thinking", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.1-thinking-fast", DisplayName: "GPT 5.1 Thinking (Fast)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2", DisplayName: "GPT 5.2", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2-codex", DisplayName: "GPT 5.2 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2-fast", DisplayName: "GPT 5.2 (Fast)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.2-pro", DisplayName: "GPT 5.2", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.3-codex", DisplayName: "GPT 5.3 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.3-codex-fast", DisplayName: "GPT 5.3 Codex (Fast)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4", DisplayName: "GPT 5.4", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-fast", DisplayName: "GPT 5.4 (Fast)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-mini", DisplayName: "GPT 5.4 Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-mini-fast", DisplayName: "GPT 5.4 Mini (Fast)", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-nano", DisplayName: "GPT 5.4 Nano", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.4-pro", DisplayName: "GPT 5.4 Pro", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.5", DisplayName: "GPT 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.5-fast", DisplayName: "GPT 5.5 (Fast)", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.5-pro", DisplayName: "GPT 5.5 Pro", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-luna", DisplayName: "GPT 5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-luna-fast", DisplayName: "GPT 5.6 Luna (Fast)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-sol", DisplayName: "GPT 5.6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-sol-fast", DisplayName: "GPT 5.6 Sol (Fast)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-terra", DisplayName: "GPT 5.6 Terra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-5.6-terra-fast", DisplayName: "GPT 5.6 Terra (Fast)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-astra-fast", DisplayName: "GPT-6 Astra (Fast)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-luna", DisplayName: "GPT-6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-luna-fast", DisplayName: "GPT-6 Luna (Fast)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-sol", DisplayName: "GPT-6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-6-sol-fast", DisplayName: "GPT-6 Sol (Fast)", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-safeguard-120b", DisplayName: "GPT OSS Safeguard 120B", ContextWindow: 128000, MaxOutputTokens: 16000, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-safeguard-20b", DisplayName: "GPT OSS Safeguard 20B", ContextWindow: 128000, MaxOutputTokens: 16000, InputTypes: inputTextOnly},
	{ID: "openai/o1", DisplayName: "o1", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o3", DisplayName: "o3", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o3-fast", DisplayName: "o3 (Fast)", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o3-mini", DisplayName: "o3-mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextOnly},
	{ID: "openai/o3-pro", DisplayName: "o3 Pro", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o4-mini", DisplayName: "o4-mini", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "openai/o4-mini-fast", DisplayName: "o4-mini (Fast)", ContextWindow: 200000, MaxOutputTokens: 100000, InputTypes: inputTextImage},
	{ID: "poolside/laguna-s-2.1", DisplayName: "Laguna S 2.1", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "poolside/laguna-s-2.1-free", DisplayName: "Laguna S 2.1 Free", ContextWindow: 256000, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "quiverai/arrow-2", DisplayName: "Arrow 2", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "quiverai/arrow-2-telos", DisplayName: "Arrow 2 Telos", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "sakana/fugu-max", DisplayName: "Fugu Max", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextImage},
	{ID: "sakana/fugu-ultra", DisplayName: "Fugu Ultra", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextImage},
	{ID: "sakana/fugu-ultra-v2", DisplayName: "Fugu Ultra v2", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextImage},
	{ID: "sakana/namazu", DisplayName: "Sakana Namazu", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.1-fast-non-reasoning", DisplayName: "Grok 4.1 Fast Non-Reasoning", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.1-fast-reasoning", DisplayName: "Grok 4.1 Fast Reasoning", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.20-multi-agent", DisplayName: "Grok 4.20 Multi-Agent", ContextWindow: 2000000, MaxOutputTokens: 2000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.20-multi-agent-beta", DisplayName: "Grok 4.20 Multi Agent Beta", ContextWindow: 2000000, MaxOutputTokens: 2000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.20-non-reasoning", DisplayName: "Grok 4.20 Non-Reasoning", ContextWindow: 2000000, MaxOutputTokens: 2000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.20-non-reasoning-beta", DisplayName: "Grok 4.20 Beta Non-Reasoning", ContextWindow: 2000000, MaxOutputTokens: 2000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.20-reasoning", DisplayName: "Grok 4.20 Reasoning", ContextWindow: 2000000, MaxOutputTokens: 2000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.20-reasoning-beta", DisplayName: "Grok 4.20 Beta Reasoning", ContextWindow: 2000000, MaxOutputTokens: 2000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.3", DisplayName: "Grok 4.3", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.5", DisplayName: "Grok 4.5", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.6", DisplayName: "Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-4.7", DisplayName: "Grok 4.7", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "spacexai/grok-build-0.1", DisplayName: "Grok Build 0.1", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "stepfun/step-3.5-flash", DisplayName: "StepFun 3.5 Flash", ContextWindow: 262114, MaxOutputTokens: 262114, InputTypes: inputTextImage},
	{ID: "stepfun/step-3.7-flash", DisplayName: "Step 3.7 Flash", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "tencent/hy3", DisplayName: "Hy3", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "tencent/hy4-preview", DisplayName: "Tencent Hy4 Preview", ContextWindow: 1024000, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "thinkingmachines/inkling", DisplayName: "Inkling", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "thinkingmachines/inkling-small", DisplayName: "Inkling Small", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextImage},
	{ID: "xiaomi/mimo-v2.5", DisplayName: "MiMo M2.5", ContextWindow: 1050000, MaxOutputTokens: 131100, InputTypes: inputTextImage},
	{ID: "xiaomi/mimo-v2.5-pro", DisplayName: "MiMo V2.5 Pro", ContextWindow: 1050000, MaxOutputTokens: 131000, InputTypes: inputTextOnly},
	{ID: "xiaomi/mimo-v2.6-flash", DisplayName: "MiMo V2.6 Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "xiaomi/mimo-v2.6-pro", DisplayName: "MiMo V2.6 Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "xiaomi/mimo-v2.6-pro-ultraspeed", DisplayName: "MiMo V2.6 Pro UltraSpeed", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "zai/glm-4.5", DisplayName: "GLM 4.5", ContextWindow: 128000, MaxOutputTokens: 96000, InputTypes: inputTextOnly},
	{ID: "zai/glm-4.5-air", DisplayName: "GLM 4.5 Air", ContextWindow: 128000, MaxOutputTokens: 96000, InputTypes: inputTextOnly},
	{ID: "zai/glm-4.5v", DisplayName: "GLM 4.5V", ContextWindow: 66000, MaxOutputTokens: 16000, InputTypes: inputTextImage},
	{ID: "zai/glm-4.6", DisplayName: "GLM 4.6", ContextWindow: 200000, MaxOutputTokens: 96000, InputTypes: inputTextOnly},
	{ID: "zai/glm-4.7", DisplayName: "GLM 4.7", ContextWindow: 200000, MaxOutputTokens: 120000, InputTypes: inputTextOnly},
	{ID: "zai/glm-4.7-flash", DisplayName: "GLM 4.7 Flash", ContextWindow: 200000, MaxOutputTokens: 131000, InputTypes: inputTextOnly},
	{ID: "zai/glm-4.7-flashx", DisplayName: "GLM 4.7 FlashX", ContextWindow: 200000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "zai/glm-5", DisplayName: "GLM 5", ContextWindow: 202800, MaxOutputTokens: 131100, InputTypes: inputTextOnly},
	{ID: "zai/glm-5-turbo", DisplayName: "GLM 5 Turbo", ContextWindow: 202800, MaxOutputTokens: 131100, InputTypes: inputTextOnly},
	{ID: "zai/glm-5.1", DisplayName: "GLM 5.1", ContextWindow: 202800, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "zai/glm-5.2", DisplayName: "GLM 5.2", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "zai/glm-5.2-fast", DisplayName: "GLM 5.2 Fast", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "zai/glm-5.3", DisplayName: "GLM 5.3", ContextWindow: 1000000, MaxOutputTokens: 1000000, InputTypes: inputTextOnly},
	{ID: "zai/glm-5.3-fast", DisplayName: "GLM 5.3 Fast", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "zai/glm-5.3-flash", DisplayName: "GLM 5.3 Flash", ContextWindow: 1000000, MaxOutputTokens: 131000, InputTypes: inputTextImage},
	{ID: "zai/glm-5.3-flashx", DisplayName: "GLM 5.3 FlashX", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "zai/glm-5v-turbo", DisplayName: "GLM 5V Turbo", ContextWindow: 200000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
}
