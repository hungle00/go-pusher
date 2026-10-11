package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"ws-demo/internal/app"
	"ws-demo/internal/auth"
	"ws-demo/internal/ws"

	"github.com/gin-gonic/gin"
)

const (
	maxEventRequestBytes       = 1 << 20
	maxChannelAuthRequestBytes = 8 << 10
)

type Publisher interface {
	Publish(appID, channel, event string, data json.RawMessage)
}

type GrantIssuer interface {
	Issue(appID, socketID, channel string, channelData *auth.ChannelData) (string, error)
	Verify(token, appID, socketID, channel string) (*auth.GrantClaims, error)
}

type SocketChecker interface {
	SocketBelongs(appID, socketID string) bool
}

type ChannelStatsProvider interface {
	ActiveChannels(appID string) []ws.ChannelStats
}

type Handler struct {
	publisher Publisher
	apps      *app.Registry
	grants    GrantIssuer
	sockets   SocketChecker
	channels  ChannelStatsProvider
}

type eventPayload struct {
	Event   string          `json:"event"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

type createAppPayload struct {
	Name string `json:"name" binding:"required"`
}

func NewHandler(publisher Publisher, apps *app.Registry, grants GrantIssuer, sockets SocketChecker, channels ChannelStatsProvider) *Handler {
	return &Handler{publisher: publisher, apps: apps, grants: grants, sockets: sockets, channels: channels}
}

func (h *Handler) ListActiveChannels(c *gin.Context) {
	if h.channels == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "channel tracking is not configured"})
		return
	}
	appID := c.Param("appID")
	c.JSON(http.StatusOK, gin.H{
		"app_id":   appID,
		"channels": h.channels.ActiveChannels(appID),
	})
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
	SocketID    string            `json:"socket_id" binding:"required"`
	Channel     string            `json:"channel" binding:"required"`
	ChannelData *auth.ChannelData `json:"channel_data"`
}

func (h *Handler) AuthorizePrivateChannel(c *gin.Context) {
	if h.grants == nil || h.sockets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "private-channel authentication is not configured"})
		return
	}
	var request privateAuthPayload
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxChannelAuthRequestBytes)
	if err := c.ShouldBindJSON(&request); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Authorization request is too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid authorization request"})
		return
	}
	isPrivate := strings.HasPrefix(request.Channel, "private-")
	isPresence := strings.HasPrefix(request.Channel, "presence-")
	if !isPrivate && !isPresence {
		c.JSON(http.StatusBadRequest, gin.H{"error": "private-* or presence-* channel with matching channel_data is required"})
		return
	}
	if isPresence {
		if err := request.ChannelData.Validate(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		request.ChannelData.UserID = strings.TrimSpace(request.ChannelData.UserID)
	} else if request.ChannelData != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "private channels cannot include presence channel_data"})
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

	grant, err := h.grants.Issue(appID, request.SocketID, request.Channel, request.ChannelData)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response := gin.H{"auth": grant}
	if isPresence {
		response["channel_data"] = request.ChannelData
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) AuthorizeSubscription(appID, socketID, channel, token string) (*ws.PresenceMember, error) {
	if h.grants == nil {
		return nil, errors.New("private-channel authentication is not configured")
	}
	claims, err := h.grants.Verify(token, appID, socketID, channel)
	if err != nil {
		return nil, err
	}
	if claims.ChannelData == nil {
		return nil, nil
	}
	return &ws.PresenceMember{UserID: claims.ChannelData.UserID, UserInfo: claims.ChannelData.UserInfo}, nil
}
