// Package llm：OpenAI 兼容客户端，只实现本机用得到的部分（流式 + 非流式）。
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	Stream      bool      `json:"stream"`
}

type Chunk struct {
	Delta     string // 正文增量
	Reasoning string // 思考增量（Strata 的 reasoning_content / 9B 若开思考也有）
	Done      bool
}

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{BaseURL: baseURL, APIKey: apiKey, HTTP: &http.Client{Timeout: 0}}
}

func (c *Client) post(ctx context.Context, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return c.HTTP.Do(req)
}

// Stream 逐块返回增量；onChunk 返回错误即中止。
func (c *Client) Stream(ctx context.Context, req ChatRequest, onChunk func(Chunk) error) error {
	req.Stream = true
	resp, err := c.post(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return fmt.Errorf("upstream %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return onChunk(Chunk{Done: true})
		}
		var ev struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue // 容忍非标准块
		}
		for _, ch := range ev.Choices {
			d := ch.Delta
			reasoning := d.ReasoningContent
			if reasoning == "" {
				reasoning = d.Reasoning
			}
			if d.Content != "" || reasoning != "" {
				if err := onChunk(Chunk{Delta: d.Content, Reasoning: reasoning}); err != nil {
					return err
				}
			}
		}
	}
	return sc.Err()
}

// Chat 非流式（P0 的 REPL 也用流式，这个留给测试和工具阶段）。
func (c *Client) Chat(ctx context.Context, req ChatRequest) (string, error) {
	req.Stream = false
	resp, err := c.post(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return "", fmt.Errorf("upstream %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("empty choices")
	}
	return out.Choices[0].Message.Content, nil
}

var _ = time.Second
