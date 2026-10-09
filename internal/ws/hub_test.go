package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestPresenceSubscriptionEventsUseUniqueUsers(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	firstAlice := &client{appID: "app-one", socketID: "alice-1", send: make(chan broadcastMessage, 8)}
	bob := &client{appID: "app-one", socketID: "bob-1", send: make(chan broadcastMessage, 8)}
	secondAlice := &client{appID: "app-one", socketID: "alice-2", send: make(chan broadcastMessage, 8)}
	for _, connected := range []*client{firstAlice, bob, secondAlice} {
		hub.register <- connected
	}

	hub.subscribe <- subscription{client: firstAlice, appID: "app-one", channel: "presence-room", member: &PresenceMember{UserID: "alice", UserInfo: json.RawMessage(`{"name":"Alice"}`)}}
	firstAck := <-firstAlice.send
	if firstAck.Event != "subscription_succeeded" || !strings.Contains(string(firstAck.Data), `"count":1`) {
		t.Fatalf("unexpected first presence acknowledgment: %#v", firstAck)
	}

	hub.subscribe <- subscription{client: bob, appID: "app-one", channel: "presence-room", member: &PresenceMember{UserID: "bob", UserInfo: json.RawMessage(`{"name":"Bob"}`)}}
	if got := (<-bob.send).Event; got != "subscription_succeeded" {
		t.Fatalf("new member got event %q, want subscription_succeeded", got)
	}
	if got := (<-firstAlice.send).Event; got != "member_added" {
		t.Fatalf("existing member got event %q, want member_added", got)
	}

	hub.subscribe <- subscription{client: secondAlice, appID: "app-one", channel: "presence-room", member: &PresenceMember{UserID: "alice", UserInfo: json.RawMessage(`{"name":"Alice"}`)}}
	secondAck := <-secondAlice.send
	if secondAck.Event != "subscription_succeeded" || !strings.Contains(string(secondAck.Data), `"count":2`) {
		t.Fatalf("second socket should see two unique users: %#v", secondAck)
	}
	select {
	case message := <-bob.send:
		t.Fatalf("duplicate socket caused an extra presence event: %#v", message)
	default:
	}

	hub.unregister <- firstAlice
	select {
	case message := <-bob.send:
		t.Fatalf("member_removed sent before the user's final socket left: %#v", message)
	default:
	}
	hub.unregister <- secondAlice
	if got := (<-bob.send).Event; got != "member_removed" {
		t.Fatalf("last socket departure sent %q, want member_removed", got)
	}
}

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

func TestActiveChannelsAreAppScopedAndCountUniqueClients(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	first := &client{appID: "app-one", socketID: "socket-one", send: make(chan broadcastMessage, 8)}
	second := &client{appID: "app-one", socketID: "socket-two", send: make(chan broadcastMessage, 8)}
	otherApp := &client{appID: "app-two", socketID: "socket-three", send: make(chan broadcastMessage, 8)}
	for _, connected := range []*client{first, second, otherApp} {
		hub.register <- connected
	}

	hub.subscribe <- subscription{client: first, appID: "app-one", channel: "notifications"}
	hub.subscribe <- subscription{client: first, appID: "app-one", channel: "notifications"}
	hub.subscribe <- subscription{client: second, appID: "app-one", channel: "notifications"}
	hub.subscribe <- subscription{client: second, appID: "app-one", channel: "private-orders"}
	hub.subscribe <- subscription{client: otherApp, appID: "app-two", channel: "notifications"}

	got := hub.ActiveChannels("app-one")
	want := []ChannelStats{
		{Name: "notifications", SubscriberCount: 2},
		{Name: "private-orders", SubscriberCount: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActiveChannels(app-one) = %#v, want %#v", got, want)
	}

	hub.unregister <- second
	got = hub.ActiveChannels("app-one")
	want = []ChannelStats{{Name: "notifications", SubscriberCount: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ActiveChannels after disconnect = %#v, want %#v", got, want)
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
