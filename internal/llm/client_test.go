package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sseServer(blocks ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, b := range blocks {
			fmt.Fprint(w, b)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
}

func TestStreamParsesDeltas(t *testing.T) {
	srv := sseServer(
		"data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"想\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n",
		"data: [DONE]\n\n",
	)
	defer srv.Close()
	c := New(srv.URL, "")
	var content, reasoning strings.Builder
	err := c.Stream(context.Background(), ChatRequest{Model: "m"}, func(ch Chunk) error {
		if ch.Done {
			return nil
		}
		content.WriteString(ch.Delta)
		reasoning.WriteString(ch.Reasoning)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if content.String() != "你好" || reasoning.String() != "想" {
		t.Fatalf("content=%q reasoning=%q", content.String(), reasoning.String())
	}
}

func TestStreamToleratesGarbage(t *testing.T) {
	srv := sseServer(
		"data: not-json\n\n",
		": keepalive\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n",
		"data: [DONE]\n\n",
	)
	defer srv.Close()
	c := New(srv.URL, "")
	var got strings.Builder
	if err := c.Stream(context.Background(), ChatRequest{Model: "m"}, func(ch Chunk) error {
		got.WriteString(ch.Delta)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got.String() != "ok" {
		t.Fatalf("应容忍非标准块，得到 %q", got.String())
	}
}

func TestStreamUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"bad"}}`)
	}))
	defer srv.Close()
	c := New(srv.URL, "")
	err := c.Stream(context.Background(), ChatRequest{Model: "m"}, func(Chunk) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("应返回上游错误，得到 %v", err)
	}
}

func TestToolCallAccumulation(t *testing.T) {
	srv := sseServer(
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"comm\"}}]}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"and\\\":\\\"uname\\\"}\"}}]}}]}\n\n",
		"data: [DONE]\n\n",
	)
	defer srv.Close()
	c := New(srv.URL, "")
	var calls []ToolCall
	if err := c.StreamWithTools(context.Background(), ChatRequest{Model: "m"}, func(Chunk) error { return nil }, &calls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("应有 1 个 tool call，得到 %d", len(calls))
	}
	if calls[0].Function.Name != "bash" || calls[0].ID != "call_1" {
		t.Fatalf("name/id 不对: %+v", calls[0])
	}
	if strings.Join(strings.Fields(calls[0].Function.Arguments), "") != `{"command":"uname"}` {
		t.Fatalf("参数拼接不对: %q", calls[0].Function.Arguments)
	}
	if calls[0].Type != "function" {
		t.Fatalf("type 应为 function: %+v", calls[0])
	}
}
