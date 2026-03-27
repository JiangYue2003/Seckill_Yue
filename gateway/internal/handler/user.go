package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"seckill-mall/gateway/internal/middleware"
	"seckill-mall/user-service/user"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/logx"
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
	Username string `json:"username" binding:"required,min=3,max=50"`
	Password string `json:"password" binding:"required,min=6"`
	Email    string `json:"email" binding:"required,email"`
	Phone    string `json:"phone"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register 用户注册
func (h *UserHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	logx.Infof("用户注册请求: username=%s, email=%s", req.Username, req.Email)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.Register(ctx, &user.RegisterRequest{
		Username: req.Username,
		Password: req.Password,
		Email:    req.Email,
		Phone:    req.Phone,
	})
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "用户名") || strings.Contains(errMsg, "邮箱") ||
			strings.Contains(errMsg, "密码") || strings.Contains(errMsg, "长度") ||
			strings.Contains(errMsg, "不能为空") {
			middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "注册失败: "+errMsg)
		} else {
			logx.Errorf("用户注册失败: %v", err)
			middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "注册失败: "+errMsg)
		}
		return
	}

	middleware.Success(c, gin.H{
		"id":       resp.Id,
		"username": resp.Username,
		"email":    resp.Email,
		"phone":    resp.Phone,
	})
}

// Login 用户登录
func (h *UserHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	logx.Infof("用户登录请求: username=%s", req.Username)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.Login(ctx, &user.LoginRequest{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "用户名") || strings.Contains(errMsg, "密码") ||
			strings.Contains(errMsg, "账号") || strings.Contains(errMsg, "不存在") ||
			strings.Contains(errMsg, "禁用") {
			middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "用户名或密码错误")
		} else {
			logx.Errorf("用户登录失败: %v", err)
			middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "登录失败: "+errMsg)
		}
		return
	}

	middleware.Success(c, gin.H{
		"userId":   resp.UserId,
		"username": resp.Username,
		"email":    resp.Email,
		"token":    resp.Token,
		"expireAt": resp.ExpireAt,
	})
}

// GetUserInfo 获取用户信息
func (h *UserHandler) GetUserInfo(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	logx.Infof("获取用户信息: userId=%d", userId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.GetUserInfo(ctx, &user.GetUserInfoRequest{UserId: userId})
	if err != nil {
		logx.Errorf("获取用户信息失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "获取用户信息失败")
		return
	}

	middleware.Success(c, gin.H{
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
func (h *UserHandler) UpdateUserInfo(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req UpdateUserInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.UpdateUserInfo(ctx, &user.UpdateUserInfoRequest{
		UserId: userId,
		Email:  req.Email,
		Phone:  req.Phone,
	})
	if err != nil {
		logx.Errorf("更新用户信息失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "更新用户信息失败")
		return
	}

	if !resp.Success {
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, resp.Message)
		return
	}

	middleware.Success(c, gin.H{
		"success": true,
		"message": resp.Message,
	})
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required,min=6"`
}

// ChangePassword 修改密码
func (h *UserHandler) ChangePassword(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.userSvc.ChangePassword(ctx, &user.ChangePasswordRequest{
		UserId:      userId,
		OldPassword: req.OldPassword,
		NewPassword: req.NewPassword,
	})
	if err != nil {
		logx.Errorf("修改密码失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "修改密码失败")
		return
	}

	if !resp.Success {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(c, gin.H{
		"success": true,
		"message": resp.Message,
	})
}
