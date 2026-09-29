package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Publisher interface {
	Publish(channel, data string)
}

type Handler struct {
	publisher Publisher
}

type eventPayload struct {
	Channel string `json:"channel" binding:"required"`
	Data    string `json:"data" binding:"required"`
}

func NewHandler(publisher Publisher) *Handler {
	return &Handler{publisher: publisher}
}

func (h *Handler) PublishEvent(c *gin.Context) {
	var event eventPayload
	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload format"})
		return
	}

	h.publisher.Publish(event.Channel, event.Data)
	c.JSON(http.StatusOK, gin.H{
		"status":  "published",
		"channel": event.Channel,
	})
}