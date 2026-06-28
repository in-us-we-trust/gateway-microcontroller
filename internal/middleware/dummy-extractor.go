package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

func ExtractDeviceIDFromHeader(c *gin.Context) (string, error) {
	deviceID := strings.TrimSpace(c.GetHeader("X-Device-ID"))
	if deviceID == "" {
		return "", errors.New("X-Device-ID header is required")
	}

	return deviceID, nil
}
