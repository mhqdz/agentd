package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OverridePath 返回工具描述覆盖文件（UI 里改过的说明写在这里）。
func OverridePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".data", "agentd", "tools.json")
}

// LoadOverrides 读取 {"bash":{"description":"…"}} 结构。
func LoadOverrides() map[string]map[string]string {
	b, err := os.ReadFile(OverridePath())
	if err != nil {
		return map[string]map[string]string{}
	}
	out := map[string]map[string]string{}
	if json.Unmarshal(b, &out) != nil {
		return map[string]map[string]string{}
	}
	return out
}

// SaveOverride 写入某个工具的描述覆盖。
func SaveOverride(name, desc string) error {
	all := LoadOverrides()
	if all[name] == nil {
		all[name] = map[string]string{}
	}
	all[name]["description"] = desc
	b, _ := json.MarshalIndent(all, "", "  ")
	if err := os.MkdirAll(filepath.Dir(OverridePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(OverridePath(), b, 0o644)
}

// applyOverrides 把覆盖后的描述写回注册表。
func applyOverrides(m map[string]*Spec) {
	for name, kv := range LoadOverrides() {
		if sp, ok := m[name]; ok {
			if d, ok := kv["description"]; ok && d != "" {
				sp.Description = d
			}
		}
	}
}
