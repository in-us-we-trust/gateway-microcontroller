// internal/middleware/mtls.go

package middleware

import (
	"net/http"

	"gateway-microcontroller/internal/requestctx"
	"gateway-microcontroller/internal/service"

	"github.com/gin-gonic/gin"
)

type MTLSMiddleware struct {
	deviceAuthService *service.DeviceAuthService
}

func NewMTLSMiddleware(
	deviceAuthService *service.DeviceAuthService,
) *MTLSMiddleware {
	return &MTLSMiddleware{
		deviceAuthService: deviceAuthService,
	}
}

func (m *MTLSMiddleware) Handle() gin.HandlerFunc {
	return func(c *gin.Context) {

		if c.Request.TLS == nil {
			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{"error": "tls required"},
			)
			return
		}

		if len(c.Request.TLS.PeerCertificates) == 0 {
			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{"error": "client certificate required"},
			)
			return
		}

		cert := c.Request.TLS.PeerCertificates[0]

		device, err := m.deviceAuthService.ValidateCertificate(
			c.Request.Context(),
			cert,
		)
		if err != nil {
			c.AbortWithStatusJSON(
				http.StatusUnauthorized,
				gin.H{"error": err.Error()},
			)
			return
		}

		ctx := requestctx.WithDeviceContext(
			c.Request.Context(),
			requestctx.DeviceContext{
				DeviceID: device.DeviceID,
				Serial:   device.Serial,
			},
		)

		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
