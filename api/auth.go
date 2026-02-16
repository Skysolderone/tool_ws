package api

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// AuthMiddleware Token 认证中间件
// 从请求 Header "X-Auth-Token" 或 Query "token" 中读取 token 并验证
func AuthMiddleware() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		configToken := Cfg.Auth.Token
		if configToken == "" {
			// 未配置 token，跳过认证
			ctx.Next(c)
			return
		}

		// 从 Header 获取
		token := string(ctx.GetHeader("X-Auth-Token"))
		if token == "" {
			// 从 Query 参数获取
			token = ctx.Query("token")
		}
		if token == "" {
			// 从 Authorization Bearer 获取
			auth := string(ctx.GetHeader("Authorization"))
			if len(auth) > 7 && auth[:7] == "Bearer " {
				token = auth[7:]
			}
		}

		if token != configToken {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, utils.H{
				"error": "unauthorized: invalid or missing token",
			})
			return
		}

		ctx.Next(c)
	}
}
