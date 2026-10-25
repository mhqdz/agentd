package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"agentd/internal/llm"
)

// SQLiteStore：一个会话一行，消息一列一行（消息只追加，不二次编辑）。
type SQLiteStore struct{ db *sql.DB }

func NewSQLite(dir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "agentd.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS conversations(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  summary_upto INTEGER NOT NULL DEFAULT 0,
  summary_model TEXT NOT NULL DEFAULT '',
  summary_at TEXT,
  kv TEXT NOT NULL DEFAULT '{}',
  archived INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS messages(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  seq INTEGER NOT NULL DEFAULT 0,
  conv_id INTEGER NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  reasoning TEXT NOT NULL DEFAULT '',
  tool_calls TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  tool_result TEXT NOT NULL DEFAULT '',
  images TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS idx_msg_conv_seq ON messages(conv_id, seq);
`)
	if err != nil {
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) NextID() string { return "" } // 由 AUTOINCREMENT 负责

func (s *SQLiteStore) Get(id string) (*Conversation, error) {
	var n int
	if _, err := fmt.Sscanf(id, "%d", &n); err != nil {
		return nil, os.ErrNotExist
	}
	var c Conversation
	var kv, sat, cat, uat string
	err := s.db.QueryRow(`SELECT id,title,summary,summary_upto,summary_model,COALESCE(summary_at,''),kv,created_at,updated_at
		FROM conversations WHERE id=? AND archived=0`, n).
		Scan(&n, &c.Title, &c.Summary, &c.SummaryUpto, &c.SummaryModel, &sat, &kv, &cat, &uat)
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, cat)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, uat)
	if err != nil {
		return nil, os.ErrNotExist
	}
	c.ID = fmt.Sprint(n)
	_ = json.Unmarshal([]byte(kv), &c.KV)
	if sat != "" {
		if t, err := time.Parse(time.RFC3339Nano, sat); err == nil {
			c.SummaryAt = t
		}
	}
	ms, err := s.messages(n)
	if err != nil {
		return nil, err
	}
	c.Messages = ms
	return &c, nil
}

func (s *SQLiteStore) messages(convID int) ([]Message, error) {
	rows, err := s.db.Query(`SELECT seq,role,content,reasoning,tool_calls,tool_call_id,tool_result,images,created_at
		FROM messages WHERE conv_id=? ORDER BY seq`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var tc, tr, im, ca string
		var seq int
		if err := rows.Scan(&seq, &m.Role, &m.Content, &m.Reasoning, &tc, &m.ToolCallID, &tr, &im, &ca); err != nil {
			return nil, err
		}
		m.Seq = seq
		if tc != "" {
			_ = json.Unmarshal([]byte(tc), &m.ToolCalls)
		}
		if tr != "" {
			var v any
			if json.Unmarshal([]byte(tr), &v) == nil {
				m.ToolResult = v
			}
		}
		if im != "" {
			_ = json.Unmarshal([]byte(im), &m.Images)
		}
		if t, err := time.Parse(time.RFC3339Nano, ca); err == nil {
			m.CreatedAt = t
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *SQLiteStore) Save(c *Conversation) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now()
	nStr := now.Format(time.RFC3339Nano)
	kv, _ := json.Marshal(c.KV)
	var sat any
	if !c.SummaryAt.IsZero() {
		sat = c.SummaryAt.Format(time.RFC3339Nano)
	}
	if c.ID == "" {
		res, err := tx.Exec(`INSERT INTO conversations(title,summary,summary_upto,summary_model,summary_at,kv,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?)`, c.Title, c.Summary, c.SummaryUpto, c.SummaryModel, sat, string(kv), nStr, nStr)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		c.ID = fmt.Sprint(id)
	} else {
		if _, err := tx.Exec(`UPDATE conversations SET title=?,summary=?,summary_upto=?,summary_model=?,summary_at=?,kv=?,updated_at=? WHERE id=?`,
			c.Title, c.Summary, c.SummaryUpto, c.SummaryModel, sat, string(kv), nStr, c.ID); err != nil {
			return err
		}
	}
	// 只追加新消息（按 Seq 是否存在判断，绝不改历史 ✓）
	for i := range c.Messages {
		m := &c.Messages[i]
		if m.Seq != 0 {
			continue
		}
		tc, _ := json.Marshal(m.ToolCalls)
		tr := ""
		if m.ToolResult != nil {
			b, _ := json.Marshal(m.ToolResult)
			tr = string(b)
		}
		im, _ := json.Marshal(m.Images)
		var nextSeq int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM messages WHERE conv_id=?`, c.ID).Scan(&nextSeq); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO messages(conv_id,seq,role,content,reasoning,tool_calls,tool_call_id,tool_result,images,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, c.ID, nextSeq, m.Role, m.Content, m.Reasoning, string(tc), m.ToolCallID, tr, string(im), nStr)
		if err != nil {
			return err
		}
		_ = res
		m.Seq = nextSeq // 会话内序号，回填后避免重复插入 ✓
	}
	c.UpdatedAt = now
	return tx.Commit()
}

func (s *SQLiteStore) list(archived int) ([]*Conversation, error) {
	rows, err := s.db.Query(`SELECT id,title,created_at,updated_at FROM conversations WHERE archived=? ORDER BY updated_at DESC`, archived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Conversation
	for rows.Next() {
		var c Conversation
		var id int
		var cat, uat string
		if err := rows.Scan(&id, &c.Title, &cat, &uat); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, cat)
		c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, uat)
		c.ID = fmt.Sprint(id)
		out = append(out, &c)
	}
	return out, nil
}

func (s *SQLiteStore) List() ([]*Conversation, error)         { return s.list(0) }
func (s *SQLiteStore) ListArchived() ([]*Conversation, error) { return s.list(1) }

func (s *SQLiteStore) Archive(id string) error {
	_, err := s.db.Exec(`UPDATE conversations SET archived=1 WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) Restore(id string) error {
	_, err := s.db.Exec(`UPDATE conversations SET archived=0 WHERE id=?`, id)
	return err
}

func (s *SQLiteStore) Delete(id string, hard bool) error {
	if hard {
		if _, err := s.db.Exec(`DELETE FROM messages WHERE conv_id=?`, id); err != nil {
			return err
		}
		_, err := s.db.Exec(`DELETE FROM conversations WHERE id=?`, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE conversations SET archived=1 WHERE id=?`, id)
	return err
}

var _ Store = (*SQLiteStore)(nil)
var _ = llm.ToolCall{}
