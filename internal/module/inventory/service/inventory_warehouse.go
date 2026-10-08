package inventoryservice

// 「必须有一个默认仓」是本文件维护的不变量：
//
//	· 工程内第一个仓自动成为默认仓（建仓即满足「未指定仓库时有兜底」）；
//	· 显式 IsDefault 即切换默认仓，同工程唯一（数据库部分唯一索引兜底并发）；
//	· 默认仓不能删除、不能停用、不能取消默认 —— 取消/删除的后果是
//	  「未指定仓库」再也找不到兜底，属于必须显式暴露的数据缺陷。

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

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/inventory/dto"
	"go_wp/internal/module/inventory/enums"
	"go_wp/internal/module/inventory/model"
	"go_wp/pkg/crypto"
)

// CreateWarehouse 新建仓库（验收 1）。
func (s *Service) CreateWarehouse(ctx context.Context, req *inventorydto.CreateWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New(inventoryenums.ErrWarehouseNameRequired)
	}
	code, err := normalizeCode(req.Code)
	if err != nil {
		return nil, err
	}
	status, err := normalizeStatus(req.Status)
	if err != nil {
		return nil, err
	}
	typ, err := normalizeWarehouseType(req.Type)
	if err != nil {
		return nil, err
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if taken, cerr := s.m.CodeExists(ctx, projectID, code, ""); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, errors.New(inventoryenums.ErrWarehouseCodeTaken)
	}
	// 工程内第一个仓自动成为默认仓：默认仓是「未指定仓库」的兜底，
	// 让「建了仓却没有默认仓」这种中间态不可能出现。
	existing, lerr := s.m.ListWarehouses(ctx, projectID)
	if lerr != nil {
		return nil, lerr
	}
	asDefault := req.IsDefault || len(existing) == 0
	// 虚拟仓没有实体收发能力，不能当兜底仓：默认仓是「未指定仓库」时的落点。
	if asDefault && typ == inventoryenums.WarehouseTypeVirtual {
		return nil, errors.New(inventoryenums.ErrWarehouseTypeVirtualDefault)
	}
	config, err := s.applyThirdPartyConfig(typ, req.ThirdParty, nil)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &inventorymodel.WarehouseEntity{
		ID: uuid.NewString(), ProjectID: projectID,
		Code: code, Name: name, Type: typ, Status: status,
		IsDefault: asDefault, Sort: req.Sort,
		Config:    config,
		Metadata:  orJSON(req.Metadata, "{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.CreateWarehouse(ctx, e, asDefault); err != nil {
		return nil, err
	}
	return toWarehouseResp(e), nil
}

// UpdateWarehouse 修改仓库（逐字段可选；nil = 本次不改）。
func (s *Service) UpdateWarehouse(ctx context.Context, req *inventorydto.UpdateWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetWarehouse(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapWarehouseNotFound(err)
	}
	if req.Code != nil {
		code, cerr := normalizeCode(*req.Code)
		if cerr != nil {
			return nil, cerr
		}
		if taken, xerr := s.m.CodeExists(ctx, e.ProjectID, code, e.ID); xerr != nil {
			return nil, xerr
		} else if taken {
			return nil, errors.New(inventoryenums.ErrWarehouseCodeTaken)
		}
		e.Code = code
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New(inventoryenums.ErrWarehouseNameRequired)
		}
		e.Name = name
	}
	if req.Status != nil {
		status, serr := normalizeStatus(*req.Status)
		if serr != nil {
			return nil, serr
		}
		// 默认仓必须保持启用：停用默认仓等于抽掉「未指定仓库」的兜底。
		if e.IsDefault && status != inventoryenums.StatusActive {
			return nil, errors.New(inventoryenums.ErrWarehouseIsDefault)
		}
		e.Status = status
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	if req.Type != nil {
		typ, terr := normalizeWarehouseType(*req.Type)
		if terr != nil {
			return nil, terr
		}
		// 默认仓不能改成虚拟仓：那等于把「未指定仓库」的兜底挪到一个不出货的仓上。
		if e.IsDefault && typ == inventoryenums.WarehouseTypeVirtual {
			return nil, errors.New(inventoryenums.ErrWarehouseTypeVirtualDefault)
		}
		e.Type = typ
	}
	// 对接配置：只有第三方仓写 config；未传配置时既有值原样保留
	//（类型可来回切换，切回去配置还在）。凭据按「未改动则不覆盖」处理。
	config, cerr := s.applyThirdPartyConfig(e.Type, req.ThirdParty, e.Config)
	if cerr != nil {
		return nil, cerr
	}
	e.Config = config
	if req.Metadata != nil {
		e.Metadata = orJSON(req.Metadata, "{}")
	}
	// 取消默认标记被拒绝（先指定另一个默认仓）：兜底不能凭空消失。
	if req.IsDefault != nil && !*req.IsDefault && e.IsDefault {
		return nil, errors.New(inventoryenums.ErrWarehouseIsDefault)
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateWarehouse(ctx, e); err != nil {
		return nil, err
	}
	if req.IsDefault != nil && *req.IsDefault && !e.IsDefault {
		if err = s.m.SetDefaultWarehouse(ctx, e.ProjectID, e.ID); err != nil {
			return nil, err
		}
		e.IsDefault = true
	}
	return toWarehouseResp(e), nil
}

// GetWarehouse 仓库详情。
func (s *Service) GetWarehouse(ctx context.Context, req *inventorydto.GetWarehouseReq) (res *inventorydto.WarehouseResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetWarehouse(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapWarehouseNotFound(err)
	}
	return toWarehouseResp(e), nil
}

// ListWarehouses 某工程的仓库列表（默认仓在最前）。
func (s *Service) ListWarehouses(ctx context.Context, req *inventorydto.ListWarehouseReq) (list []*inventorydto.WarehouseResp, err error) {
	projectID := ""
	if req != nil {
		projectID = strings.TrimSpace(req.ProjectID)
	}
	if projectID == "" {
		if projectID, err = s.resolveProjectID(ctx, ""); err != nil {
			return nil, err
		}
	}
	rows, err := s.m.ListWarehouses(ctx, projectID)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.WarehouseResp, 0, len(rows))
	for _, e := range rows {
		list = append(list, toWarehouseResp(e))
	}
	return list, nil
}

