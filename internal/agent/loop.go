package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"agentd/internal/config"
	"agentd/internal/llm"
	"agentd/internal/store"
	"agentd/internal/tools"
)

// 工具循环：最多 8 轮，避免模型自己绕不出来。

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
	// 用户消息立刻落盘：请求被中断也不会丢这句
	_ = a.st.Save(c)

	limit := a.MaxRounds
	if limit <= 0 {
		limit = int(^uint(0) >> 1) // 关闭限制 = 不设上限（由用户自行掌控）
	}
	for round := 0; round < limit; round++ {
		// 中途插话：把用户新消息插进本轮上下文（模型这一轮就能看到）
		if a.Drain != nil {
			for _, t := range a.Drain() {
				if strings.TrimSpace(t) == "" {
					continue
				}
				c.Messages = append(c.Messages, store.Message{Role: "user", Content: t})
				if a.OnInterject != nil {
					a.OnInterject(t)
				}
			}
		}
		// 超阈值就先做一次摘要压缩（带检查点），再走轻量裁剪兜底
		keep := a.profile.KeepMessages
		if keep <= 0 {
			keep = a.cfg.KeepMessages
		}
		_ = a.maybeSummarize(ctx, c, keep)
		msgs, toolMsgs := toWire(c.Messages, reg)
		msgs = compact(msgs, keep, a.cfg.MaxPromptChars/2)
		if a.profile.System != "" || c.Summary != "" || len(c.KV) > 0 {
			sys := a.profile.System
			if len(c.KV) > 0 {
				sys += "\n\n[已知事实]\n"
				keys := make([]string, 0, len(c.KV))
				for k := range c.KV {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					sys += "- " + k + ": " + c.KV[k] + "\n"
				}
			}
			pfx := make([]llm.Message, 0, 2)
			pfx = append(pfx, llm.Message{Role: "system", Content: sys})
			if c.Summary != "" {
				pfx = append(pfx, llm.Message{Role: "user", Content: "[更早对话的摘要]\n" + c.Summary})
			}
			msgs = append(pfx, msgs...)
		}
		total := 0
		for _, m := range msgs {
			total += wireLen(m.Content)
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
			// 意外中断（用户停止 / 网络断 / 上游报错）：先保住已生成的内容，再上抛错误
			if strings.TrimSpace(content.String()) != "" || reasoning.Len() > 0 {
				c.Messages = append(c.Messages, store.Message{
					Role: "assistant", Content: strings.TrimSpace(content.String()), Reasoning: reasoning.String(),
				})
			}
			// 中断：保留半截内容，并在末尾追加一句中断说明（是数据，不是状态）
			note := "\n\n——（中断：" + err.Error() + "）"
			if len(c.Messages) > 0 && c.Messages[len(c.Messages)-1].Role == "assistant" {
				last := &c.Messages[len(c.Messages)-1]
				last.Content = strings.TrimRight(last.Content, "\n") + note
			} else {
				// 工具轮被打断：补一条 assistant 说明，前端能明确看到"中断"
				c.Messages = append(c.Messages, store.Message{Role: "assistant", Content: strings.TrimSpace(note)})
			}
			_ = a.st.Save(c) // 中断也要留住上下文（用户消息 / 已完成的工具轮次）
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
		_ = a.st.Save(c) // 工具轮：先落库，再执行（中断也不丢）
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
			textOut, imgs := splitResult(results[i])
			c.Messages = append(c.Messages, store.Message{
				Role: "tool", ToolCallID: call.ID, Content: textOut, ToolResult: results[i], Images: imgs})
			if len(imgs) > 0 {
				// OpenAI 的 tool 消息不能带图 → 用一条 user 消息把像素喂给模型
				c.Messages = append(c.Messages, store.Message{
					Role: "user", Content: "（上一个工具的截图，请以图为准）", Images: imgs})
			}
		}
		_ = a.st.Save(c) // 工具结果落库
	}
	// 轮次用尽：不能静默返回空回复 ✗ —— 补一条说明并落库
	note := fmt.Sprintf("（已达到工具轮次上限 %d 轮，本轮没有给出最终结论。可以追问一句\"继续\"让它接着做，或到设置里调高上限。）", limit)
	c.Messages = append(c.Messages, store.Message{Role: "assistant", Content: note})
	if err := a.st.Save(c); err != nil {
		return err
	}
	if onDelta != nil {
		onDelta(note, "")
	}
	return nil
}

