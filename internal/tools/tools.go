// Package tools：agent 能用的工具。返回 text，或 text+image 的内容数组（由 web 层决定怎么展示）。
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"agentd/internal/store"
	"time"
)

// Env：工具需要的会话上下文（recall 用）。Messages 传 []store.Message，避免包循环。
type Env struct {
	Messages any
}

type Spec struct {
	Name        string
	Description string
	Params      map[string]any // JSON Schema properties
	Required    []string
	Run         func(ctx context.Context, args map[string]any, env *Env) (any, error)
}

func Registry() map[string]*Spec {
	m := map[string]*Spec{}
	for _, s := range []*Spec{
		{Name: "bash", Description: "在本机执行一条 shell 命令（默认 180 秒超时，到点杀整个进程组）。",
			Params: map[string]any{"command": map[string]any{"type": "string", "description": "完整 shell 命令"},
				"timeout": map[string]any{"type": "integer", "description": "超时秒数，默认 180，最大 600"}},
			Required: []string{"command"}, Run: runBash},
		{Name: "screenshot", Description: "截屏并返回文字说明 + 截屏原图（模型能看到像素）。用于判断窗口内容/排版/报错。",
			Params: map[string]any{"question": map[string]any{"type": "string", "description": "想让它重点看什么，可留空"}},
			Run:    runScreenshot},
		{Name: "kb_search", Description: "检索本机知识库（权威事实与经验）。写知识库前先读约定：kb help --write。",
			Params:   map[string]any{"query": map[string]any{"type": "string"}, "max_results": map[string]any{"type": "integer"}},
			Required: []string{"query"}, Run: runKBSearch},
		{Name: "recall", Description: "在**本会话**历史里检索（不跨会话）。默认在消息正文里按关键词 OR 匹配，返回命中的片段与序号。" +
			"需要正则时加 re=true（Go RE2 语法，示例: (鹈鹕|pelican).*(车|bike)）。" +
			"想看某条附近上下文用 around=<序号>。level=raw 才返回整段原文（贵，慎用）。" +
			"软限制：一次最多 3 条；不够就先问用户，不要反复 recall。",
			Params: map[string]any{
				"query":  map[string]any{"type": "string", "description": "关键词（空格分词，默认 OR）或正则"},
				"re":     map[string]any{"type": "boolean", "description": "query 按正则解释"},
				"all":    map[string]any{"type": "boolean", "description": "关键词全部命中（AND）"},
				"not":    map[string]any{"type": "string", "description": "排除词"},
				"around": map[string]any{"type": "integer", "description": "序号；返回它附近 ±2 条"},
				"level":  map[string]any{"type": "string", "description": "summary(默认) 或 raw"},
				"limit":  map[string]any{"type": "integer", "description": "最多几条，默认 3"},
			}, Run: runRecall},
	} {
		m[s.Name] = s
	}
	applyOverrides(m)
	return m
}

func SchemaJSON(reg map[string]*Spec) []map[string]any {
	out := make([]map[string]any, 0, len(reg))
	for _, s := range reg {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": s.Name, "description": s.Description,
				"parameters": map[string]any{"type": "object", "properties": s.Params, "required": s.Required},
			},
		})
	}
	return out
}

func str(args map[string]any, k string) string {
	if v, ok := args[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func runBash(ctx context.Context, args map[string]any, _ *Env) (any, error) {
	cmd := str(args, "command")
	if strings.TrimSpace(cmd) == "" {
		return "[error] 缺少 command", nil
	}
	t := 180
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		t = int(v)
	}
	if t < 5 {
		t = 5
	}
	if t > 600 {
		t = 600
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(t)*time.Second)
	defer cancel()
	c := exec.CommandContext(cctx, "/bin/bash", "-lc", cmd)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Dir, _ = os.UserHomeDir()
	var out strings.Builder
	c.Stdout, c.Stderr = &out, &out
	// 先 Start（c.Process 由此确定），再用看门狗在超时时杀整个进程组，
	// 最后 Wait。必须先 Start 再起 goroutine，否则 c.Process 会和 Run 内部写入竞争。
	if err := c.Start(); err != nil {
		return "[error] " + err.Error(), nil
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-cctx.Done():
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		case <-done:
		}
	}()
	err := c.Wait()
	close(done)
	if cctx.Err() == context.DeadlineExceeded {
		if c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
		return fmt.Sprintf("[timeout] 命令超过 %d 秒，已杀掉进程组\n%s", t, tail(out.String(), 4000)), nil
	}
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return "[error] " + err.Error(), nil
		}
	}
	return fmt.Sprintf("%s\n[exit %d]", tail(out.String(), 8000), code), nil
}

