package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// sseEvent 一条推送给浏览器的事件
type sseEvent struct {
	Type string
	Data interface{}
}

// Hub 管理所有 SSE 客户端连接与历史日志（页面重连时回放）
type Hub struct {
	mu         sync.Mutex
	clients    map[chan sseEvent]struct{}
	history    []sseEvent
	maxHistory int
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[chan sseEvent]struct{}),
		maxHistory: 200,
	}
}

func (h *Hub) add(ch chan sseEvent) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(ch chan sseEvent) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

// Emit 广播事件；log 类型进入历史缓冲
func (h *Hub) Emit(evType string, data interface{}) {
	ev := sseEvent{Type: evType, Data: data}

	h.mu.Lock()
	if evType == "log" {
		h.history = append(h.history, ev)
		if len(h.history) > h.maxHistory {
			h.history = h.history[len(h.history)-h.maxHistory:]
		}
	}
	clients := make([]chan sseEvent, 0, len(h.clients))
	for ch := range h.clients {
		clients = append(clients, ch)
	}
	h.mu.Unlock()

	for _, ch := range clients {
		select {
		case ch <- ev:
		default: // 客户端积压时丢弃该条，避免阻塞
		}
	}
}

func (h *Hub) historySnapshot() []sseEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]sseEvent, len(h.history))
	copy(out, h.history)
	return out
}

// ServeSSE 处理 /api/events 的 SSE 长连接
func (h *Hub) ServeSSE(w http.ResponseWriter, r *http.Request, status func() interface{}) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	ch := make(chan sseEvent, 512)
	h.add(ch)
	defer h.remove(ch)

	// 回放历史日志 + 推送当前状态
	for _, ev := range h.historySnapshot() {
		writeSSE(w, ev)
	}
	writeSSE(w, sseEvent{Type: "status", Data: status()})
	fl.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case ev := <-ch:
			writeSSE(w, ev)
			fl.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSE(w http.ResponseWriter, ev sseEvent) {
	b, err := json.Marshal(ev.Data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b)
}
