// ai_provider_crud.go — 供应商的增删改查用例（含密钥加密与乐观锁）。
//
// 写操作的「读-改-写」全部落在 model 的 version 条件下（见 updateWithVersion）：
// 两个管理员同时改同一个供应商时，后提交者拿到「已被其他人修改」而不是静默覆盖。
package aiservice

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
)

// ListProviders 列出全部供应商（按 sort 升序）。
func (s *Service) ListProviders(ctx context.Context) (res []aidto.Provider, err error) {
	rows, err := s.m.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]aidto.Provider, 0, len(rows))
	for i := range rows {
		out = append(out, s.providerOf(&rows[i]))
	}
	return out, nil
}

// GetProvider 按主键取一个供应商；不存在返回 ErrProviderNotFound。
func (s *Service) GetProvider(ctx context.Context, id int64) (res *aidto.Provider, err error) {
	if id <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	e, err := s.m.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, ErrProviderNotFound
	}
	out := s.providerOf(e)
	return &out, nil
}

// SaveProvider 新建（ID == 0）或按版本号更新。
//
// 两条产品语义落在这里：
//
//	· ProviderKey 只在新建立时生效，更新时**忽略**同名字段 —— 内置默认模型清单按
//	  provider_key 索引（「恢复默认模型」的语义就是「回到这个 key 的内置清单」），
//	  允许改 key 会让一个供应商的目录语义凭空漂移。
//	· APIKey 为空 = 不改动已存的密钥（页面占位文案「已配置 —— 输入新值可替换」）；
//	  非空 = 加密后覆盖。想换密钥就填新值，不填就保持原样。
//	· **改 API 地址即作废旧密钥**（安全补偿控制，见 511 表头注释与 docs/16 的 ADR）：
//	  地址变更时若原本有密钥、这次却没给新值 → 直接拒绝（ErrAPIKeyRequiredOnBaseURLChange，
//	  要求重填）；原本就没密钥 → 写空。旧密钥是「发给这个地址」的凭据，让它跟着新地址
//	  走，等于允许把已存密钥发往请求方自选的任意主机（fetch 的 Authorization 头）。
func (s *Service) SaveProvider(ctx context.Context, req *aidto.SaveProviderReq) (res *aidto.Provider, err error) {
	if req == nil {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		return nil, errors.New(aienums.ErrDisplayNameRequired)
	}
	protocol := strings.TrimSpace(req.Protocol)
	if protocol == "" {
		protocol = aienums.ProtocolOpenAIChatCompletions
	}
	if !aienums.IsSupportedProtocol(protocol) {
		return nil, errors.New(aienums.ErrProtocolUnsupported)
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	// Status 未表态 = 启用（与 ai_provider.status 的 DEFAULT 1 同一口径）。
	status := aienums.StatusEnabled
	if req.Status != nil {
		status = normalizeStatus(*req.Status)
	}

	if req.ID == 0 {
		key := strings.ToLower(strings.TrimSpace(req.ProviderKey))
		if key == "" {
			return nil, errors.New(aienums.ErrProviderKeyRequired)
		}
		dup, derr := s.m.FindByKey(ctx, key)
		if derr != nil {
			return nil, derr
		}
		if dup != nil {
			return nil, ErrProviderKeyExists
		}
		cipherText, cerr := s.encryptSecret(req.APIKey)
		if cerr != nil {
			return nil, cerr
		}
		entity := &aimodel.AIProviderEntity{
			ProviderKey:  key,
			DisplayName:  name,
			BaseURL:      baseURL,
			Protocol:     protocol,
			APIKeyCipher: cipherText,
			Status:       status,
			Sort:         req.Sort,
			ConfigData:   aimodel.JSONMap{},
			Version:      1,
			CreateBy:     req.UpdateBy,
			UpdateBy:     req.UpdateBy,
		}
		if err = s.m.Create(ctx, entity); err != nil {
			// FindByKey 是「先查后插」（TOCTOU），并发提交同一 provider_key 时查不出重复，
			// 只能靠唯一约束兜底 —— 把 PG 23505 / gorm.ErrDuplicatedKey 归口成明确的
			// 业务错误，否则用户看到的是「服务器内部错误」。
			if isDuplicateKeyErr(err) {
				return nil, ErrProviderKeyExists
			}
			return nil, err
		}
		out := s.providerOf(entity)
		return &out, nil
	}

	current, err := s.m.FindByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	fields := map[string]any{
		"display_name": name,
		"base_url":     baseURL,
		"protocol":     protocol,
		"status":       status,
		"sort":         req.Sort,
	}
	cipherText, cerr := s.encryptSecret(req.APIKey)
	if cerr != nil {
		return nil, cerr
	}
	// 地址变更即作废旧密钥：旧凭据不能跟着新地址出站（见函数头注释）。
	baseURLChanged := baseURL != strings.TrimSpace(current.BaseURL)
	if cipherText != "" {
		fields["api_key_cipher"] = cipherText
	} else if baseURLChanged {
		if strings.TrimSpace(current.APIKeyCipher) != "" {
			return nil, ErrAPIKeyRequiredOnBaseURLChange
		}
		fields["api_key_cipher"] = ""
	}
	if err = s.updateWithVersion(ctx, req.ID, req.Version, fields, req.UpdateBy); err != nil {
		return nil, err
	}
	return s.GetProvider(ctx, req.ID)
}

// DeleteProvider 删除供应商（模型目录随行一并消失）。
//
// 幂等口径：行不存在返回 ErrProviderNotFound（而不是静默成功）—— 页面上的「删除」
// 是个明确动作，删不掉要说清楚，否则用户会以为删了。
func (s *Service) DeleteProvider(ctx context.Context, id int64) (err error) {
	if id <= 0 {
		return errors.New(aienums.ErrInvalidParam)
	}
	rows, derr := s.m.DeleteByID(ctx, id)
	if derr != nil {
		return derr
	}
	if rows == 0 {
		return ErrProviderNotFound
	}
	return nil
}

// SetProviderStatus 启停一个供应商（卡片上的状态点）。
//
// 已是目标状态时直接回读（幂等，不推进版本号）：避免「重复点两下启停」把版本号推高、
// 让同时打开的编辑页无谓冲突。
func (s *Service) SetProviderStatus(ctx context.Context, req *aidto.SetStatusReq) (res *aidto.Provider, err error) {
	if req == nil || req.ID <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	current, err := s.m.FindByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	status := normalizeStatus(req.Status)
	if current.Status == status {
		return s.GetProvider(ctx, req.ID)
	}
	if err = s.updateWithVersion(ctx, req.ID, req.Version, map[string]any{"status": status}, req.UpdateBy); err != nil {
		return nil, err
	}
	return s.GetProvider(ctx, req.ID)
}

// normalizeStatus 状态归一：只认 0 / 1，其余值一律按停用处理（fail-closed）。
func normalizeStatus(v int) int {
	if v == aienums.StatusEnabled {
		return aienums.StatusEnabled
	}
	return aienums.StatusDisabled
}

// isDuplicateKeyErr 判「唯一约束冲突」（并发新建同一 provider_key）。
//
// 双判据：优先认 gorm 的翻译错误（连接开启了 TranslateError 时），
// 否则回退到 PG 原始错误文本（SQLSTATE 23505）—— 两种装配下都能归口。
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
}
