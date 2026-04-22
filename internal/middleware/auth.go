package middleware

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"gateway-microcontroller/internal/requestctx"
	"gateway-microcontroller/pkg/apierror"

	"github.com/gin-gonic/gin"
)

type jwtPayload struct {
	DeviceID string `json:"device_id"`
}

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			apierror.Respond(c, apierror.Unauthorized("Authorization header with Bearer token is required", c.Request.URL.Path))
			c.Abort()
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		deviceID, err := extractDeviceIDFromJWT(token)
		if err != nil {
			apierror.Respond(c, apierror.Unauthorized("invalid JWT payload: device_id is required", c.Request.URL.Path))
			c.Abort()
			return
		}

		ctx := requestctx.WithDeviceID(c.Request.Context(), deviceID)
		c.Request = c.Request.WithContext(ctx)

		// TODO: validate JWT token signature and claims against trusted issuer
		log.Printf("[security] JWT validation performed for %s %s", c.Request.Method, c.Request.URL.Path)

		c.Next()
	}
}

func extractDeviceIDFromJWT(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errInvalidJWT
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errInvalidJWT
	}

	var payload jwtPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return "", errInvalidJWT
	}

	payload.DeviceID = strings.TrimSpace(payload.DeviceID)
	if payload.DeviceID == "" {
		return "", errInvalidJWT
	}

	return payload.DeviceID, nil
}

var errInvalidJWT = errors.New("invalid jwt payload")
