// Package config 只做一件事：决定"跟谁说话"。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	BaseURL string `json:"base_url"` // 例如 http://127.0.0.1:18320/9/v1（走 path 路由）
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
	DataDir string `json:"data_dir"` // JSON 存储目录（P0 先不用 sqlite）
}

func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		BaseURL: "http://127.0.0.1:18320/9/v1",
		Model:   "Qwen3.5-9B-int4-ov",
		APIKey:  "local-no-auth",
		DataDir: filepath.Join(home, ".local/state/agentd"),
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
	return c
}
