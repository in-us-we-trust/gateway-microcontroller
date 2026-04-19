package main

import (
	"log"

	_ "gateway-microcontroller/docs"
	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/config"
	"gateway-microcontroller/internal/handler"
	"gateway-microcontroller/internal/middleware"
	"gateway-microcontroller/internal/service"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title           Secure Home Gateway API
// @version         1.0
// @description     IoT gateway for the Secure Home project — routes microcontroller events to the detection service.
// @host            localhost:8080
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
func main() {
	cfg := config.Load()

	detectorClient := client.NewHTTPDetectorClient(cfg.DetectorServiceURL)
	eventService := service.NewEventService(detectorClient)
	eventHandler := handler.NewEventHandler(eventService)

	router := gin.Default()

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := router.Group("/api/v1")
	api.Use(middleware.JWTAuth())
	{
		api.POST("/events", eventHandler.PostEvent)
	}

	log.Printf("starting server on :%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
