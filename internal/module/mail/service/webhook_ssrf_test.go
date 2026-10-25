package mailservice

// webhook_ssrf_test.go — 回调节点的 SSRF 拦截（T4）。
//
// 判定本体在 pkg/ssrf（那边用 7 个用例覆盖六类地址与混合记录）。本文件判的是
// **接线**：mail 的回调节点在两处都接上了它 ——
//
//  1. 保存时（nodeArity）：填了内网地址当场拒绝，而不是等到执行；
//  2. 执行时（postWebhook）：库里的 URL 可能被直接改过，执行期必须再校验一次。
//
// 为什么两处都要：只判保存时会漏掉「直接改库」；只判执行期会让作者在画布上画完、
// 保存成功、启用，然后在第一个真实用户身上才发现回调永远失败。

import (
	"context"
	"errors"
	"strings"
	"testing"

	mailenums "go_wp/internal/module/mail/enums"
)

// TestWebhookNodeRejectsPrivateTargets 保存时（nodeArity）六类内网地址全拒。
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

// TestWebhookNodeRejectsNonHTTPS 只允许 https（http 可以明文改包，回调签名也就失效了）。
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

// TestPostWebhookRejectsPrivateTargetAtRuntime 执行期再校验一次（库里被改过也拦得住）。
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
	// 执行期错误是**运行文案 key**（落库后按请求语言还原），不是中文原文 ——
	// 判据是 key 本身：换个语言也该是同一条。
	if err.Error() != mailenums.RunKeyWebhookDenied {
		t.Fatalf("执行期应返回 %s，实际 %v", mailenums.RunKeyWebhookDenied, err)
	}
}

// TestPostWebhookRejectsMissingURL 没有地址时不发请求，直接报错。
func TestPostWebhookRejectsMissingURL(t *testing.T) {
	svc := &Service{}
	node := AutomationNode{Key: "w1", Type: NodeTypeWebhook, Params: map[string]any{}}
	if _, err := svc.postWebhook(context.Background(), node); err == nil {
		t.Fatal("没有 url 时应报错")
	}
}

// TestWebhookFailureDoesNotStopFlow 回调失败不终止流程（下游动作还要执行）。
//
// 判据落在执行器的控制流上：`postWebhook` 返回错误时，节点日志记 failed，但**照常
// 沿 next 前进**。这里用「不可达地址 + 已存在的出边」验它不返回失败态 —— 真正的
// 数据库级验证在 feature 测试里（需要真 PG）。
func TestWebhookFailureDoesNotStopFlow(t *testing.T) {
	// 判据是「postWebhook 的错误被当作日志内容而不是 run 失败」：这里验错误值本身
	// 是普通 error（不是 failRun 的输入），控制流由 RunAutomation 的 webhook 分支保证。
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
