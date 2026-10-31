package mailservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	mailenums "go_wp/internal/module/mail/enums"
)

func TestWebhookNodeRejectsPrivateTargets(t *testing.T) {
	targets := map[string]string{
		"环回":       "https://127.0.0.1:6379/",
		"云元数据":     "https://169.254.169.254/latest/meta-data/",
		"私网 10":    "https://10.0.0.1/",
		"私网 192":   "https://192.168.1.1/",
		"IPv6环回":   "https://[::1]/",
		"IPv6唯一本地": "https://[fc00::1]/",
	}
	for name, raw := range targets {
		t.Run(name, func(t *testing.T) {
			node := AutomationNode{
				Key: "w1", Type: NodeTypeWebhook,
				Params: map[string]any{"url": raw},
				Out:    map[string]string{"next": "n2"},
			}
			_, err := nodeArity(node)
			if err == nil {
				t.Fatalf("%s（%s）应被拒绝", name, raw)
			}
			if !strings.Contains(err.Error(), "不允许指向内网") {
				t.Fatalf("错误应说明「不允许指向内网」，实际 %v", err)
			}
		})
	}
}

func TestWebhookNodeRejectsNonHTTPS(t *testing.T) {
	node := AutomationNode{
		Key: "w1", Type: NodeTypeWebhook,
		Params: map[string]any{"url": "http://93.184.216.34/hook"},
		Out:    map[string]string{"next": "n2"},
	}
	if _, err := nodeArity(node); err == nil {
		t.Fatal("http 地址应被拒绝（只允许 https）")
	}
}

func TestPostWebhookRejectsPrivateTargetAtRuntime(t *testing.T) {
	svc := &Service{}
	node := AutomationNode{
		Key: "w1", Type: NodeTypeWebhook,
		Params: map[string]any{"url": "https://10.1.2.3/hook"},
	}
	_, err := svc.postWebhook(context.Background(), node)
	if err == nil {
		t.Fatal("执行期必须拒绝内网地址（保存时的校验挡不住「直接改库」）")
	}

	if err.Error() != mailenums.RunKeyWebhookDenied {
		t.Fatalf("执行期应返回 %s，实际 %v", mailenums.RunKeyWebhookDenied, err)
	}
}

func TestPostWebhookRejectsMissingURL(t *testing.T) {
	svc := &Service{}
	node := AutomationNode{Key: "w1", Type: NodeTypeWebhook, Params: map[string]any{}}
	if _, err := svc.postWebhook(context.Background(), node); err == nil {
		t.Fatal("没有 url 时应报错")
	}
}

func TestWebhookFailureDoesNotStopFlow(t *testing.T) {

	svc := &Service{}
	node := AutomationNode{
		Key: "w1", Type: NodeTypeWebhook,
		Params: map[string]any{"url": "https://127.0.0.1/hook"},
		Out:    map[string]string{"next": "n2"},
	}
	_, err := svc.postWebhook(context.Background(), node)
	if err == nil {
		t.Fatal("内网地址应返回错误（该错误只用于节点日志，不终止流程）")
	}
	var ge *GraphError
	if errors.As(err, &ge) {
		t.Fatal("执行期错误不该是 GraphError（那是保存期校验的载体）")
	}
}
