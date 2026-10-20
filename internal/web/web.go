// Package web：P1 的 HTTP + SSE 层。前端只跟这里的接口说话。
package web

import (
	"context"
	"encoding/json"
	"fmt"

	"net/http"
	"os"
	"strings"
	"time"

	"agentd/internal/agent"
	"agentd/internal/config"
	"agentd/internal/store"
)

type Server struct {
	cfg config.Config
	a   *agent.Agent
	st  store.Store
}

func New(cfg config.Config, a *agent.Agent, st store.Store) *Server {
	return &Server{cfg: cfg, a: a, st: st}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "model": s.cfg.Model, "base_url": s.cfg.BaseURL})
	})
	mux.HandleFunc("/api/convs", s.handleConvs)
	mux.HandleFunc("/api/conv/", s.handleConv)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/profiles", s.handleProfiles)
	mux.HandleFunc("/api/profiles/", s.handleProfileItem)
	mux.HandleFunc("/", s.handleStatic)
	return logReq(mux)
}

func logReq(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleConvs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "GET only"})
		return
	}
	list, err := s.st.List()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	type row struct {
		ID, Title string
		UpdatedAt time.Time
		N         int
	}
	out := make([]row, 0, len(list))
	for _, c := range list {
		out = append(out, row{c.ID, c.Title, c.UpdatedAt, len(c.Messages)})
	}
	writeJSON(w, 200, out)
}

// GET /api/conv/<id>
func (s *Server) handleConv(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/conv/")
	c, err := s.st.Get(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "没有这个会话"})
		return
	}
	writeJSON(w, 200, c)
}

// POST /api/chat {"conv_id":"","text":"..."}  → SSE: event: delta / event: done
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "POST only"})
		return
	}
	var in struct {
		ConvID  string `json:"conv_id"`
		Text    string `json:"text"`
		Profile string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Text) == "" {
		writeJSON(w, 400, map[string]string{"error": "需要 text"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "不支持流式"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func(ev string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev, b)
		flusher.Flush()
	}

	ag := s.a
	if in.Profile != "" {
		if prof, ok := config.GetProfile(in.Profile); ok {
			ag = agent.New(s.cfg, s.st, prof)
		}
	}
	c, err := ag.Conversation(in.ConvID)
	if err != nil {
		send("error", map[string]string{"error": err.Error()})
		return
	}
	send("meta", map[string]string{"conv_id": c.ID, "title": c.Title})

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	start := time.Now()
	err = ag.SendWithTools(ctx, c, in.Text,
		func(content, reasoning string) {
			if content != "" {
				send("delta", map[string]string{"text": content})
			}
			if reasoning != "" {
				send("reasoning", map[string]string{"text": reasoning})
			}
		},
		func(name, args string, result any) {
			text := ""
			hasImage := false
			if parts, ok := result.([]map[string]any); ok {
				for _, pt := range parts {
					if pt["type"] == "text" {
						if t, ok := pt["text"].(string); ok {
							text += t
						}
					}
					if pt["type"] == "image" {
						hasImage = true
					}
				}
			} else if str, ok := result.(string); ok {
				text = str
			}
			send("tool", map[string]any{"name": name, "args": args, "text": text, "image": hasImage})
		})
	if err != nil {
		send("error", map[string]string{"error": err.Error()})
		return
	}
	send("done", map[string]any{"conv_id": c.ID, "secs": time.Since(start).Seconds()})
}

// 静态文件：优先 embed（P4 会填 web/dist），没有就返回占位页。
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page, err := os.ReadFile(s.cfg.WebDir + "/index.html")
	if err != nil {
		fmt.Fprint(w, placeholder)
		return
	}
	w.Write(page)
}

const placeholder = `<!doctype html><meta charset="utf-8"><title>agentd</title>
<style>body{background:#0f1115;color:#d7dae0;font:14px/1.6 ui-monospace,monospace;padding:2rem}</style>
<h3>agentd</h3><p>API 已就绪；前端还没构建（P4）。</p>
<ul><li>GET /api/health</li><li>GET /api/convs</li><li>POST /api/chat</li></ul>`

// GET/POST /api/profiles  ·  PUT/DELETE /api/profiles/<name>
func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list := config.LoadProfiles()
		writeJSON(w, 200, list)
	case http.MethodPost, http.MethodPut:
		var p config.Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := config.UpsertProfile(p); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "profile": p.Name})
	default:
		writeJSON(w, 405, map[string]string{"error": "GET/POST/PUT only"})
	}
}

func (s *Server) handleProfileItem(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	switch r.Method {
	case http.MethodGet:
		if p, ok := config.GetProfile(name); ok {
			writeJSON(w, 200, p)
			return
		}
		writeJSON(w, 404, map[string]string{"error": "没有这个 profile"})
	case http.MethodDelete:
		if err := config.DeleteProfile(name); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeJSON(w, 405, map[string]string{"error": "GET/DELETE only"})
	}
}
