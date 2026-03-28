package logic

import (
	"context"
	"errors"
	"fmt"

	"seckill-mall/user-service/internal/svc"
	"seckill-mall/user-service/user"

	"github.com/zeromicro/go-zero/core/logx"
)

type RefreshTokenLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRefreshTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RefreshTokenLogic {
	return &RefreshTokenLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RefreshToken 用 Refresh Token 换取新的 Access Token + Refresh Token（rotation）
func (l *RefreshTokenLogic) RefreshToken(in *user.RefreshTokenRequest) (*user.RefreshTokenResponse, error) {
	if in.RefreshToken == "" {
		return nil, errors.New("refresh token 不能为空")
	}

	// 从 refresh token 中提取 userId（格式：uuid.userId）
	parts := splitRefreshToken(in.RefreshToken)
	if len(parts) != 2 {
		return nil, errors.New("refresh token 格式错误")
	}

	userId, err := parseUserIdFromRefreshToken(parts[1])
	if err != nil {
		return nil, errors.New("refresh token 格式错误")
	}

	// 验证 Redis 中该 refresh token 仍然有效（未被动过）
	if err := ValidateRefreshToken(l.svcCtx.Redis, userId, in.RefreshToken); err != nil {
		l.Logger.Infof("RefreshToken 验证失败: userId=%d, err=%v", userId, err)
		return nil, errors.New("refresh token 已失效，请重新登录")
	}

	// 作废旧 refresh token（rotation，防止 replay attack）
	if err := RevokeRefreshToken(l.svcCtx.Redis, userId, in.RefreshToken); err != nil {
		l.Logger.Errorf("RevokeRefreshToken 失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 签发新的 access token
	newAccessToken, newAccessExp, err := generateAccessToken(l.svcCtx.Config.JWTConfig.AccessSecret, userId)
	if err != nil {
		l.Logger.Errorf("生成新AccessToken失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 签发新的 refresh token（rotation）
	newRefreshToken, newRefreshExp, err := generateRefreshToken(userId, l.svcCtx.Config.JWTConfig.RefreshExpire)
	if err != nil {
		l.Logger.Errorf("生成新RefreshToken失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 新 refresh token 入 Redis
	newParts := splitRefreshToken(newRefreshToken)
	if len(newParts) != 2 {
		return nil, errors.New("系统错误，请稍后重试")
	}
	refreshKey := fmt.Sprintf("user:refresh:%d:%s", userId, newParts[0])
	l.svcCtx.Redis.Setex(refreshKey, "1", int(l.svcCtx.Config.JWTConfig.RefreshExpire))

	l.Logger.Infof("RefreshToken 成功: userId=%d", userId)

	return &user.RefreshTokenResponse{
		AccessToken:     newAccessToken,
		AccessExpireAt:  newAccessExp,
		RefreshToken:    newRefreshToken,
		RefreshExpireAt: newRefreshExp,
	}, nil
}

func parseUserIdFromRefreshToken(s string) (int64, error) {
	var userId int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errors.New("invalid userId")
		}
		userId = userId*10 + int64(s[i]-'0')
	}
	return userId, nil
}
