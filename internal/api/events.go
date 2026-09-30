package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"ws-demo/internal/app"

	"github.com/gin-gonic/gin"
)

type Publisher interface {
	Publish(appID, channel, event string, data json.RawMessage)
}

type Handler struct {
	publisher Publisher
	apps      *app.Registry
}

type eventPayload struct {
	Event   string          `json:"event"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

type createAppPayload struct {
	Name string `json:"name" binding:"required"`
}

func NewHandler(publisher Publisher, apps *app.Registry) *Handler {
	return &Handler{publisher: publisher, apps: apps}
}

func (h *Handler) CreateApp(c *gin.Context) {
	var request createAppPayload
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "App name is required"})
		return
	}

	created, err := h.apps.FindOrCreateApp(request.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create app"})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) PublishEvent(c *gin.Context) {
	var event eventPayload
	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload format"})
		return
	}
	if event.Event == "" || event.Topic == "" || len(event.Payload) == 0 || !json.Valid(event.Payload) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Event, topic, and valid JSON payload are required"})
		return
	}

	appID := c.Param("appID")
	secret := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	validSecret, err := h.apps.HasSecret(appID, secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not verify app credentials"})
		return
	}
	if secret == "" || !validSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid app credentials"})
		return
	}

	h.publisher.Publish(appID, event.Topic, event.Event, event.Payload)
	c.JSON(http.StatusOK, gin.H{
		"status": "published",
		"topic":  event.Topic,
		"event":  event.Event,
	})
}
