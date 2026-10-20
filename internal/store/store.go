// Package store：P0 用 JSON 文件存会话（接口留好，P1 可换 sqlite）。
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Reasoning string    `json:"reasoning,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Messages  []Message `json:"messages"`
}

type Store interface {
	Get(id string) (*Conversation, error)
	Save(c *Conversation) error
	List() ([]*Conversation, error)
}

type JSONStore struct{ Dir string }

func NewJSON(dir string) (*JSONStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &JSONStore{Dir: dir}, nil
}

func (s *JSONStore) file(id string) string { return filepath.Join(s.Dir, "conv-"+id+".json") }

func (s *JSONStore) Get(id string) (*Conversation, error) {
	b, err := os.ReadFile(s.file(id))
	if err != nil {
		return nil, err
	}
	var c Conversation
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *JSONStore) Save(c *Conversation) error {
	c.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file(c.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.file(c.ID)) // 原子替换
}

func (s *JSONStore) List() ([]*Conversation, error) {
	ents, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	var out []*Conversation
	for _, e := range ents {
		if len(e.Name()) < 5 || e.Name()[:5] != "conv-" || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[5 : len(e.Name())-5]
		if c, err := s.Get(id); err == nil {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
