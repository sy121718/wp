package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/queue"
)

// ========== 任务类型 ==========

const TypeEmailSend = "email:send"

// ========== Payload 定义 ==========

type EmailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// ========== 处理器 ==========

func HandleEmailSend(ctx context.Context, data []byte) error {
	var payload EmailPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("解析邮件载荷失败: %w", err)
	}

	// 这里写发送邮件的逻辑
	logger.Scene("task").With("to", payload.To).With("subject", payload.Subject).Info("发送邮件")

	return nil
}

// ========== 业务调用方法 ==========

func SendEmail(to, subject, body string) error {
	return queue.Enqueue(TypeEmailSend, EmailPayload{
		To:      to,
		Subject: subject,
		Body:    body,
	})
}

func SendEmailDelay(to, subject, body string, delay time.Duration) error {
	return queue.EnqueueIn(TypeEmailSend, delay, EmailPayload{
		To:      to,
		Subject: subject,
		Body:    body,
	})
}

// ========== 自动注册 ==========
