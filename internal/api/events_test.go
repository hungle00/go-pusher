package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPublishEventRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/apps/:appID/events", NewHandler(nil, nil, nil, nil).PublishEvent)

	requestBody := `{"event":"message","topic":"bench","payload":"` + strings.Repeat("x", maxEventRequestBytes) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/apps/test/events", strings.NewReader(requestBody))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, recorder.Code)
	}
}