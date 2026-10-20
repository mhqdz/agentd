// Package config 只做一件事：决定"跟谁说话"。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	BaseURL        string `json:"base_url"` // 例如 http://127.0.0.1:18320/9/v1（走 path 路由）
	Model          string `json:"model"`
	APIKey         string `json:"api_key"`
	DataDir        string `json:"data_dir"`         // JSON 存储目录（P0 先不用 sqlite）
	WebDir         string `json:"web_dir"`          // 前端构建产物目录
	System         string `json:"system"`           // 系统提示（空 = 不加）
	MaxPromptChars int    `json:"max_prompt_chars"` // 单次请求 messages 的字符上限，超过就拒绝（防爆炸）
	KeepMessages   int    `json:"keep_messages"`
}

func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		BaseURL: "http://127.0.0.1:18320/9/v1",
		Model:   "Qwen3.5-9B-int4-ov",
		APIKey:  "local-no-auth",
		DataDir: filepath.Join(home, ".data/agentd"),
		WebDir:  "web/dist",
		System: `你是本机 agent。只回答用户要的东西；需要本机事实（版本/路径/进程/屏幕）时才用工具。
不要为了"验证"而反复调用工具：生成内容就直接输出，不要写文件再读回来。
输出代码时用围栏代码块，不要解释。`,
		MaxPromptChars: 240000,
		KeepMessages:   16,
	}
}

func path() string {
	if v := os.Getenv("AGENTD_CONFIG"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config/agentd/config.json")
}

// Load 读配置；文件不存在就返回默认值（不创建文件，避免无意义副作用）。
func Load() Config {
	c := Default()
	if b, err := os.ReadFile(path()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if v := os.Getenv("AGENTD_BASE_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("AGENTD_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("AGENTD_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("AGENTD_WEB_DIR"); v != "" {
		c.WebDir = v
	}
	return c
}
