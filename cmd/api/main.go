package main

import (
	"crypto/tls"
	"crypto/x509"
	_ "gateway-microcontroller/docs"
	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/config"
	"gateway-microcontroller/internal/handler"
	"gateway-microcontroller/internal/middleware"
	"gateway-microcontroller/internal/service"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title           Secure Home Gateway API
// @version         1.0
// @description     IoT gateway for the Secure Home project — routes microcontroller events to the detection service. All endpoints require mutual TLS (mTLS) client certificate authentication.
// @host            localhost:8080
// @BasePath        /
func main() {
	cfg := config.Load()
	authClient := client.NewHTTPAuthClient(cfg.AuthServiceURL)

	deviceAuthService := service.NewDeviceAuthService(
		authClient,
	)

	mtlsMiddleware := middleware.NewMTLSMiddleware(
		deviceAuthService,
	)
	detectorClient := client.NewHTTPDetectorClient(cfg.DetectorServiceURL)
	eventService := service.NewEventService(detectorClient)
	eventHandler := handler.NewEventHandler(eventService)

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
	api.Use(mtlsMiddleware.Handle())
	{
		api.POST("/events", eventHandler.PostEvent)
	}

	log.Printf("starting server on :%s", cfg.Port)
	if cfg.TLS.ServerCertPath != "" && cfg.TLS.ServerKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.ServerCertPath, cfg.TLS.ServerKeyPath)
		if err != nil {
			log.Fatalf("failed to load server certificate: %v", err)
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		}

		if cfg.TLS.ClientCAPath != "" {
			caPEM, err := os.ReadFile(cfg.TLS.ClientCAPath)
			if err != nil {
				log.Fatalf("failed to read client CA file: %v", err)
			}

			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caPEM) {
				log.Fatalf("failed to append client CA certificates")
			}
			tlsConfig.ClientCAs = pool
		}

		srv := &http.Server{
			Addr:      ":" + cfg.Port,
			Handler:   router,
			TLSConfig: tlsConfig,
		}

		log.Printf("starting HTTPS server with mTLS on :%s", cfg.Port)
		if err := srv.ListenAndServeTLS("", ""); err != nil {
			log.Fatalf("server error: %v", err)
		}
		return
	}

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
