package testutil

import (
	"gateway-microcontroller/internal/requestctx"

	"github.com/gin-gonic/gin"
)

func FakeMTLS(deviceID, serial string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := requestctx.WithDeviceContext(c.Request.Context(), requestctx.DeviceContext{
			DeviceID: deviceID,
			Serial:   serial,
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
