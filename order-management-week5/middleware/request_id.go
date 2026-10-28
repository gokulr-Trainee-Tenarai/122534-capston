package middleware

import (
	"order-management/observability"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		c.Set("request_id", id)
		c.Request = c.Request.WithContext(observability.WithRequestID(c.Request.Context(), id))
		c.Header("X-Request-ID", id)
		c.Next()
	}
}
