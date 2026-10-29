package tools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestViewImage(t *testing.T) {
	// 1x1 透明 PNG
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	if err := os.WriteFile(p, png, 0o644); err != nil {
		t.Fatal(err)
	}
	sp := Registry()["view_image"]
	if sp == nil {
		t.Fatal("没有 view_image")
	}
	out, err := sp.Run(context.Background(), map[string]any{"path": p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parts, ok := out.([]map[string]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("返回形态不对: %#v", out)
	}
	if u, _ := parts[1]["url"].(string); !strings.HasPrefix(u, "data:image/png;base64,") {
		t.Fatalf("不是 data URL: %v", u)
	}
	// 不存在
	out, _ = sp.Run(context.Background(), map[string]any{"path": filepath.Join(dir, "nope.png")}, nil)
	if s, _ := out.(string); !strings.Contains(s, "文件不存在") {
		t.Fatalf("缺文件应报错: %v", out)
	}
	// 非图片
	txt := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(txt, []byte("hi"), 0o644)
	out, _ = sp.Run(context.Background(), map[string]any{"path": txt}, nil)
	if s, _ := out.(string); !strings.Contains(s, "不支持") {
		t.Fatalf("非图片应报错: %v", out)
	}
}
