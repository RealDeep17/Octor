package web

import (
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// ErrorLogger is a middleware that logs all errors added to gin.Context
// via c.AbortWithError or c.Error. This allows handlers to simply call
// c.AbortWithError with a wrapped error and skip manual logging.
func ErrorLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		for _, ginErr := range c.Errors {
			log.WithError(ginErr.Err).
				WithField("status", c.Writer.Status()).
				WithField("method", c.Request.Method).
				WithField("path", c.Request.URL.Path).
				Error("request failed")
		}
	}
}

// CORSPNAMiddleware handles CORS preflight and Private Network Access (PNA) preflight requests.
func CORSPNAMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE, PATCH")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, Access-Control-Request-Private-Network, x-access-token")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Access-Control-Allow-Private-Network")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle Private Network Access preflight request
		if c.Request.Header.Get("Access-Control-Request-Private-Network") == "true" {
			c.Writer.Header().Set("Access-Control-Allow-Private-Network", "true")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

