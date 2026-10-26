// Package web：P1 的 HTTP + SSE 层。前端只跟这里的接口说话。
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"agentd/internal/logx"

	"net/http"
	"os"
	"strings"
	"time"

	"agentd/internal/agent"
	"agentd/internal/config"
	"agentd/internal/store"
	"agentd/internal/tools"
)

type Server struct {
	jobMu       sync.Mutex
	jobs        map[string]*job
	convMu      sync.Mutex
	convCancel  map[string]context.CancelFunc
	interjectMu sync.Mutex
	interject   map[string][]string
	cfg         config.Config
	a           *agent.Agent
	st          store.Store
}

func New(cfg config.Config, a *agent.Agent, st store.Store) *Server {
	s := &Server{cfg: cfg, a: a, st: st}
	logx.Init(filepath.Join(cfg.DataDir, ""))
	s.RunJobSweeper()
	s.RunAutoArchive()
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/shots/", func(w http.ResponseWriter, r *http.Request) {
		home, _ := os.UserHomeDir()
		w.Header().Set("Cache-Control", "no-store")
		http.FileServer(http.Dir(filepath.Join(home, ".data", "agentd"))).ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "model": s.cfg.Model, "base_url": s.cfg.BaseURL})
	})
	mux.HandleFunc("/api/convs", s.handleConvs)
	mux.HandleFunc("/api/conv/", s.handleConv)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/running", func(w http.ResponseWriter, r *http.Request) {
		s.convMu.Lock()
		ids := make([]string, 0, len(s.convCancel))
		for id := range s.convCancel {
			ids = append(ids, id)
		}
		s.convMu.Unlock()
		sort.Strings(ids)
		writeJSON(w, 200, ids)
	})
	mux.HandleFunc("/api/shots/cleanup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "POST only"})
			return
		}
		var in struct {
			Days int `json:"days"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Days <= 0 {
			in.Days = config.LoadSettings().ShotsKeepDays
		}
		n, bytes := s.cleanupShots(in.Days)
		writeJSON(w, 200, map[string]any{"removed": n, "freed_kb": bytes / 1024, "days": in.Days})
	})
	mux.HandleFunc("/api/shots/stats", func(w http.ResponseWriter, r *http.Request) {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".data", "agentd", "shots")
		ents, _ := os.ReadDir(dir)
		var n int
		var bytes int64
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			if info, err := e.Info(); err == nil {
				n++
				bytes += info.Size()
			}
		}
		writeJSON(w, 200, map[string]any{"count": n, "size_mb": float64(bytes) / 1024 / 1024})
	})
	mux.HandleFunc("/api/context", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("conv")
		name := r.URL.Query().Get("profile")
		if name == "" {
			name = r.URL.Query().Get("p")
		}
		prof, ok := config.GetProfile(name)
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "需要 profile"})
			return
		}
		ag := agent.New(s.cfg, s.st, prof)
		c, err := s.st.Get(id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		chars, max := ag.ContextSize(c)
		raw := 0 // 未压缩时的字符数（给人看真实体量）
		for _, m := range c.Messages {
			raw += len([]rune(m.Content)) + len([]rune(m.Reasoning))
		}
		pct := 0.0
		if max > 0 {
			pct = float64(chars) / float64(max) * 100
		}
		compactPct := ag.CompactPct()
		compactAt := max * compactPct / 100
		writeJSON(w, 200, map[string]any{"chars": chars, "max": max, "pct": pct, "raw": raw,
			"compact_pct": compactPct, "compact_at": compactAt,
			"messages": len(c.Messages), "summary": c.Summary != ""})
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, config.LoadSettings())
		case http.MethodPut, http.MethodPost:
			var in config.Settings
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			before := config.LoadSettings()
			if err := config.SaveSettings(in); err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			after := config.LoadSettings()
			// 只在"自动归档"相关配置变化时，立即巡检一次（其它配置保存不触发）
			if before.AutoArchiveEnabled != after.AutoArchiveEnabled ||
				before.AutoArchiveDays != after.AutoArchiveDays {
				logx.Info("settings.auto_archive enabled=%v days=%d", after.AutoArchiveEnabled, after.AutoArchiveDays)
				go s.sweepAutoArchive()
			}
			writeJSON(w, 200, after)
		default:
			writeJSON(w, 405, map[string]string{"error": "GET/PUT only"})
		}
	})
	mux.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		base := strings.TrimRight(r.URL.Query().Get("base_url"), "/")
		key := r.URL.Query().Get("api_key")
		if base == "" {
			writeJSON(w, 400, map[string]string{"error": "需要 base_url"})
			return
		}
		req, err := http.NewRequest("GET", base+"/models", nil)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		req.Close = true
		cl := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DisableCompression: true}}
		resp, err := cl.Do(req)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": "连不上：" + err.Error()})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			writeJSON(w, 502, map[string]string{"error": fmt.Sprintf("接口返回 %d（检查地址或 API Key）", resp.StatusCode)})
			return
		}
		var out struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			writeJSON(w, 502, map[string]string{"error": "返回格式不是 OpenAI /v1/models"})
			return
		}
		ids := make([]string, 0, len(out.Data))
		for _, d := range out.Data {
			ids = append(ids, d.ID)
		}
		sort.Strings(ids)
		writeJSON(w, 200, map[string]any{"models": ids})
	})
	mux.HandleFunc("/api/tools", func(w http.ResponseWriter, r *http.Request) {
		reg := tools.Registry()
		out := make([]map[string]any, 0, len(reg))
		for _, sp := range reg {
			out = append(out, map[string]any{"name": sp.Name, "description": sp.Description, "params": sp.Params})
		}
		sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("/api/tools/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeJSON(w, 405, map[string]string{"error": "PUT only"})
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/tools/")
		var body struct {
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if _, ok := tools.Registry()[name]; !ok {
			writeJSON(w, 404, map[string]string{"error": "没有这个工具"})
			return
		}
		logx.Info("tool.save name=%s chars=%d", name, len([]rune(body.Description)))
		if err := tools.SaveOverride(name, body.Description); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/profiles", s.handleProfiles)
	mux.HandleFunc("/api/profiles/", s.handleProfileItem)
	mux.HandleFunc("/", s.handleStatic)
	return logReq(mux)
}

func logReq(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/" {
			ua := r.UserAgent()
			if len(ua) > 28 {
				ua = ua[:28]
			}
			fmt.Fprintf(os.Stderr, "[web] %s %s ua=%q\n", r.Method, r.URL.Path, ua)
		}
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
	var list []*store.Conversation
	var err error
	if r.URL.Query().Get("archived") == "1" {
		list, err = s.st.ListArchived()
	} else {
		list, err = s.st.List()
	}
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
		n := c.MsgCount
		if n == 0 {
			n = len(c.Messages)
		}
		out = append(out, row{c.ID, c.Title, c.UpdatedAt, n})
	}
	writeJSON(w, 200, out)
}

func (s *Server) pushInterject(convID, text string) {
	s.interjectMu.Lock()
	defer s.interjectMu.Unlock()
	if s.interject == nil {
		s.interject = map[string][]string{}
	}
	s.interject[convID] = append(s.interject[convID], text)
}

func (s *Server) drainInterject(convID string) []string {
	s.interjectMu.Lock()
	defer s.interjectMu.Unlock()
	q := s.interject[convID]
	delete(s.interject, convID)
	return q
}

// GET /api/conv/<id>
func (s *Server) handleConv(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/conv/")
	if strings.HasSuffix(raw, "/stream") {
		s.streamFrom(w, r, strings.TrimSuffix(raw, "/stream"))
		return
	}
	if strings.HasSuffix(raw, "/stop") {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "POST only"})
			return
		}
		id := strings.TrimSuffix(raw, "/stop")
		s.convMu.Lock()
		cancel := s.convCancel[id]
		s.convMu.Unlock()
		if cancel == nil {
			writeJSON(w, 404, map[string]string{"error": "这一轮已经结束"})
			return
		}
		cancel()
		writeJSON(w, 200, map[string]any{"ok": true, "stopped": id})
		return
	}
	if strings.HasSuffix(raw, "/interject") {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "POST only"})
			return
		}
		id := strings.TrimSuffix(raw, "/interject")
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
			writeJSON(w, 400, map[string]string{"error": "需要 text"})
			return
		}
		s.pushInterject(id, body.Text)
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if strings.HasSuffix(raw, "/restore") && r.Method == http.MethodPost {
		id := strings.TrimSuffix(raw, "/restore")
		logx.Info("conv.restore id=%s", id)
		if err := s.st.Restore(id); err != nil {
			writeJSON(w, 404, map[string]string{"error": "恢复失败：" + err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "restored": id})
		return
	}
	id := raw
	if r.Method == http.MethodDelete {
		logx.Info("conv.archive id=%s", id)
		if err := s.st.Archive(id); err != nil {
			writeJSON(w, 404, map[string]string{"error": "归档失败：" + err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "archived": id})
		return
	}
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

	var sendMu sync.Mutex
	var jb *job
	send := func(ev string, v any) {
		b, _ := json.Marshal(v)
		sendMu.Lock()
		defer sendMu.Unlock()
		frame := fmt.Sprintf("event: %s\ndata: %s\n\n", ev, b)
		if jb != nil {
			jb.emit(frame)
		}
		fmt.Fprint(w, frame)
		flusher.Flush()
	}

	// 心跳：工具执行可能几十秒没有任何帧，不加心跳连接会被浏览器/系统判为空闲断掉 ✗
	// 只写注释帧 ": ping"，不会进入事件流解析（前端忽略）
	stopBeat := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopBeat:
				return
			case <-t.C:
				sendMu.Lock()
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
				sendMu.Unlock()
			}
		}
	}()
	defer close(stopBeat)

	ag := s.a
	set := config.LoadSettings()
	if set.ToolRoundsEnabled {
		ag.MaxRounds = set.ToolRounds
	}
	if in.Profile != "" {
		if prof, ok := config.GetProfile(in.Profile); ok {
			ag = agent.New(s.cfg, s.st, prof)
			if set.ToolRoundsEnabled {
				ag.MaxRounds = set.ToolRounds
			}
		}
	}
	c, err := ag.Conversation(in.ConvID)
	if err != nil {
		logx.Error("chat.error conv=%s err=%v", in.ConvID, err)
		send("error", map[string]string{"error": err.Error()})
		return
	}
	// 会话一建立就先落盘：即使这一轮被中断，会话本身与用户消息也不会丢
	_ = s.st.Save(c)
	jb = s.job(c.ID)
	jb.reset()
	defer jb.finish()
	send("meta", map[string]string{"conv_id": c.ID, "title": c.Title})

	// 中途插话：本请求期间可被 /api/conv/<id>/interject 追加
	convID := c.ID
	ag.Drain = func() []string { return s.drainInterject(convID) }
	ag.OnInterject = func(t string) { send("interject", map[string]string{"text": t}) }

	// 后台任务：**不绑 r.Context()** —— 浏览器刷新/断开不会中断这一轮
	ctx, cancel := context.WithCancel(context.Background())
	s.convMu.Lock()
	if s.convCancel == nil {
		s.convCancel = map[string]context.CancelFunc{}
	}
	s.convCancel[convID] = cancel
	s.convMu.Unlock()
	defer func() {
		s.convMu.Lock()
		delete(s.convCancel, convID)
		s.convMu.Unlock()
	}()
	start := time.Now()
	toolStart := start
	logx.Info("chat.start conv=%s profile=%s chars=%d", convID, ag.Profile().Name, len([]rune(in.Text)))
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
			logx.Info("tool.call name=%s ms=%d image=%v out=%d",
				name, time.Since(toolStart).Milliseconds(), hasImage, len([]rune(text)))
			toolStart = time.Now()
			send("tool", map[string]any{"name": name, "args": args, "text": text, "image": hasImage})
		})
	if err != nil {
		logx.Error("chat.error conv=%s err=%v", in.ConvID, err)
		send("error", map[string]string{"error": err.Error()})
		return
	}
	logx.Info("chat.done conv=%s secs=%.1f", c.ID, time.Since(start).Seconds())
	send("done", map[string]any{"conv_id": c.ID, "secs": time.Since(start).Seconds()})
}

// 静态文件：优先 embed（P4 会填 web/dist），没有就返回占位页。
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebDir != "" {
		if _, err := os.Stat(s.cfg.WebDir + "/index.html"); err == nil {
			// 带 hash 的资源可以长缓存；index.html 必须不缓存，否则改了页面刷不出来
			if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
				w.Header().Set("Cache-Control", "no-store, must-revalidate")
			}
			http.FileServer(http.Dir(s.cfg.WebDir)).ServeHTTP(w, r)
			return
		}
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// 静态资源：dist 下的文件（/assets/*.js|css 等）；缺文件时给占位页
	if s.cfg.WebDir != "" {
		if _, err := os.Stat(s.cfg.WebDir + "/index.html"); err == nil {
			// 带 hash 的资源可以长缓存；index.html 必须不缓存，否则改了页面刷不出来
			if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
				w.Header().Set("Cache-Control", "no-store, must-revalidate")
			}
			http.FileServer(http.Dir(s.cfg.WebDir)).ServeHTTP(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, placeholder)
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

// sweepAutoArchive：按当前设置执行一次自动归档
// cleanupShots：删除 shots 目录里超过 days 天的截图，返回删除数量与释放字节
func (s *Server) cleanupShots(days int) (int, int64) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".data", "agentd", "shots")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	var n int
	var bytes int64
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			n++
			bytes += info.Size()
		}
	}
	if n > 0 {
		logx.Info("shots.cleanup removed=%d freed=%dKB days=%d", n, bytes/1024, days)
	}
	return n, bytes
}

func (s *Server) sweepAutoArchive() {
	st := config.LoadSettings()
	if st.ShotsAutoClean && st.ShotsKeepDays > 0 {
		s.cleanupShots(st.ShotsKeepDays)
	}
	if !st.AutoArchiveEnabled || st.AutoArchiveDays <= 0 {
		return
	}
	before := time.Now().Add(-time.Duration(st.AutoArchiveDays) * 24 * time.Hour)
	if n, err := s.st.ArchiveInactiveBefore(before); err == nil && n > 0 {
		logx.Info("auto_archive archived=%d days=%d", n, st.AutoArchiveDays)
	}
}

// RunAutoArchive：按设置把长期不活跃的会话自动归档（启动时一次，之后每小时一次）
func (s *Server) RunAutoArchive() {
	go func() {
		tick := func() {
			s.sweepAutoArchive()
		}
		tick()
		for range time.Tick(time.Hour) {
			tick()
		}
	}()
}
