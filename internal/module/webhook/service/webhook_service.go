package webhookservice

// 本文件只放 Service 结构体、构造函数、装配注入与端点密钥加解密；各能力域见
// 以及既有的 deliver.go / sign.go / ssrf.go / webhook_task.go / webhook_replay*.go。

// 防线是「DNS 解析后的 IP」而不是 hostname 字符串：攻击者可以把内网地址
// 绑到公网域名上（DNS rebinding / 域名指内网），只看字符串拦不住。
// 因此这里强制解析出全部 A/AAAA 记录，任何一个 IP 落在禁止网段即整体拒绝。
//
// lookupIP 做成包级变量是为了测试可注入（私有 DNS / 环回场景不必真起解析）。

// 签名串 = "<timestamp>.<rawBody>"，密钥 = 端点预注册的 secret（管理员配置，
// 加密存库）。接收方按同一算法重算并用常量时间比较验签；
// timestamp 入签可防重放（接收方按自己的时间窗校验时效）。

// 投递一律异步：事件发布方（订单、内容等模块）只调 DispatchEvent，
// 真正的出站 HTTP 由 worker 执行 —— 慢目标或超时不会拖住业务请求。

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go_wp/internal/module/webhook/contract"
	"go_wp/internal/module/webhook/enums"
	"go_wp/internal/module/webhook/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/queue"
)

// Service webhook 域服务：只持本模块 model。
type Service struct {
	m *webhookmodel.WebhookModel
	// cipherSecret 端点签名密钥的加密密钥（装配期注入 app.secret）。
	cipherSecret string
	// client 出站 HTTP 客户端（测试可注入假 Transport）。
	client *http.Client
}

// NewService 构造。
func NewService(m *webhookmodel.WebhookModel) *Service {
	return &Service{m: m, client: newWebhookClient()}
}

// SetCipherSecret 注入敏感配置加密密钥。
func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = secret }

// SetHTTPClient 测试注入口（注入受限客户端的替身）。
func (s *Service) SetHTTPClient(c *http.Client) { s.client = c }

// 编译期断言：本 service 实现模块对外契约。
var (
	_ webhookcontract.EndpointService = (*Service)(nil)
	_ webhookcontract.Dispatcher      = (*Service)(nil)
)

// encryptSecret / decryptSecret 密钥加解密（cipherSecret 未配置时明确报错）。
func (s *Service) encryptSecret(plain string) (string, error) {
	if s.cipherSecret == "" {
		return "", errors.New("未配置加密密钥，无法保存 webhook 签名密钥")
	}
	return crypto.Encrypt(plain, s.cipherSecret)
}

func (s *Service) decryptSecret(cipherText string) (string, error) {
	if s.cipherSecret == "" {
		return "", errors.New("未配置加密密钥，无法解密 webhook 签名密钥")
	}
	return crypto.Decrypt(cipherText, s.cipherSecret)
}

// lookupIP DNS 解析入口（测试可替换）。
var lookupIP = (*net.Resolver).LookupIPAddr

// validateURL 投递路径调用的校验入口（测试可替换；生产恒为 validateWebhookURL）。
var validateURL = validateWebhookURL

// SSRF 防护错误。
//
// 错误值取自 enums 的 **i18n key**（不是中文文案）：这几个错误会经 service 直接
// 返回给 HTTP 层，而 pkg/response.ErrorAuto 按「值是不是 key 形态」区分业务错误 ——
// 用中文原文会让「你填了个内网地址」变成「服务器内部错误，请稍后重试」（500），
// 用户看不出该改哪里。文案在迁移 216 的词条里。
//
// 导出这些变量而不是让调用方比较 key 字符串：调用方（含测试）比较的是变量本身。
var (
	ErrWebhookURLScheme = errors.New(webhookenums.ErrURLSchemeUnsupported)
	ErrWebhookURLHost   = errors.New(webhookenums.ErrURLHostMissing)
	ErrWebhookURLNoIP   = errors.New(webhookenums.ErrURLUnresolvable)
	ErrWebhookURLDenied = errors.New(webhookenums.ErrURLDenied)
)

