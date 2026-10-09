package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"ws-demo/internal/api"
	"ws-demo/internal/app"
	"ws-demo/internal/auth"
	"ws-demo/internal/ws"
)

func main() {
	hub := ws.NewHub()
	go hub.Run()
	databasePath := os.Getenv("PUSHER_DB_PATH")
	if databasePath == "" {
		databasePath = "data/pusher.db"
	}
	apps, err := app.OpenRegistry(databasePath)
	if err != nil {
		log.Fatalf("Could not open app database: %v", err)
	}
	defer apps.Close()
	var grants api.GrantIssuer
	if signingKey := os.Getenv("CHANNEL_AUTH_SIGNING_KEY"); signingKey != "" {
		grantService, grantErr := auth.NewGrantService(signingKey, 30*time.Second)
		if grantErr != nil {
			log.Fatalf("Could not configure channel authorization: %v", grantErr)
		}
		grants = grantService
	}
	handler := api.NewHandler(hub, apps, grants, hub, hub)

	r := gin.Default()
	r.Static("/public", "./public")
	r.StaticFile("/", "./public/index.html")
	r.POST("/apps", handler.CreateApp)
	r.GET("/apps/:appID/channels", handler.ListActiveChannels)
	r.POST("/apps/:appID/events", handler.PublishEvent)
	r.POST("/apps/:appID/private-channel-auth", handler.AuthorizePrivateChannel)
	r.GET("/apps/:appID/ws", func(c *gin.Context) {
		appID := c.Param("appID")
		validKey, err := apps.HasKey(appID, c.Query("key"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not verify app key"})
			return
		}
		if !validKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid app key"})
			return
		}
		hub.ServeAppHTTP(appID, c.Writer, c.Request, func(socketID, channel, token string) (*ws.PresenceMember, error) {
			return handler.AuthorizeSubscription(appID, socketID, channel, token)
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Server running on http://localhost:%s\n", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