func runScreenshot(ctx context.Context, args map[string]any, _ *Env) (any, error) {
	q := str(args, "question")
	if q == "" {
		q = "先逐字抄出每个窗口的标题和其中可读的文字，再描述布局。只报看到的东西，不要猜显示器排列。"
	}
	img := filepath.Join("/tmp", fmt.Sprintf("agentd-shot-%d.png", time.Now().UnixNano()))
	env := append(os.Environ(), "XDG_RUNTIME_DIR=/run/user/"+fmt.Sprint(os.Getuid()), "WAYLAND_DISPLAY=wayland-1")
	grim := exec.CommandContext(ctx, "grim", img)
	grim.Env = env
	if err := grim.Run(); err != nil {
		return "✗ 截图失败：" + err.Error(), nil
	}
	defer os.Remove(img)
	b, err := os.ReadFile(img)
	if err != nil {
		return "✗ 读截图失败：" + err.Error(), nil
	}
	// 返回 content parts：文本 + 图片。上层（llm/web）按需展开。
	return []map[string]any{
		{"type": "text", "text": "截屏完成，请基于下图回答：" + q},
		{"type": "image", "url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)},
	}, nil
}

func runKBSearch(ctx context.Context, args map[string]any, _ *Env) (any, error) {
	q := str(args, "query")
	n := 5
	if v, ok := args["max_results"].(float64); ok && v > 0 {
		n = int(v)
	}
	home, _ := os.UserHomeDir()
	c := exec.CommandContext(ctx, filepath.Join(home, ".local/bin/kb"), "search", q, "-n", fmt.Sprint(n))
	o, err := c.CombinedOutput()
	if err != nil {
		return "✗ kb search 失败：" + string(o), nil
	}
	return tail(string(o), 6000), nil
}

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…（前略）\n" + string(r[len(r)-n:])
}

var _ = json.Marshal

// runRecall：只搜本会话。RE2 线性时间（无回溯爆炸），外加 300ms 兜底超时。
func runRecall(ctx context.Context, args map[string]any, env *Env) (any, error) {
	if env == nil || env.Messages == nil {
		return "✗ 没有会话上下文可检索", nil
	}
	msgs, ok := env.Messages.([]store.Message)
	if !ok {
		return "✗ 会话上下文类型不对", nil
	}
	level := str(args, "level")
	if level == "" {
		level = "summary"
	}
	limit := 3
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	cctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	// --around：直接给附近几条
	if v, ok := args["around"].(float64); ok && v > 0 {
		center := int(v)
		var out []string
		for _, m := range msgs {
			if m.Seq >= center-2 && m.Seq <= center+2 {
				out = append(out, fmt.Sprintf("#%d [%s] %s", m.Seq, m.Role, truncate(m.Content, 400)))
			}
		}
		if len(out) == 0 {
			return fmt.Sprintf("（序号 %d 附近没有消息）", center), nil
		}
		return strings.Join(out, "\n"), nil
	}

	q := str(args, "query")
	if strings.TrimSpace(q) == "" {
		return "✗ 缺少 query", nil
	}
	var match func(string) bool
	if b, _ := args["re"].(bool); b {
		re, err := regexp.Compile(q)
		if err != nil {
			return "✗ 正则编译失败（Go RE2 语法）：" + err.Error(), nil
		}
		match = re.MatchString
	} else {
		words := strings.Fields(q)
		all, _ := args["all"].(bool)
		not := str(args, "not")
		match = func(s string) bool {
			ls := strings.ToLower(s)
			hit := 0
			for _, w := range words {
				if strings.Contains(ls, strings.ToLower(w)) {
					hit++
				}
			}
			if not != "" && strings.Contains(ls, strings.ToLower(not)) {
				return false
			}
			if all {
				return hit == len(words)
			}
			return hit > 0
		}
	}

	type hit struct {
		seq    int
		role   string
		snip   string
		approx bool
	}
	var hits []hit
	for _, m := range msgs {
		select {
		case <-cctx.Done():
			return "✗ 检索超时（300ms），请把 query 缩小一点", nil
		default:
		}
		body := m.Content
		if level == "summary" && m.Role == "tool" {
			body = truncate(body, 400) // 工具输出只截前 400 字参与匹配，避免大块文本
		}
		if match(body) {
			hits = append(hits, hit{m.Seq, m.Role, snippet(body, q, 60), level == "summary"})
			if len(hits) >= limit {
				break
			}
		}
	}
	if len(hits) == 0 {
		return "（本会话没有命中）", nil
	}
	var out []string
	for _, h := range hits {
		out = append(out, fmt.Sprintf("#%d [%s] %s", h.seq, h.role, h.snip))
	}
	return strings.Join(out, "\n"), nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// snippet：关键词附近 ±60 字；正则可选则退化为前 120 字。
func snippet(s, q string, window int) string {
	rs := []rune(s)
	low := strings.ToLower(s)
	if w := strings.Fields(q); len(w) > 0 && !strings.ContainsAny(q, "()|*+?[{\\") {
		if idx := strings.Index(low, strings.ToLower(w[0])); idx >= 0 {
			// 字符索引与 rune 索引不一致时退化为前段
			if idx+window < len(rs) && idx-window > 0 {
				return "…" + string(rs[idx-window:idx+window]) + "…"
			}
		}
	}
	return truncate(s, 120)
}
