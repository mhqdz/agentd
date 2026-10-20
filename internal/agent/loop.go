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
		var content, reasoning strings.Builder
		var calls []llm.ToolCall
		err := a.llm.StreamWithTools(ctx, llm.ChatRequest{Model: a.cfg.Model, Messages: msgs, Tools: specs},
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
				res, err := spec.Run(ctx, args)
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
