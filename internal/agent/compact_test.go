package agent

import (
	"strings"
	"testing"

	"agentd/internal/llm"
)

func mk(n int) []llm.Message {
	// 交替 user/assistant，带 seq 不重要，这里只测压缩函数
	out := []llm.Message{{Role: "system", Content: "sys"}}
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		out = append(out, llm.Message{Role: role, Content: strings.Repeat("x", 100)})
	}
	return out
}

func TestCompactNoopWhenSmall(t *testing.T) {
	in := mk(4) // 4 条 < keep 16
	out := compact(in, 16, 1000000)
	if len(out) != len(in) {
		t.Fatalf("小上下文不应压缩: %d → %d", len(in), len(out))
	}
}

func TestCompactKeepsRecentAndSystem(t *testing.T) {
	in := mk(40)
	out := compact(in, 10, 1) // budget=1 强制压缩
	if out[0].Role != "system" || out[0].Content.(string) != "sys" {
		t.Fatalf("system 应保留: %+v", out[0])
	}
	if !strings.Contains(out[1].Content.(string), "上下文压缩") {
		t.Fatalf("应有省略提示: %v", out[1].Content)
	}
	if len(out) > 13 {
		t.Fatalf("应保留约 keep 条: %d", len(out))
	}
	// 保留的尾部要与原尾部一致
	if out[len(out)-1].Content.(string) != in[len(in)-1].Content.(string) {
		t.Fatal("尾部消息应原样保留")
	}
}

func TestCompactBoundaryIsUser(t *testing.T) {
	in := mk(30)
	out := compact(in, 5, 1)
	// 省略提示之后的第一条应当是 user（否则会把 tool_call 与结果拆开）
	if out[2].Role != "user" {
		t.Fatalf("切点应落在 user 上，得到 %q", out[2].Role)
	}
}
