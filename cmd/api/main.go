package main

import (
	_ "gateway-microcontroller/docs"
	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/config"
	"gateway-microcontroller/internal/handler"
	"gateway-microcontroller/internal/middleware"
	"gateway-microcontroller/internal/service"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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

	detectorClient := client.NewHTTPDetectorClient(cfg.DetectiveManagerURL)
	sessionService := service.NewSessionService(cfg.SessionTTL)
	mediaExtractor := service.NewFFmpegMediaExtractor(cfg.FFmpegPath)
	eventService := service.NewGatewayEventService(
		detectorClient,
		sessionService,
		mediaExtractor,
		service.EventServiceConfig{
			AudioChunkDuration:  cfg.AudioChunkDuration,
			VideoChunkDuration:  cfg.VideoChunkDuration,
			AudioFormat:         cfg.AudioFormat,
			MediaRequestTimeout: cfg.MediaRequestTimeout,
			MediaChunkBatchSize: cfg.MediaChunkBatchSize,
			RTSPBase:            cfg.RTSPBase,
			DebugMedia: service.NewDebugMediaWriter(
				cfg.DebugMediaEnabled,
				cfg.DebugMediaDir,
				cfg.DebugMediaMaxFiles,
			),
		},
	)
	defer eventService.Close()

	eventHandler := handler.NewEventHandler(eventService)

	deviceService := service.NewDeviceService(detectorClient)
	deviceHandler := handler.NewDeviceHandler(deviceService)

	var mqttClient mqtt.Client
	if cfg.MQTTEnabled {
		options := mqtt.NewClientOptions().
			AddBroker(cfg.MQTTBrokerURL).
			SetClientID(cfg.MQTTClientID).
			SetAutoReconnect(true).
			SetConnectRetry(true).
			SetConnectRetryInterval(5 * time.Second).
			SetOnConnectHandler(func(connectedClient mqtt.Client) {
				if err := eventHandler.RegisterMQTTSubscriptions(connectedClient); err != nil {
					log.Printf("failed to register MQTT subscriptions: %v", err)
				}
			}).
			SetConnectionLostHandler(func(_ mqtt.Client, err error) {
				log.Printf("MQTT connection lost: %v", err)
			})
		if cfg.MQTTUsername != "" {
			options.SetUsername(cfg.MQTTUsername)
			options.SetPassword(cfg.MQTTPassword)
		}

		mqttClient = mqtt.NewClient(options)
		connectToken := mqttClient.Connect()
		if !connectToken.WaitTimeout(30 * time.Second) {
			log.Fatal("timed out while connecting to MQTT broker")
		}
		if err := connectToken.Error(); err != nil {
			log.Fatalf("failed to connect to MQTT broker: %v", err)
		}
		eventService.SetDeviceCommander(client.NewMQTTInstructionPublisher(mqttClient))
		defer mqttClient.Disconnect(250)
	}

	router := gin.Default()

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	api := router.Group("/api/v1")

	authMiddleware := middleware.Auth(middleware.ExtractDeviceIDFromHeader)

	if cfg.EnableMTLS {
		authMiddleware = middleware.Auth(middleware.ExtractDeviceIDFromMTLS)
		log.Println("mTLS is enabled. Device ID will be extracted from the client certificate.")
	}

	api.Use(authMiddleware)
	{
		api.POST("/events", eventHandler.PostEvent)
		api.POST("/devices", deviceHandler.PostDevice)
	}

	log.Printf("starting server on :%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
