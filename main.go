package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"

	"ws-demo/internal/api"
	"ws-demo/internal/ws"
)

func main() {
	hub := ws.NewHub()
	go hub.Run()

	r := gin.Default()
	r.Static("/public", "./public")
	r.StaticFile("/", "./public/index.html")
	r.GET("/ws", gin.WrapH(hub))
	r.POST("/events", api.NewHandler(hub).PublishEvent)

	fmt.Println("Server running on http://localhost:8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}