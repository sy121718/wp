package aiservice

// ai_session_dialogue.go — 按会话键取回最近的对话轮次。
//
// 为什么要它：悬浮球与概览页的提问框都是**单次问答**的渲染形态 —— 一次回答画在
// 一个节点里，关掉面板 / 刷新页面就只剩空框。用户看到的是「搜索引擎」，而服务端
// 那边其实一直是同一条会话在续写（fabSessionKey 固定、历史进 stablePrefix），
// 模型记得上一轮，界面上却看不出来。这个函数就是把「服务端已有的历史」交给界面。
//
// 只读、不建会话：找不到键就回空（首访时用户还没问过任何问题）。

import (
	"context"
	"strings"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
)

// dialogueTurnLimit 默认回填的轮数上限（一轮 = 一条提问 + 一条回答）。
// 再多也没有意义：回填是给人扫一眼「我们聊到哪了」，不是审计工具。
const dialogueTurnLimit = 12

// RecentDialogue 取会话键下最近的若干轮对话，按时间**正序**返回。
//
// 会话不存在或没有提问时返回 nil（不是错误）—— 首访就走这条路。
func (s *SessionService) RecentDialogue(ctx context.Context, key string, limit int) ([]aidto.DialogueTurn, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrSessionKeyMissing
	}
	if limit < 1 || limit > dialogueTurnLimit {
		limit = dialogueTurnLimit
	}
	sess, err := s.model.FindSessionByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, nil
	}

	// 一轮至少两条事件（user + assistant），实际还会夹着工具事件；按 4 倍取够。
	// 倒序取是为了「最近的优先」：会话很长时也要能立刻看到最新的那几轮。
	rows, _, err := s.model.ListEventsDesc(ctx, sess.ID, 0, limit*4)
	if err != nil {
		return nil, err
	}

	turns := make([]aidto.DialogueTurn, 0, limit*2)
	// 倒序取回来的行要翻回正序，界面才是「从上往下读」的时间线。
	for i := len(rows) - 1; i >= 0; i-- {
		kind := rows[i].Kind
		if kind != string(aienums.EventKindUser) && kind != string(aienums.EventKindAssistant) {
			continue
		}
		text := strings.TrimSpace(rows[i].Content)
		if text == "" {
			continue
		}
		turn := aidto.DialogueTurn{Role: "user", Text: text}
		if kind == string(aienums.EventKindAssistant) {
			turn.Role = "assistant"
			if v, ok := rows[i].Meta[eventMetaReasoning].(string); ok {
				turn.Reasoning = v
			}
		} else if v, ok := rows[i].Meta[eventMetaUserText].(string); ok && strings.TrimSpace(v) != "" {
			// 原话优先：Content 是注入过页面上下文的那份，界面回填只该显示用户敲进去的。
			turn.Text = strings.TrimSpace(v)
		}
		turns = append(turns, turn)
	}
	return tailDialogueTurns(turns, limit), nil
}

// tailDialogueTurns 只保留最后 limit 轮（从末尾往前数 limit 个 user 行）。
//
// 按「轮」而不是按「条」截断：截出半轮（只有提问没有回答）会让界面显示一个
// 永远等不到回答的气泡，看起来像卡住了。
func tailDialogueTurns(turns []aidto.DialogueTurn, limit int) []aidto.DialogueTurn {
	if len(turns) <= limit {
		return turns
	}
	seen := 0
	start := 0
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role != "user" {
			continue
		}
		seen++
		if seen > limit {
			break
		}
		// 记住**第 limit 轮的那个提问**的位置：保留要从它开始，
		// 而不是从它的下一行（那会把这一轮的提问切掉，留下一个没有问题的回答）。
		start = i
	}
	return turns[start:]
}
