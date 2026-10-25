package store

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// 会话编号：进程内互斥 + 持久化计数器（单后端进程，锁足够）。
// 首次使用或计数器丢失时，扫描 live/archive 目录里最大的纯数字 id 作为起点。
var seqOnce bool

func (s *JSONStore) seqPath() string { return filepath.Join(s.Dir, "seq") }

func (s *JSONStore) loadSeq() int {
	if b, err := os.ReadFile(s.seqPath()); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n > 0 {
			return n
		}
	}
	max := 0
	for _, dir := range []string{s.liveDir(), s.archiveDir()} {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			name := e.Name()
			if !strings.HasPrefix(name, "conv-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			id := strings.TrimSuffix(strings.TrimPrefix(name, "conv-"), ".json")
			if n, err := strconv.Atoi(id); err == nil && n > max {
				max = n
			}
		}
	}
	return max
}

// NextID 返回下一个顺序编号（纯数字字符串）。
func (s *JSONStore) NextID() string {
	seqMu.Lock()
	defer seqMu.Unlock()
	n := s.loadSeq()
	if !seqOnce {
		seqOnce = true
	}
	n++
	_ = os.MkdirAll(s.Dir, 0o755)
	_ = os.WriteFile(s.seqPath(), []byte(strconv.Itoa(n)), 0o644)
	return strconv.Itoa(n)
}
