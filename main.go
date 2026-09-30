package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"ws-demo/internal/api"
	"ws-demo/internal/app"
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
	handler := api.NewHandler(hub, apps)

	r := gin.Default()
	r.Static("/public", "./public")
	r.StaticFile("/", "./public/index.html")
	r.POST("/apps", handler.CreateApp)
	r.POST("/apps/:appID/events", handler.PublishEvent)
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
		hub.ServeAppHTTP(appID, c.Writer, c.Request)
	})

	fmt.Println("Server running on http://localhost:8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
