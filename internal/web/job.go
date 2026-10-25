package web

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// job 保存一轮生成的 SSE 帧，供前端断点续传（刷新后逐字接上）。
type job struct {
	mu   sync.Mutex
	buf  []string
	done bool
}

func (s *Server) job(convID string) *job {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if s.jobs == nil {
		s.jobs = map[string]*job{}
	}
	j, ok := s.jobs[convID]
	if !ok {
		j = &job{}
		s.jobs[convID] = j
	}
	return j
}

func (j *job) reset() { j.mu.Lock(); j.buf = nil; j.done = false; j.mu.Unlock() }

func (j *job) emit(f string) { j.mu.Lock(); j.buf = append(j.buf, f); j.mu.Unlock() }

func (j *job) finish() { j.mu.Lock(); j.done = true; j.mu.Unlock() }

func (j *job) snapshot(from int) ([]string, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if from < 0 || from > len(j.buf) {
		from = len(j.buf)
	}
	out := make([]string, len(j.buf)-from)
	copy(out, j.buf[from:])
	return out, j.done
}

func (j *job) length() int { j.mu.Lock(); defer j.mu.Unlock(); return len(j.buf) }

// GET /api/conv/<id>/stream?from=N：先回放缓冲，再跟随后续帧；结束后返回。
func (s *Server) streamFrom(w http.ResponseWriter, r *http.Request, convID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "不支持流式"})
		return
	}
	from := 0
	if v := r.URL.Query().Get("from"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			from = n
		}
	}
	s.convMu.Lock()
	running := s.convCancel[convID] != nil
	s.convMu.Unlock()
	j := s.job(convID)
	if !running && j.length() <= from {
		writeJSON(w, 404, map[string]string{"error": "这一轮已经结束，或没有可续传的内容"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	idx := from
	for {
		frames, done := j.snapshot(idx)
		for _, f := range frames {
			if _, err := w.Write([]byte(f)); err != nil {
				return
			}
			flusher.Flush()
			idx++
		}
		if done && len(frames) == 0 {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
