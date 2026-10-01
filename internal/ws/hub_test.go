package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCommandRateLimiterResetsAfterWindow(t *testing.T) {
	start := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	limiter := commandRateLimiter{}

	for commandIndex := 0; commandIndex < maxCommandsPerWindow; commandIndex++ {
		if !limiter.allow(start) {
			t.Fatalf("command %d should be allowed", commandIndex+1)
		}
	}
	if limiter.allow(start.Add(commandRateWindow - time.Nanosecond)) {
		t.Fatal("command over the per-window limit should be rejected")
	}
	if !limiter.allow(start.Add(commandRateWindow)) {
		t.Fatal("commands should be allowed again in the next window")
	}
}

func TestWebSocketRejectsOversizedMessage(t *testing.T) {
	hub := NewHub()
	go hub.Run()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeAppHTTP("test-app", w, r, nil)
	}))
	t.Cleanup(server.Close)

	socketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(socketURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read connected message: %v", err)
	}
	err = conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", maxWebSocketMessageBytes+1)))
	if err == nil {
		_, _, err = conn.ReadMessage()
	}
	if err == nil {
		t.Fatal("expected oversized message to close the connection")
	}
	if closeError, ok := err.(*websocket.CloseError); ok && closeError.Code != websocket.CloseMessageTooBig {
		t.Fatalf("expected close code %d, got %d", websocket.CloseMessageTooBig, closeError.Code)
	}
}