func toWire(msgs []store.Message, reg map[string]*tools.Spec) ([]llm.Message, bool) {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		var content any = m.Content
		if len(m.Images) > 0 {
			parts := []map[string]any{{"type": "text", "text": m.Content}}
			for _, img := range m.Images {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": img}})
			}
			content = parts
		}
		out = append(out, llm.Message{Role: m.Role, Content: content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID})
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
		total += wireLen(m.Content)
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

// maybeSummarize：超过预算的一半时，把"最近 keep 条之前"的消息交给摘要模型，
// 生成检查点（summary + upto + model + ts）。失败不阻断本轮（降级为轻量裁剪）。
func (a *Agent) maybeSummarize(ctx context.Context, c *store.Conversation, keep int) error {
	total := 0
	for _, m := range c.Messages {
		total += wireLen(m.Content)
	}
	if total < a.cfg.MaxPromptChars/2 {
		return nil
	}
	cut := len(c.Messages) - keep
	if cut <= 0 {
		return nil
	}
	for cut < len(c.Messages) && c.Messages[cut].Role != "user" {
		cut++
	}
	if cut <= 0 {
		return nil
	}
	var b strings.Builder
	for _, m := range c.Messages[:cut] {
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(tailRunes(m.Content, 400))
		b.WriteString("\n")
	}
	prompt := "把下面的对话压缩成不超过 400 字的摘要，供后续继续工作使用。务必保留：" +
		"任务目标、已确认的决策、涉及的文件路径与服务端口、遇到的问题与结论、下一步。" +
		"不要编造；不确定就写\"不确定\"。\n\n" + b.String()

	// 摘要模型：profile.Summarizer 指定，否则用自己
	sumProfile := a.profile
	if a.profile.Summarizer != "" {
		if p, ok := config.GetProfile(a.profile.Summarizer); ok {
			sumProfile = p
		}
	}
	sumKey := sumProfile.APIKey
	if sumKey == "" {
		sumKey = a.cfg.APIKey
	}
	sc := llm.New(sumProfile.BaseURL, sumKey)
	out, err := sc.Chat(ctx, llm.ChatRequest{Model: sumProfile.Model, ReasoningEffort: sumProfile.ReasoningEffort,
		Messages: []llm.Message{{Role: "user", Content: prompt}}})
	if err != nil || strings.TrimSpace(out) == "" {
		return err
	}
	c.Summary = strings.TrimSpace(out)
	c.SummaryUpto = c.Messages[cut-1].Seq
	c.SummaryModel = sumProfile.Model
	c.SummaryAt = time.Now()
	// KV：先只放"生成者"，其余由后续从摘要里抽（下一轮做）
	if c.KV == nil {
		c.KV = map[string]string{}
	}
	c.KV["summary_generated_by"] = sumProfile.Model + " @ " + c.SummaryAt.Format("2006-01-02 15:04")
	return a.st.Save(c)
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// splitResult：工具结果是 []map{type:text|image} 时拆成 文本 + 图片列表。
func splitResult(v any) (string, []string) {
	parts, ok := v.([]map[string]any)
	if !ok {
		return stringify(v), nil
	}
	var text string
	var imgs []string
	for _, p := range parts {
		switch p["type"] {
		case "text":
			if t, ok := p["text"].(string); ok {
				text += t
			}
		case "image":
			if u, ok := p["url"].(string); ok {
				imgs = append(imgs, u)
			}
		}
	}
	if text == "" {
		text = "（工具返回了一张图片）"
	}
	return text, imgs
}

// wireLen：Content 可能是 string 或 []parts，估算字符数即可（只用于预算判断）。
func wireLen(v any) int {
	switch t := v.(type) {
	case string:
		return len(t)
	case []map[string]any:
		n := 0
		for _, p := range t {
			if s, ok := p["text"].(string); ok {
				n += len(s)
			}
		}
		return n
	}
	return 0
}