// isForbiddenIP 判断 IP 是否落在禁止网段：
// 私有段（10/8、172.16/12、192.168/16、IPv6 fc00::/7）、环回（127/8、::1）、
// 链路本地（169.254/16、fe80::/10）、组播与未指定地址。
func isForbiddenIP(ip net.IP) bool {
	return !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// dialWebhookContext 把校验绑定到真正建立连接的 IP，避免校验与拨号各解析一次域名。
// URL 中的 hostname 保持原样，HTTP Host 与 TLS 证书校验仍针对原始主机。
func dialWebhookContext(ctx context.Context, network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := lookupIP(net.DefaultResolver, ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, ErrWebhookURLNoIP
	}
	// 先检查全部结果，再尝试连接；混合公私地址不得因记录顺序不同而放行。
	for _, addr := range ips {
		if isForbiddenIP(addr.IP) || addr.Zone != "" {
			return nil, ErrWebhookURLDenied
		}
	}
	dialer := net.Dialer{Timeout: DialTimeout}
	for _, addr := range ips {
		var conn net.Conn
		conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, err
}

// validateWebhookURL 校验出站目标 URL：协议白名单 + DNS 解析后逐 IP 检查。
//
// 注意：本函数只做网络层防护；「域名白名单」（只允许向预注册端点发请求）
// 由 service 层保证 —— 投递永远使用 webhook_endpoints 里存的 URL，
// 事件发布方根本没有指定 URL 的入口。
func validateWebhookURL(raw string) (err error) {
	u, perr := url.Parse(raw)
	if perr != nil {
		// 丢掉 url.Parse 的技术细节：用户要的是「这个地址填得不对」，
		// 而不是 Go 标准库的错误串（后者会一起被渲染到界面上）。
		return errors.New(webhookenums.ErrURLMalformed)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrWebhookURLScheme
	}
	host := u.Hostname()
	if strings.TrimSpace(host) == "" {
		return ErrWebhookURLHost
	}

	ctx, cancel := context.WithTimeout(context.Background(), DialTimeout)
	defer cancel()
	ips, lerr := lookupIP(net.DefaultResolver, ctx, host)
	if lerr != nil || len(ips) == 0 {
		return ErrWebhookURLNoIP
	}
	for _, addr := range ips {
		if isForbiddenIP(addr.IP) {
			return ErrWebhookURLDenied
		}
	}
	return nil
}

// SignHeader 签名头 / 时间戳头（与交付客户端共用）。
const (
	HeaderSignature = "X-Webhook-Signature"
	HeaderTimestamp = "X-Webhook-Timestamp"
	HeaderEvent     = "X-Webhook-Event"
	HeaderDelivery  = "X-Webhook-Delivery-ID"
)

// SignaturePrefix 签名值前缀（对齐 GitHub webhook 的 sha256= 约定，便于通用验签器解析）。
const SignaturePrefix = "sha256="

// SignPayload 计算 HMAC-SHA256 签名，返回 "sha256=<hex>" 形式的头值。
func SignPayload(secret string, timestampNano int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", timestampNano)
	mac.Write(body)
	return SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// VerifyPayload 验签（常量时间比较；导出供接收方实现与测试共用）。
func VerifyPayload(secret string, timestampNano int64, body []byte, signature string) bool {
	expected := SignPayload(secret, timestampNano, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// TimestampHeaderValue 时间戳头的字符串形态。
func TimestampHeaderValue(ts int64) string { return strconv.FormatInt(ts, 10) }

// TaskWebhookDeliver 投递任务类型。
const TaskWebhookDeliver = "webhook:deliver"

// WebhookDeliverPayload 投递载荷。
//
// 只带 DeliveryID：worker 回库取端点与负载 —— 端点密钥与 URL 以投递时刻
// 数据库为准（管理员改 URL / 停用端点对未派发任务立即生效）。
type WebhookDeliverPayload struct {
	DeliveryID uint64 `json:"delivery_id"`
}

// webhookDeliverTask 队列任务门面（MaxRetry 即投递重试上限）。
var webhookDeliverTask = queue.NewTask(TaskWebhookDeliver, queue.WithQueue("default"), queue.WithMaxRetry(3))

// enqueueWebhookDeliver 入队一次投递。
func enqueueWebhookDeliver(p WebhookDeliverPayload) error {
	if !queue.IsInited() {
		return errors.New("队列未启用，无法异步投递 webhook")
	}
	return webhookDeliverTask.Enqueue(p)
}

// RegisterWebhookTaskHandler 把 handler 注册进队列 worker（路由装配时调用）。
func RegisterWebhookTaskHandler(db *gorm.DB, cipherSecret string) {
	queue.Register(TaskWebhookDeliver, handleWebhookDeliver(db, cipherSecret))
}

// handleWebhookDeliver 投递 handler。
func handleWebhookDeliver(db *gorm.DB, cipherSecret string) queue.Handler {
	return func(ctx context.Context, raw []byte) error {
		var p WebhookDeliverPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.DeliveryID == 0 {
			return errors.New("投递任务载荷缺少 delivery_id")
		}
		svc := NewService(webhookmodel.NewWebhookModel(db))
		svc.SetCipherSecret(cipherSecret)
		return svc.DeliverDelivery(ctx, p.DeliveryID)
	}
}
