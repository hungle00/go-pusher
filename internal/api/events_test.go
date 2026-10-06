package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"ws-demo/internal/ws"
)

type channelStatsStub struct {
	channels []ws.ChannelStats
}

func (stub channelStatsStub) ActiveChannels(string) []ws.ChannelStats {
	return stub.channels
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

func TestListActiveChannelsReturnsAppScopedSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	provider := channelStatsStub{channels: []ws.ChannelStats{{Name: "notifications", SubscriberCount: 2}}}
	router.GET("/apps/:appID/channels", NewHandler(nil, nil, nil, nil, provider).ListActiveChannels)

	request := httptest.NewRequest(http.MethodGet, "/apps/app-one/channels", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	var response struct {
		AppID    string            `json:"app_id"`
		Channels []ws.ChannelStats `json:"channels"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.AppID != "app-one" || len(response.Channels) != 1 || response.Channels[0].SubscriberCount != 2 {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestAuthorizePrivateChannelReturnsUnavailableWithoutGrantService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/apps/:appID/private-channel-auth", NewHandler(nil, nil, nil, nil, nil).AuthorizePrivateChannel)

	request := httptest.NewRequest(http.MethodPost, "/apps/test/private-channel-auth", strings.NewReader(`{"socket_id":"socket-1","channel":"private-orders"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, recorder.Code)
	}
}
