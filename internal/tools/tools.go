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
	"strings"
	"syscall"
	"time"
)

type Spec struct {
	Name        string
	Description string
	Params      map[string]any // JSON Schema properties
	Required    []string
	Run         func(ctx context.Context, args map[string]any) (any, error)
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
	} {
		m[s.Name] = s
	}
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

func runBash(ctx context.Context, args map[string]any) (any, error) {
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
	err := c.Run()
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
	return fmt.Sprintf("[exit %d]\n%s", code, tail(out.String(), 8000)), nil
}

func runScreenshot(ctx context.Context, args map[string]any) (any, error) {
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

func runKBSearch(ctx context.Context, args map[string]any) (any, error) {
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
