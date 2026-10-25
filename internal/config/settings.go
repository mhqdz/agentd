package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings 是全局常规配置（与 profile / 工具说明同级）。
type Settings struct {
	ToolRoundsEnabled bool `json:"tool_rounds_enabled"` // 是否限制工具轮次
	ToolRounds        int  `json:"tool_rounds"`         // 限制值（默认 8）
}

func settingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".data", "agentd", "settings.json")
}

func LoadSettings() Settings {
	s := Settings{ToolRoundsEnabled: true, ToolRounds: 8}
	b, err := os.ReadFile(settingsPath())
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.ToolRounds <= 0 {
		s.ToolRounds = 8
	}
	return s
}

func SaveSettings(s Settings) error {
	if s.ToolRounds <= 0 {
		s.ToolRounds = 8
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0o755); err != nil {
		return err
	}
	tmp := settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, settingsPath())
}
