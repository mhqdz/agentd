package llm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContextWindowTopLevel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"object":"list","data":[{"id":"deepseek-flash","context_window":1048576,"max_output_tokens":393216}]}`)
	}))
	defer srv.Close()
	if got := ContextWindow(srv.URL+"/v1", "k", "deepseek-flash"); got != 1048576 {
		t.Fatalf("窗口 = %d，期望 1048576", got)
	}
}

func TestContextWindowMetaNCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"m","meta":{"n_ctx":131072}}]}`)
	}))
	defer srv.Close()
	if got := ContextWindow(srv.URL+"/v1", "", "m"); got != 131072 {
		t.Fatalf("窗口 = %d", got)
	}
}
