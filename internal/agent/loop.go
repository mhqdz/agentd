package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"agentd/internal/llm"
	"agentd/internal/store"
	"agentd/internal/tools"
)

// 工具循环：最多 8 轮，避免模型自己绕不出来。
const maxRounds = 8

// SendWithTools 带上工具跑一轮（含多轮工具调用）。并行执行同一轮里的多个工具。
func (a *Agent) SendWithTools(ctx context.Context, c *store.Conversation, text string,
	onDelta func(content, reasoning string), onTool func(name, args string, result any)) error {
	reg := tools.Registry()
	if len(a.profile.Tools) > 0 {
		for name := range reg {
			keep := false
			for _, t := range a.profile.Tools {
				if t == name {
					keep = true
					break
				}
			}
			if !keep {
				delete(reg, name)
			}
		}
	}
	specs := tools.SchemaJSON(reg)

	c.Messages = append(c.Messages, store.Message{Role: "user", Content: text})
	if c.Title == "新会话" || c.Title == "" {
		t := strings.TrimSpace(text)
		if r := []rune(t); len(r) > 24 {
			t = string(r[:24]) + "…"
		}
		c.Title = t
	}

	for round := 0; round < maxRounds; round++ {
		msgs, toolMsgs := toWire(c.Messages, reg)
		msgs = compact(msgs, a.cfg.KeepMessages, a.cfg.MaxPromptChars/2)
		if a.cfg.System != "" {
			msgs = append([]llm.Message{{Role: "system", Content: a.cfg.System}}, msgs...)
		}
		total := 0
		for _, m := range msgs {
			total += len(m.Content)
			for _, tc := range m.ToolCalls {
				total += len(tc.Function.Arguments)
			}
		}
		if a.cfg.MaxPromptChars > 0 && total > a.cfg.MaxPromptChars {
			return fmt.Errorf("请求体已达 %d 字符（上限 %d），已中止以免爆上下文；建议新开会话",
				total, a.cfg.MaxPromptChars)
		}
		var content, reasoning strings.Builder
		var calls []llm.ToolCall
		err := a.llm.StreamWithTools(ctx, llm.ChatRequest{Model: a.profile.Model, Messages: msgs, Tools: specs, ReasoningEffort: a.profile.ReasoningEffort},
			func(ch llm.Chunk) error {
				if ch.Done {
					return nil
				}
				content.WriteString(ch.Delta)
				reasoning.WriteString(ch.Reasoning)
				if onDelta != nil && (ch.Delta != "" || ch.Reasoning != "") {
					onDelta(ch.Delta, ch.Reasoning)
				}
				return nil
			}, &calls)
		if err != nil {
			return err
		}
		if len(calls) == 0 {
			c.Messages = append(c.Messages, store.Message{
				Role: "assistant", Content: strings.TrimSpace(content.String()), Reasoning: reasoning.String()})
			return a.st.Save(c)
		}
		// 记录带 tool_calls 的助手消息
		c.Messages = append(c.Messages, store.Message{
			Role: "assistant", Content: strings.TrimSpace(content.String()),
			Reasoning: reasoning.String(), ToolCalls: calls})
		// 并行执行
		results := make([]any, len(calls))
		var wg sync.WaitGroup
		for i, call := range calls {
			wg.Add(1)
			go func(i int, call llm.ToolCall) {
				defer wg.Done()
				spec, ok := reg[call.Function.Name]
				if !ok {
					results[i] = "✗ 没有这个工具：" + call.Function.Name
					return
				}
				var args map[string]any
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				if args == nil {
					args = map[string]any{}
				}
				res, err := spec.Run(ctx, args, &tools.Env{Messages: c.Messages})
				if err != nil {
					res = "✗ " + err.Error()
				}
				results[i] = res
				if onTool != nil {
					onTool(call.Function.Name, call.Function.Arguments, res)
				}
			}(i, call)
			_ = toolMsgs
		}
		wg.Wait()
		for i, call := range calls {
			c.Messages = append(c.Messages, store.Message{
				Role: "tool", ToolCallID: call.ID, Content: stringify(results[i]), ToolResult: results[i]})
		}
	}
	return fmt.Errorf("超过 %d 轮工具调用，已停止", maxRounds)
}

func toWire(msgs []store.Message, reg map[string]*tools.Spec) ([]llm.Message, bool) {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, llm.Message{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID})
	}
	return out, true
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// compact：上下文压缩（轻量、可预测）。
// 规则：保留 system + 最近 keep 条消息；更早的折叠成一条"省略说明"。
// 不调用模型做摘要（省钱、可预测）；需要更聪明的摘要时再换。
func compact(msgs []llm.Message, keep int, budget int) []llm.Message {
	if len(msgs) <= keep+1 {
		return msgs
	}
	var sys *llm.Message
	start := 0
	if len(msgs) > 0 && msgs[0].Role == "system" {
		sys = &msgs[0]
		start = 1
	}
	body := msgs[start:]
	total := 0
	for _, m := range body {
		total += len(m.Content)
	}
	if keep <= 0 {
		keep = 12
	}
	if total <= budget && len(body) <= keep {
		return msgs
	}
	cut := len(body) - keep
	if cut <= 0 {
		return msgs
	}
	// 边界必须落在 user 消息上，避免把 tool 结果和它的 tool_call 拆开
	for cut < len(body) && body[cut].Role != "user" {
		cut++
	}
	note := llm.Message{Role: "user", Content: fmt.Sprintf(
		"（上下文压缩：更早的 %d 条消息已省略；如果缺少必要信息，请向用户确认，不要猜。）", cut)}
	out := make([]llm.Message, 0, keep+2)
	if sys != nil {
		out = append(out, *sys)
	}
	out = append(out, note)
	out = append(out, body[cut:]...)
	return out
}
