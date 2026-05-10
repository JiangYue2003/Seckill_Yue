package handler

import (
	"context"
	"strings"
	"time"

	"seckill-mall/common/user"
	"seckill-mall/gateway-hertz/internal/middleware"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// UserHandler 用户相关接口
type UserHandler struct {
	userSvc user.UserServiceClient
}

func NewUserHandler(svc user.UserServiceClient) *UserHandler {
	return &UserHandler{userSvc: svc}
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Register 用户注册
func (h *UserHandler) Register(ctx context.Context, c *app.RequestContext) {
	var req RegisterRequest
	if err := c.BindJSON(&req); err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}
	if req.Username == "" || req.Password == "" || req.Email == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "用户名、密码、邮箱不能为空")
		return
	}

	hlog.CtxInfof(ctx, "用户注册请求: username=%s, email=%s", req.Username, req.Email)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.Register(rpcCtx, &user.RegisterRequest{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
		Phone:    req.Phone,
	})
	if err != nil {
		if isBreakerError(err) {
			middleware.ErrorWithStatus(ctx, c, consts.StatusServiceUnavailable, 503, "系统繁忙，请稍后重试")
			return
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "用户名") || strings.Contains(errMsg, "邮箱") ||
			strings.Contains(errMsg, "密码") || strings.Contains(errMsg, "长度") ||
			strings.Contains(errMsg, "不能为空") {
			middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "注册失败: "+errMsg)
		} else {
			hlog.CtxErrorf(ctx, "用户注册失败: %v", err)
			middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "注册失败")
		}
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"id":       resp.Id,
		"username": resp.Username,
		"email":    resp.Email,
		"phone":    resp.Phone,
	})
}

// Login 用户登录
func (h *UserHandler) Login(ctx context.Context, c *app.RequestContext) {
	var req LoginRequest
	if err := c.BindJSON(&req); err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}
	if req.Username == "" || req.Password == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "用户名和密码不能为空")
		return
	}

	hlog.CtxInfof(ctx, "用户登录请求: username=%s", req.Username)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.Login(rpcCtx, &user.LoginRequest{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		if isBreakerError(err) {
			middleware.ErrorWithStatus(ctx, c, consts.StatusServiceUnavailable, 503, "系统繁忙，请稍后重试")
			return
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "用户名") || strings.Contains(errMsg, "密码") ||
			strings.Contains(errMsg, "账号") || strings.Contains(errMsg, "不存在") ||
			strings.Contains(errMsg, "禁用") {
			middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "用户名或密码错误")
		} else {
			hlog.CtxErrorf(ctx, "用户登录失败: %v", err)
			middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "登录失败")
		}
		return
	}

	hlog.CtxInfof(ctx, "用户登录成功: userId=%d", resp.UserId)
	middleware.Success(ctx, c, map[string]interface{}{
		"userId":          resp.UserId,
		"username":        resp.Username,
		"email":           resp.Email,
		"accessToken":     resp.AccessToken,
		"accessExpireAt":  resp.AccessExpireAt,
		"refreshToken":    resp.RefreshToken,
		"refreshExpireAt": resp.RefreshExpireAt,
	})
}

// RefreshTokenRequest 刷新 Token 请求
type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// RefreshToken 刷新 Token
func (h *UserHandler) RefreshToken(ctx context.Context, c *app.RequestContext) {
	var req RefreshTokenRequest
	if err := c.BindJSON(&req); err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}
	if req.RefreshToken == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "refreshToken 不能为空")
		return
	}

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.RefreshToken(rpcCtx, &user.RefreshTokenRequest{
		RefreshToken: req.RefreshToken,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "刷新Token失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "refresh token 无效或已过期，请重新登录")
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"accessToken":     resp.AccessToken,
		"accessExpireAt":  resp.AccessExpireAt,
		"refreshToken":    resp.RefreshToken,
		"refreshExpireAt": resp.RefreshExpireAt,
	})
}

// GetUserInfo 获取用户信息
func (h *UserHandler) GetUserInfo(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	hlog.CtxInfof(ctx, "获取用户信息: userId=%d", userId)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.GetUserInfo(rpcCtx, &user.GetUserInfoRequest{UserId: userId})
	if err != nil {
		handleRPCError(ctx, c, err, "获取用户信息")
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"id":        resp.Id,
		"username":  resp.Username,
		"email":     resp.Email,
		"phone":     resp.Phone,
		"status":    resp.Status,
		"createdAt": resp.CreatedAt,
		"updatedAt": resp.UpdatedAt,
	})
}

// UpdateUserInfoRequest 更新用户信息请求
type UpdateUserInfoRequest struct {
	Email string `json:"email"`
	Phone string `json:"phone"`
}

// UpdateUserInfo 更新用户信息
func (h *UserHandler) UpdateUserInfo(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req UpdateUserInfoRequest
	if err := c.BindJSON(&req); err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.UpdateUserInfo(rpcCtx, &user.UpdateUserInfoRequest{
		UserId: userId,
		Email:  req.Email,
		Phone:  req.Phone,
	})
	if err != nil {
		handleRPCError(ctx, c, err, "更新用户信息")
		return
	}
	if !resp.Success {
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, resp.Message)
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{"success": true, "message": resp.Message})
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// ChangePassword 修改密码
func (h *UserHandler) ChangePassword(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req ChangePasswordRequest
	if err := c.BindJSON(&req); err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "密码不能为空")
		return
	}

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.ChangePassword(rpcCtx, &user.ChangePasswordRequest{
		UserId:      userId,
		OldPassword: req.OldPassword,
		NewPassword: req.NewPassword,
	})
	if err != nil {
		handleRPCError(ctx, c, err, "修改密码")
		return
	}
	if !resp.Success {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{"success": true, "message": resp.Message})
}