// DeleteWarehouse 删除仓库（默认仓与仓内有非零库存时拒绝）。
func (s *Service) DeleteWarehouse(ctx context.Context, req *inventorydto.DeleteWarehouseReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	e, err := s.m.GetWarehouse(ctx, req.ID, projectID)
	if err != nil {
		return mapWarehouseNotFound(err)
	}
	if e.IsDefault {
		return errors.New(inventoryenums.ErrWarehouseIsDefault)
	}
	// 库存行随仓删除（外键级联）—— 有货被删掉就是静默丢账，必须先清货 / 调拨。
	n, cerr := s.m.CountNonZeroStocks(ctx, e.ID, e.ProjectID)
	if cerr != nil {
		return cerr
	}
	if n > 0 {
		return errors.New(inventoryenums.ErrWarehouseHasStock)
	}
	return s.m.DeleteWarehouse(ctx, e.ID, e.ProjectID)
}

// resolveWarehouse 解析归属仓：显式指定优先（必须同工程且启用），为空则兜底默认仓。
//
// 这是「未指定仓库 → 默认仓」的唯一解析入口：商品模块建变体时经端口调用它拿短码
// 参与 SKU 编码，库存记录也落在同一个仓上，两处口径不会漂移。
func (s *Service) resolveWarehouse(ctx context.Context, projectID, warehouseID string) (e *inventorymodel.WarehouseEntity, err error) {
	id := strings.TrimSpace(warehouseID)
	if id != "" {
		// 作用域：显式工程优先，未指定时按唯一工程兜底 —— 与下面「兜底默认仓」分支同一口径。
		// 商品模块建变体经 ResolveWarehouse 端口调用本方法时**不带工程上下文**，
		// 因此这里不能要求 projectID 必填。
		//
		// 作用域只能用调用方给的工程，**不能**用行上的工程：那要先读到行才知道，
		// 而读行本身就要求作用域（鸡生蛋）。
		pid, perr := s.resolveProjectID(ctx, projectID)
		if perr != nil {
			return nil, perr
		}
		e, err = s.m.GetWarehouse(ctx, id, pid)
		if err != nil {
			return nil, mapWarehouseNotFound(err)
		}
		if e.ProjectID != pid {
			return nil, errors.New(inventoryenums.ErrWarehouseProjectMismatch)
		}
		if e.Status != inventoryenums.StatusActive {
			return nil, errors.New(inventoryenums.ErrWarehouseDisabled)
		}
		return e, nil
	}
	pid, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	e, err = s.m.GetDefaultWarehouse(ctx, pid)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(inventoryenums.ErrWarehouseDefaultMissing)
		}
		return nil, err
	}
	if e.Status != inventoryenums.StatusActive {
		return nil, errors.New(inventoryenums.ErrWarehouseDisabled)
	}
	return e, nil
}

// normalizeCode 归一并校验仓库短码：大写字母数字，1..maxWarehouseCodeLen。
//
// 短码是 SKU 编码的前缀（{短码}_{商品码}_{序号}），下划线是分隔符，
// 因此短码本身不允许出现分隔符 —— 否则 SKU 编码无法反解出仓库。
func normalizeCode(code string) (out string, err error) {
	out = strings.ToUpper(strings.TrimSpace(code))
	if out == "" {
		return "", errors.New(inventoryenums.ErrWarehouseCodeRequired)
	}
	if len(out) > maxWarehouseCodeLen {
		return "", errors.New(inventoryenums.ErrWarehouseCodeInvalid)
	}
	for _, r := range out {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return "", errors.New(inventoryenums.ErrWarehouseCodeInvalid)
	}
	return out, nil
}

// normalizeStatus 归一仓库状态：空串取默认启用，其余必须是内置取值。
func normalizeStatus(status string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return inventoryenums.StatusActive, nil
	case inventoryenums.StatusActive:
		return inventoryenums.StatusActive, nil
	case inventoryenums.StatusDisabled:
		return inventoryenums.StatusDisabled, nil
	default:
		return "", errors.New(inventoryenums.ErrWarehouseStatusInvalid)
	}
}

// mapWarehouseNotFound 行不存在 → 业务错误，其余原样透出。
func mapWarehouseNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrWarehouseNotFound)
	}
	return err
}

// toWarehouseResp 实体 → 响应。
func toWarehouseResp(e *inventorymodel.WarehouseEntity) *inventorydto.WarehouseResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.WarehouseResp{
		ID: e.ID, ProjectID: e.ProjectID, Code: e.Code, Name: e.Name,
		Type: e.Type, Status: e.Status, IsDefault: e.IsDefault, Sort: e.Sort,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	// 只有第三方仓带对接配置，且给出的是**脱敏后**的结构（凭据只有掩码与布尔值）。
	if e.Type == inventoryenums.WarehouseTypeThirdParty {
		resp.ThirdParty = warehouseThirdPartyResp(e.Config)
	}
	return resp
}

// orJSON jsonb 列的兜底值。
func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

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
