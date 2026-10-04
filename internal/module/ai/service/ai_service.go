// ai_service.go — ai 模块的 Service 与共享面。
//
// Service 只持本模块 model 与出站 HTTP 客户端：**不持 *gorm.DB**（AGENTS /
// internal/module/CLAUDE.md 的硬约束，由 scripts/check-service-db-boundary.sh 拦截）。
//
// 密钥口径：明文只在两个瞬间存在 —— ①调用方传进来待加密的 SaveProviderReq.APIKey，
// ②拉取可用模型时解密后放进 Authorization 头。其余任何时候对外只有 HasAPIKey 布尔。
package aiservice

import (
	"context"
	"errors"
	"net/http"
	"strings"

	aicontract "go_wp/internal/module/ai/contract"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/utils"
)

// Service AI 供应商 / 模型配置的服务实现。
type Service struct {
	m            *aimodel.Model
	cipherSecret string
	client       *http.Client
	// calls 调用流水（ai_call_log）的写入端口，装配期注入；未注入时 Chat 照常工作、不记流水。
	// 详见 ai_call_log.go 的 logCallAsync（协程 + 脱离请求 ctx + panic 不外溢）。
	calls CallLogWriter
}

// NewService 构造服务（client 用受限的 ai 出站客户端，见 ai_client.go）。
func NewService(m *aimodel.Model) *Service {
	return &Service{m: m, client: newAIClient()}
}

// SetCipherSecret 注入密钥加密口令（装配期从 app.secret 取，不新造配置项）。
func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = strings.TrimSpace(secret) }

// SetHTTPClient 注入自定义客户端（测试用；nil 忽略）。
func (s *Service) SetHTTPClient(c *http.Client) {
	if c != nil {
		s.client = c
	}
}

// 编译期断言：Service 实现契约。
var _ aicontract.AIService = (*Service)(nil)

// 语义化哨兵错误。
//
// 值是 enums 的 **i18n key**（不是中文原文）：这些错误要么被页面按白名单翻成中文兜底，
// 要么经 pkg/response.ErrorAuto 直接回给接口调用方 —— 中文原文会被判成内部错误（500）。
var (
	ErrProviderNotFound  = errors.New(aienums.ErrProviderNotFound)
	ErrVersionConflict   = errors.New(aienums.ErrVersionConflict)
	ErrCipherUnavailable = errors.New(aienums.ErrCipherUnavailable)
	ErrNoBuiltinModels   = errors.New(aienums.ErrNoBuiltinModels)
	ErrBaseURLRequired   = errors.New(aienums.ErrBaseURLRequired)
	ErrModelsFetchFailed = errors.New(aienums.ErrModelsFetchFailed)
	// ErrProviderKeyExists 供应商标识重复（先查后插漏网时由唯一约束兜底归口到这里）。
	ErrProviderKeyExists = errors.New(aienums.ErrProviderKeyExists)
	// ErrAPIKeyRequiredOnBaseURLChange 改了 API 地址却没给新密钥：旧凭据不能跟着新地址出站。
	ErrAPIKeyRequiredOnBaseURLChange = errors.New(aienums.ErrAPIKeyRequiredOnBaseURLChange)
)

// encryptSecret 把明文密钥加密成落库密文；空串表示「不改动已存的密钥」，直接回空。
func (s *Service) encryptSecret(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", nil
	}
	if s.cipherSecret == "" {
		return "", ErrCipherUnavailable
	}
	return crypto.Encrypt(plain, s.cipherSecret)
}

// decryptSecret 解开落库密文；空密文回空串（不报错：没有密钥是合法状态）。
func (s *Service) decryptSecret(cipherText string) (string, error) {
	if strings.TrimSpace(cipherText) == "" {
		return "", nil
	}
	if s.cipherSecret == "" {
		return "", ErrCipherUnavailable
	}
	return crypto.Decrypt(cipherText, s.cipherSecret)
}

// providerOf 实体 → 对外形状（模型目录从 config_data 解析；解析失败回退空目录）。
func (s *Service) providerOf(e *aimodel.AIProviderEntity) aidto.Provider {
	return aidto.Provider{
		ID:          e.ID,
		ProviderKey: e.ProviderKey,
		DisplayName: e.DisplayName,
		BaseURL:     e.BaseURL,
		Protocol:    e.Protocol,
		Status:      e.Status,
		Sort:        e.Sort,
		Models:      parseModels(e.ConfigData),
		HasAPIKey:   strings.TrimSpace(e.APIKeyCipher) != "",
		Version:     e.Version,
		UpdateTime:  utils.NewJSONTime(e.UpdateTime),
	}
}

// updateWithVersion 带乐观锁的更新，并把「0 行」归因到具体错误。
//
// model 层刻意不区分「不存在」与「版本冲突」（它只回受影响行数），归因属于业务判断：
// 再读一次版本号，能读到 = 版本被别人推进过，读不到 = 行已经没了。
func (s *Service) updateWithVersion(ctx context.Context, id, version int64, fields map[string]any, updateBy int64) error {
	if version <= 0 {
		return errors.New(aienums.ErrVersionRequired)
	}
	rows, err := s.m.UpdateFields(ctx, id, version, fields, updateBy)
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}
	_, found, verr := s.m.VersionOf(ctx, id)
	if verr != nil {
		return verr
	}
	if !found {
		return ErrProviderNotFound
	}
	return ErrVersionConflict
}
