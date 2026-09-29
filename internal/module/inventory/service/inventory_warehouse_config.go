// inventory_warehouse_config.go — 仓库类型与第三方对接配置（迁移 240）。
//
// 产品决策：仓库不止「默认虚拟仓」一种 —— 需要类型（自营 / 第三方 / 虚拟），
// 第三方仓各家的对接方式不同，要能在库里存**每家不一样**的配置
// （对接方、外部仓代码、地址联系人、凭据，以及将来扩展的任意字段）。
//
// 凭据安全（本文件的死线）：
//
//  1. 明文既不落库也不回显。写入方向收到的明文凭据立刻用 pkg/crypto 的 AES-256-GCM
//     加密（密钥 = 装配期从 config 读入的 app.secret），config 里只留密文；
//     出参方向一律只给掩码 **** 与「配没配」的布尔值（见 warehouseThirdPartyResp）。
//  2. 没有可用加密能力时**不退回明文**：要么报 ErrWarehouseCredentialKeyMissing 让人把
//     app.secret 配好，要么改用引用名（secretRef）—— 系统只记「值在别处叫什么」，
//     值由部署方在进程环境里配置。
//  3. 「未改动则不覆盖」：后台回显的是掩码，运营只改地址联系人时直接保存，
//     不能把已经配好的凭据清掉；显式清除要走 ClearCredential。
//  4. 回显时按**键名**再过一道敏感键过滤：即便有人手工往 config 里写了明文 apiKey，
//     它也不会从这里流出去（防御性收口，见 sensitiveConfigKeys）。
package inventoryservice

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/pkg/crypto"
)

// maxCredentialLen 凭据长度上限（密钥是配置值，不该是半篇文档）。
const maxCredentialLen = 512

// normalizeWarehouseType 归一仓库类型：空串按 self（存量语义：不加类型就是自营仓）。
func normalizeWarehouseType(raw string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return inventoryenums.WarehouseTypeSelf, nil
	case inventoryenums.WarehouseTypeSelf:
		return inventoryenums.WarehouseTypeSelf, nil
	case inventoryenums.WarehouseTypeThirdParty:
		return inventoryenums.WarehouseTypeThirdParty, nil
	case inventoryenums.WarehouseTypeVirtual:
		return inventoryenums.WarehouseTypeVirtual, nil
	default:
		return "", errors.New(inventoryenums.ErrWarehouseTypeInvalid)
	}
}

// knownConfigKeys config 里由本模块识别的固定键（小写比较）。
var knownConfigKeys = map[string]bool{
	inventoryenums.WarehouseConfigKeyProvider:       true,
	inventoryenums.WarehouseConfigKeyExternalCode:   true,
	inventoryenums.WarehouseConfigKeyAddress:        true,
	inventoryenums.WarehouseConfigKeyContact:        true,
	inventoryenums.WarehouseConfigKeyAllowsShipping: true,
	inventoryenums.WarehouseConfigKeyAPICipher:      true,
	inventoryenums.WarehouseConfigKeySecretRef:      true,
}

// sensitiveConfigKeys 一律不得回显的 config 键（小写比较）。
//
// 名单刻意比「本模块会写的那几个键」宽：运营手工改库、历史数据、将来别的适配器写进来的
// 明文凭据都可能在 config 里，回显口子不按当前写入口径收窄。
var sensitiveConfigKeys = map[string]bool{
	"apikeycipher": true, "apikey": true, "api_key": true, "apisecret": true,
	"credential": true, "credentials": true, "password": true, "passwd": true,
	"secret": true, "token": true, "accesstoken": true, "access_token": true,
}

// isSensitiveConfigKey 判断 config 键是否属于「不得回显」的敏感键。
func isSensitiveConfigKey(key string) bool {
	return sensitiveConfigKeys[strings.ToLower(strings.TrimSpace(key))]
}

// decodeConfigObject 把 config jsonb 解成 map：非对象 / 空值一律退化成空对象。
func decodeConfigObject(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return out
	}
	obj, ok := probe.(map[string]any)
	if !ok {
		return out
	}
	return obj
}

// encodeConfigObject 把 map 编回 jsonb（编码失败时退化成空对象，不阻断写入）。
func encodeConfigObject(cfg map[string]any) (json.RawMessage, error) {
	if len(cfg) == 0 {
		return json.RawMessage("{}"), nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return json.RawMessage("{}"), nil
	}
	return raw, nil
}

// hasThirdPartyFields 入参里是否带了任何第三方配置字段。
func hasThirdPartyFields(in *inventorydto.WarehouseThirdPartyReq) bool {
	if in == nil {
		return false
	}
	return strings.TrimSpace(in.Provider) != "" || strings.TrimSpace(in.ExternalCode) != "" ||
		strings.TrimSpace(in.Address) != "" || strings.TrimSpace(in.Contact) != "" ||
		strings.TrimSpace(in.APICredential) != "" || strings.TrimSpace(in.SecretRef) != "" ||
		in.AllowsShipping || in.ClearCredential
}

