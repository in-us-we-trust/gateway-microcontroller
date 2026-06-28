package middleware

import (
	"gateway-microcontroller/internal/requestctx"
	"gateway-microcontroller/pkg/apierror"

	"github.com/gin-gonic/gin"
)

type DeviceIDExtractor func(*gin.Context) (string, error)

func Auth(extractor DeviceIDExtractor) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID, err := extractor(c)

		if err != nil {
			apierror.Respond(c, apierror.Unauthorized(
				err.Error(),
				c.Request.URL.Path,
			))
			c.Abort()
			return
		}

		ctx := requestctx.WithDeviceID(c.Request.Context(), deviceID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
