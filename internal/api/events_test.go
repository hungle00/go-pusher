package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"ws-demo/internal/app"
	"ws-demo/internal/auth"
)

type connectedSocketStub bool

func (stub connectedSocketStub) SocketBelongs(string, string) bool {
	return bool(stub)
}

func TestPublishEventRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/apps/:appID/events", NewHandler(nil, nil, nil, nil, nil).PublishEvent)

	requestBody := `{"event":"message","topic":"bench","payload":"` + strings.Repeat("x", maxEventRequestBytes) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/apps/test/events", strings.NewReader(requestBody))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, recorder.Code)
	}
}

func TestAuthorizePresenceChannelReturnsGrantAndChannelData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry, err := app.OpenRegistry(t.TempDir() + "/apps.db")
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	created, err := registry.Create("presence-test")
	if err != nil {
		t.Fatal(err)
	}
	grants, err := auth.NewGrantService("test-signing-key", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(nil, registry, grants, connectedSocketStub(true), nil)
	router := gin.New()
	router.POST("/apps/:appID/private-channel-auth", handler.AuthorizePrivateChannel)

	requestBody := `{"socket_id":"socket-1","channel":"presence-room","channel_data":{"user_id":"user-7","user_info":{"name":"Ada"}}}`
	request := httptest.NewRequest(http.MethodPost, "/apps/"+created.ID+"/private-channel-auth", strings.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+created.Secret)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var response struct {
		Auth        string           `json:"auth"`
		ChannelData auth.ChannelData `json:"channel_data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	claims, err := grants.Verify(response.Auth, created.ID, "socket-1", "presence-room")
	if err != nil {
		t.Fatalf("verify returned grant: %v", err)
	}
	if claims.ChannelData == nil || claims.ChannelData.UserID != "user-7" || response.ChannelData.UserID != "user-7" {
		t.Fatalf("presence identity missing from response or signed grant: %#v", response)
	}
}
