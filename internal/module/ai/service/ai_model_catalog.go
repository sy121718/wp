// ai_model_catalog.go — 模型目录：内置预设的读取、目录解析 / 归一 / 合并、拉取可用模型。
//
// 三条口径：
//
//	· **内置清单是随版本发布的静态预设**（按 provider_key 索引，数据在 catalog_data*.go）：内置的是「这家供应商大致有哪些模型」，
//	  不是权威清单 —— 所以「恢复默认模型」是显式动作，缺清单时明确报错而不是静默清空。
//	· **目录形状由代码白名单约束**：只认 config_data.models（元素形状见 dto.ModelEntry），
//	  解析失败一律回退（跳过坏行 / 回退空目录），写回时保留 config_data 的其它键。
//	· **拉取走 SSRF 防护口径**（ssrf.go），地址每次出站都重新校验。
package aiservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
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
