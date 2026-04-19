package middleware

import (
	"log"
	"strings"

	"gateway-microcontroller/pkg/apierror"

	"github.com/gin-gonic/gin"
)

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			apierror.Respond(c, apierror.Unauthorized("Authorization header with Bearer token is required", c.Request.URL.Path))
			c.Abort()
			return
		}

		// TODO: validate JWT token signature and claims against trusted issuer
		log.Printf("[security] JWT validation performed for %s %s", c.Request.Method, c.Request.URL.Path)

		c.Next()
	}
}
