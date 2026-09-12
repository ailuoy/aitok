package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthWithoutDatabase(t *testing.T) {
	// 部署探针仅验证服务存活，未连接数据库时也不触发访问。
	s := &Server{}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest(method, "/healthz", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s /healthz: %d", method, w.Code)
		}
	}
}
