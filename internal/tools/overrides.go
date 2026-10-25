package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OverridePath 返回工具配置覆盖文件（UI 里改过的说明/开关/子工具写在这里）。
func OverridePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".data", "agentd", "tools.json")
}

// SubTool 是一个工具下的子操作（如 kb 下的 kb_search / kb_get）。
type SubTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// Override 是单个工具的可编辑配置。
type Override struct {
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`  // false = 关闭（不暴露给模型）
	Subtools    []SubTool `json:"subtools"` // 子工具（可选）
}

// LoadOverrides 读取 {"bash":{...}} 结构；老格式（只有 description 字符串映射）也能读。
func LoadOverrides() map[string]Override {
	raw := map[string]json.RawMessage{}
	b, err := os.ReadFile(OverridePath())
	if err != nil {
		return map[string]Override{}
	}
	if json.Unmarshal(b, &raw) != nil {
		return map[string]Override{}
	}
	out := map[string]Override{}
	for k, v := range raw {
		var ov Override
		var legacy map[string]string
		if json.Unmarshal(v, &ov) == nil && ov.Description != "" {
			out[k] = ov
			continue
		}
		if json.Unmarshal(v, &legacy) == nil {
			out[k] = Override{Description: legacy["description"], Enabled: true}
		}
	}
	// Enabled 默认为 true（未配置过时）
	for k, v := range out {
		if _, ok := mapKeys(raw)[k]; ok && v.Description != "" && !v.Enabled {
			v.Enabled = true
			out[k] = v
		}
	}
	return out
}

func mapKeys(m map[string]json.RawMessage) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// SaveOverride 写入某个工具的完整配置。
func SaveOverride(name string, ov Override) error {
	all := LoadOverrides()
	all[name] = ov
	b, _ := json.MarshalIndent(all, "", "  ")
	if err := os.MkdirAll(filepath.Dir(OverridePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(OverridePath(), b, 0o644)
}

// applyOverrides 把覆盖后的描述/开关写回注册表（关闭的工具直接从表里删掉）。
func applyOverrides(m map[string]*Spec) {
	for name, ov := range LoadOverrides() {
		sp, ok := m[name]
		if !ok {
			continue
		}
		if ov.Description != "" {
			sp.Description = ov.Description
		}
		// 子工具：把可用的子操作补进说明，让模型知道怎么用
		var subs []string
		for _, st := range ov.Subtools {
			if st.Enabled {
				subs = append(subs, st.Name)
			}
		}
		if len(subs) > 0 {
			sp.Description += "（子工具：" + joinComma(subs) + "）"
		}
		if ov.Enabled == false && ov.Description != "" { // 只有被显式配置过开关才生效
			delete(m, name)
		}
	}
}

func joinComma(a []string) string {
	out := ""
	for i, s := range a {
		if i > 0 {
			out += "、"
		}
		out += s
	}
	return out
}