// applyThirdPartyConfig 把入参的第三方配置合并进 config jsonb。
//
// 合并而不是整体替换：config 允许承载「将来扩展的任意字段」，整体替换会把它们抹掉。
// 已知键按入参覆盖，未识别的键原样保留。
func (s *Service) applyThirdPartyConfig(typ string, in *inventorydto.WarehouseThirdPartyReq,
	old json.RawMessage) (out json.RawMessage, err error) {
	cfg := decodeConfigObject(old)
	if in == nil {
		return encodeConfigObject(cfg)
	}
	if typ != inventoryenums.WarehouseTypeThirdParty {
		// 非第三方仓不接受对接配置：类型是自营却存着别人家的凭据，是将来最容易出事的
		// 那种「看起来配了、其实不生效」的配置。既有值不清空（类型可来回切换）。
		if hasThirdPartyFields(in) {
			return nil, errors.New(inventoryenums.ErrWarehouseConfigInvalid)
		}
		return encodeConfigObject(cfg)
	}
	cfg[inventoryenums.WarehouseConfigKeyProvider] = strings.TrimSpace(in.Provider)
	cfg[inventoryenums.WarehouseConfigKeyExternalCode] = strings.TrimSpace(in.ExternalCode)
	cfg[inventoryenums.WarehouseConfigKeyAddress] = strings.TrimSpace(in.Address)
	cfg[inventoryenums.WarehouseConfigKeyContact] = strings.TrimSpace(in.Contact)
	cfg[inventoryenums.WarehouseConfigKeyAllowsShipping] = in.AllowsShipping
	if err = s.applyCredential(cfg, in); err != nil {
		return nil, err
	}
	return encodeConfigObject(cfg)
}

// applyCredential 处理凭据三个动作：清除 / 写入 / 保持不变。
func (s *Service) applyCredential(cfg map[string]any, in *inventorydto.WarehouseThirdPartyReq) (err error) {
	if in.ClearCredential {
		delete(cfg, inventoryenums.WarehouseConfigKeyAPICipher)
		delete(cfg, inventoryenums.WarehouseConfigKeySecretRef)
		return nil
	}
	plain := strings.TrimSpace(in.APICredential)
	// 掩码等于「没改」：后台回显的是 ****，运营只改地址联系人时它随表单一起提交回来。
	if plain != "" && plain != inventoryenums.CredentialMask {
		if verr := validateCredential(plain); verr != nil {
			return verr
		}
		if strings.TrimSpace(s.cipherSecret) == "" {
			// 没有密钥就**不写**：宁可让运营显式报错，也不能把明文落进 jsonb。
			return errors.New(inventoryenums.ErrWarehouseCredentialKeyMissing)
		}
		cipherText, cerr := crypto.Encrypt(plain, s.cipherSecret)
		if cerr != nil {
			return errors.New(inventoryenums.ErrWarehouseCredentialInvalid)
		}
		cfg[inventoryenums.WarehouseConfigKeyAPICipher] = cipherText
		delete(cfg, inventoryenums.WarehouseConfigKeySecretRef)
		return nil
	}
	if ref := strings.TrimSpace(in.SecretRef); ref != "" {
		if strings.ContainsRune(ref, 0) || len(ref) > maxCredentialLen {
			return errors.New(inventoryenums.ErrWarehouseCredentialInvalid)
		}
		cfg[inventoryenums.WarehouseConfigKeySecretRef] = ref
		delete(cfg, inventoryenums.WarehouseConfigKeyAPICipher)
		return nil
	}
	// 两者都空：保持不变（未改动则不覆盖）。
	return nil
}

// validateCredential 凭据明文的形状校验（长度 + 不含控制字符 / NUL）。
func validateCredential(plain string) error {
	if len(plain) > maxCredentialLen {
		return errors.New(inventoryenums.ErrWarehouseCredentialInvalid)
	}
	for _, r := range plain {
		if r == 0 || (unicode.IsControl(r) && r != '\t') {
			return errors.New(inventoryenums.ErrWarehouseCredentialInvalid)
		}
	}
	return nil
}

// warehouseThirdPartyResp config jsonb → 出参（**结构里没有凭据本体的位置**）。
func warehouseThirdPartyResp(raw json.RawMessage) *inventorydto.WarehouseThirdPartyResp {
	cfg := decodeConfigObject(raw)
	resp := &inventorydto.WarehouseThirdPartyResp{
		Provider:       stringOf(cfg[inventoryenums.WarehouseConfigKeyProvider]),
		ExternalCode:   stringOf(cfg[inventoryenums.WarehouseConfigKeyExternalCode]),
		Address:        stringOf(cfg[inventoryenums.WarehouseConfigKeyAddress]),
		Contact:        stringOf(cfg[inventoryenums.WarehouseConfigKeyContact]),
		AllowsShipping: boolOf(cfg[inventoryenums.WarehouseConfigKeyAllowsShipping]),
		SecretRef:      stringOf(cfg[inventoryenums.WarehouseConfigKeySecretRef]),
	}
	extras := map[string]any{}
	for key, value := range cfg {
		lower := strings.ToLower(strings.TrimSpace(key))
		if knownConfigKeys[lower] {
			if lower == inventoryenums.WarehouseConfigKeyAPICipher {
				resp.HasCredential = true
			}
			continue
		}
		// 未识别的键原样带回（将来扩展的任意字段），但敏感键一律只反映「存在」，不回显值。
		if isSensitiveConfigKey(lower) {
			resp.HasCredential = true
			continue
		}
		extras[key] = value
	}
	if resp.SecretRef != "" {
		resp.HasCredential = true
	}
	if resp.HasCredential {
		resp.CredentialMasked = inventoryenums.CredentialMask
	}
	if len(extras) > 0 {
		resp.Extras = extras
	}
	return resp
}

// stringOf / boolOf 松类型取值（jsonb 里的值不一定还是当初写进去的类型）。
func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}
