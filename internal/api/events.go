package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"ws-demo/internal/app"

	"github.com/gin-gonic/gin"
)

const maxEventRequestBytes = 1 << 20

type Publisher interface {
	Publish(appID, channel, event string, data json.RawMessage)
}

type GrantIssuer interface {
	Issue(appID, socketID, channel string) (string, error)
	Verify(token, appID, socketID, channel string) error
}

type SocketChecker interface {
	SocketBelongs(appID, socketID string) bool
}

type Handler struct {
	publisher Publisher
	apps      *app.Registry
	grants    GrantIssuer
	sockets   SocketChecker
}

type eventPayload struct {
	Event   string          `json:"event"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

type createAppPayload struct {
	Name string `json:"name" binding:"required"`
}

func NewHandler(publisher Publisher, apps *app.Registry, grants GrantIssuer, sockets SocketChecker) *Handler {
	return &Handler{publisher: publisher, apps: apps, grants: grants, sockets: sockets}
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxEventRequestBytes)
	var event eventPayload
	if err := c.ShouldBindJSON(&event); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Request body is too large"})
			return
		}
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

type privateAuthPayload struct {
	SocketID string `json:"socket_id" binding:"required"`
	Channel  string `json:"channel" binding:"required"`
}

func (h *Handler) AuthorizePrivateChannel(c *gin.Context) {
	if h.grants == nil || h.sockets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "private-channel authentication is not configured"})
		return
	}
	var request privateAuthPayload
	if err := c.ShouldBindJSON(&request); err != nil || !strings.HasPrefix(request.Channel, "private-") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "socket_id and a private-* channel are required"})
		return
	}

	appID := c.Param("appID")
	secret := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	validSecret, err := h.apps.HasSecret(appID, secret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not verify app credentials"})
		return
	}
	if secret == "" || !validSecret {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid app credentials"})
		return
	}
	if !h.sockets.SocketBelongs(appID, request.SocketID) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "socket is not connected"})
		return
	}

	grant, err := h.grants.Issue(appID, request.SocketID, request.Channel)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create channel authorization"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"auth": grant})
}

func (h *Handler) AuthorizeSubscription(appID, socketID, channel, token string) error {
	if h.grants == nil {
		return errors.New("private-channel authentication is not configured")
	}
	return h.grants.Verify(token, appID, socketID, channel)
}
