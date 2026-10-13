package main

import (
	"embed"
	"encoding/json"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// routes 注册全部 HTTP 路由
func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/events", a.handleEvents)
	mux.HandleFunc("/api/test", a.handleTest)
	mux.HandleFunc("/api/clear-proxy", a.handleClearProxy)
	return mux
}

// handleIndex 返回内嵌的界面页面
func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// handleStatus 返回应用状态
func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.info())
}

// handleEvents SSE 实时事件流
func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	a.hub.ServeSSE(w, r, func() interface{} { return a.info() })
}

// handleTest 启动测试
func (a *App) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req TestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, StartResult{Error: "请求格式错误"})
		return
	}
	writeJSON(w, a.StartTest(req))
}

// handleClearProxy 清除系统代理
func (a *App) handleClearProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, a.ClearProxy())
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
