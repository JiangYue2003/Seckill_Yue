package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"seckill-mall/gateway/internal/middleware"
)

// HealthHandler 健康检查
func HealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, middleware.Response{
		Code:    0,
		Message: "ok",
	})
}
