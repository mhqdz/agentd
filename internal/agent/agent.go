// Package agent：多轮对话循环（P2 才在这里加工具）。
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"agentd/internal/config"
	"agentd/internal/llm"
	"agentd/internal/store"
)

type Agent struct {
	cfg config.Config
	llm *llm.Client
	// Drain：取出本轮期间用户插话（web 层提供）；OnInterject：把插话回显给前端
	Drain       func() []string
	OnInterject func(string)
	st          store.Store
	profile     config.Profile
}

func New(cfg config.Config, st store.Store, profile config.Profile) *Agent {
	if profile.Model == "" {
		profile.Model = cfg.Model
	}
	if profile.BaseURL == "" {
		profile.BaseURL = cfg.BaseURL
	}
	key := profile.APIKey
	if key == "" {
		key = cfg.APIKey
	}
	return &Agent{cfg: cfg, llm: llm.New(profile.BaseURL, key), st: st, profile: profile}
}

func (a *Agent) Profile() config.Profile { return a.profile }

func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Load 或新建会话。
func (a *Agent) Conversation(id string) (*store.Conversation, error) {
	if id != "" {
		if c, err := a.st.Get(id); err == nil {
			return c, nil
		}
	}
	now := time.Now()
	return &store.Conversation{ID: a.st.NextID(), Title: "新会话", CreatedAt: now, UpdatedAt: now}, nil
}

// Send：追加用户消息 → 流式请求 → 追加助手消息 → 存盘。
// onDelta 用来把增量交给调用方（CLI 打印 / HTTP SSE 推送）。
func (a *Agent) Send(ctx context.Context, c *store.Conversation, text string,
	onDelta func(content, reasoning string)) error {
	c.Messages = append(c.Messages, store.Message{Role: "user", Content: text, CreatedAt: time.Now()})
	if c.Title == "新会话" || c.Title == "" {
		t := strings.TrimSpace(text)
		if r := []rune(t); len(r) > 24 {
			t = string(r[:24]) + "…"
		}
		c.Title = t
	}

	msgs := make([]llm.Message, 0, len(c.Messages))
	for _, m := range c.Messages {
		msgs = append(msgs, llm.Message{Role: m.Role, Content: m.Content})
	}

	var content, reasoning strings.Builder
	err := a.llm.Stream(ctx, llm.ChatRequest{Model: a.profile.Model, Messages: msgs, ReasoningEffort: a.profile.ReasoningEffort}, func(ch llm.Chunk) error {
		if ch.Done {
			return nil
		}
		content.WriteString(ch.Delta)
		reasoning.WriteString(ch.Reasoning)
		if onDelta != nil && (ch.Delta != "" || ch.Reasoning != "") {
			onDelta(ch.Delta, ch.Reasoning)
		}
		return nil
	})
	// 即使出错也把已收到的部分存下来，便于排查
	if content.Len() > 0 || err == nil {
		c.Messages = append(c.Messages, store.Message{
			Role: "assistant", Content: strings.TrimSpace(content.String()), Reasoning: reasoning.String(), CreatedAt: time.Now(),
		})
	}
	if serr := a.st.Save(c); serr != nil {
		return fmt.Errorf("存盘失败: %w", serr)
	}
	return err
}
