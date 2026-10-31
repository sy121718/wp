package webhookservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"

	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/queue"
	"go_wp/pkg/ssrf"
)

type Service struct {
	m *webhookmodel.WebhookModel

	cipherSecret string

	client *http.Client
}

func NewService(m *webhookmodel.WebhookModel) *Service {
	return &Service{m: m, client: newWebhookClient()}
}

func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = secret }

func (s *Service) SetHTTPClient(c *http.Client) { s.client = c }

var (
	_ webhookcontract.EndpointService = (*Service)(nil)
	_ webhookcontract.Dispatcher      = (*Service)(nil)
)

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

var lookupIP = (*net.Resolver).LookupIPAddr

var validateURL = validateWebhookURL

var (
	ErrWebhookURLScheme = webhookenums.Err(webhookenums.ErrURLSchemeUnsupported)
	ErrWebhookURLHost   = webhookenums.Err(webhookenums.ErrURLHostMissing)
	ErrWebhookURLNoIP   = webhookenums.Err(webhookenums.ErrURLUnresolvable)
	ErrWebhookURLDenied = webhookenums.Err(webhookenums.ErrURLDenied)
)

func isForbiddenIP(ip net.IP) bool { return ssrf.IsForbiddenIP(ip) }

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

func validateWebhookURL(raw string) (err error) {
	u, perr := url.Parse(raw)
	if perr != nil {

		return webhookenums.Err(webhookenums.ErrURLMalformed)
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

const (
	HeaderSignature = "X-Webhook-Signature"
	HeaderTimestamp = "X-Webhook-Timestamp"
	HeaderEvent     = "X-Webhook-Event"
	HeaderDelivery  = "X-Webhook-Delivery-ID"
)

const SignaturePrefix = "sha256="

func SignPayload(secret string, timestampNano int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", timestampNano)
	mac.Write(body)
	return SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

func VerifyPayload(secret string, timestampNano int64, body []byte, signature string) bool {
	expected := SignPayload(secret, timestampNano, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func TimestampHeaderValue(ts int64) string { return strconv.FormatInt(ts, 10) }

const TaskWebhookDeliver = "webhook:deliver"

type WebhookDeliverPayload struct {
	DeliveryID uint64 `json:"delivery_id"`
}

var webhookDeliverTask = queue.NewTask(TaskWebhookDeliver, queue.WithQueue("default"), queue.WithMaxRetry(3))

func enqueueWebhookDeliver(p WebhookDeliverPayload) error {
	if !queue.IsInited() {
		return errors.New("队列未启用，无法异步投递 webhook")
	}
	return webhookDeliverTask.Enqueue(p)
}

func RegisterWebhookTaskHandler(db *gorm.DB, cipherSecret string) {
	queue.Register(TaskWebhookDeliver, handleWebhookDeliver(db, cipherSecret))
}

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
