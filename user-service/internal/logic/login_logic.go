package logic

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"seckill-mall/common/user"
	"seckill-mall/user-service/internal/model"
	"seckill-mall/user-service/internal/svc"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"golang.org/x/crypto/bcrypt"
)

type LoginLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Login 用户登录，签发 Access Token + Refresh Token
func (l *LoginLogic) Login(in *user.LoginRequest) (*user.LoginResponse, error) {
	if in.Username == "" || in.Password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}

	existingUser, err := l.svcCtx.UserModel.FindOneByUsername(l.ctx, in.Username)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("用户不存在")
		}
		l.Logger.Errorf("查询用户失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	if existingUser.Status != 1 {
		return nil, errors.New("账号已被禁用")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(existingUser.Password), []byte(in.Password)); err != nil {
		l.Logger.Infof("密码错误: username=%s", in.Username)
		return nil, errors.New("密码错误")
	}

	accessToken, accessExp, err := generateAccessToken(l.svcCtx.Config.JWTConfig.AccessSecret, existingUser.ID)
	if err != nil {
		l.Logger.Errorf("生成AccessToken失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	refreshToken, refreshExp, err := generateRefreshToken(existingUser.ID, l.svcCtx.Config.JWTConfig.RefreshExpire)
	if err != nil {
		l.Logger.Errorf("生成RefreshToken失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	parts := splitRefreshToken(refreshToken)
	if len(parts) != 2 {
		return nil, errors.New("系统错误，请稍后重试")
	}
	refreshKey := fmt.Sprintf("user:refresh:%d:%s", existingUser.ID, parts[0])
	l.svcCtx.Redis.Setex(refreshKey, "1", int(l.svcCtx.Config.JWTConfig.RefreshExpire))

	l.Logger.Infof("用户登录成功: userId=%d, username=%s", existingUser.ID, existingUser.Username)

	return &user.LoginResponse{
		UserId:          existingUser.ID,
		Username:        existingUser.Username,
		Email:           existingUser.Email,
		AccessToken:     accessToken,
		AccessExpireAt:  accessExp,
		RefreshToken:    refreshToken,
		RefreshExpireAt: refreshExp,
	}, nil
}

// generateAccessToken 生成 Access Token（HMAC-SHA256 签名，短有效期）
func generateAccessToken(secret string, userId int64) (string, int64, error) {
	now := time.Now().Unix()
	expireAt := now + 900 // 15 分钟

	payload := map[string]interface{}{
		"userId": userId,
		"iat":    now,
		"exp":    expireAt,
		"jti":    uuid.New().String(),
	}
	return signPayload(payload, expireAt, secret)
}

// generateRefreshToken 生成 Refresh Token（UUID.userId 格式，存 Redis）
func generateRefreshToken(userId int64, refreshExpire int64) (string, int64, error) {
	token := fmt.Sprintf("%s.%d", uuid.New().String(), userId)
	expireAt := time.Now().Unix() + refreshExpire
	return token, expireAt, nil
}

// ValidateRefreshToken 验证 Refresh Token 是否在 Redis 中有效
func ValidateRefreshToken(redisClient *redis.Redis, userId int64, refreshToken string) error {
	parts := splitRefreshToken(refreshToken)
	if len(parts) != 2 {
		return errors.New("invalid refresh token format")
	}
	refreshKey := fmt.Sprintf("user:refresh:%d:%s", userId, parts[0])
	exists, err := redisClient.Exists(refreshKey)
	if err != nil {
		return fmt.Errorf("redis error: %w", err)
	}
	if !exists {
		return errors.New("refresh token 已失效或已被吊销")
	}
	return nil
}

// RevokeRefreshToken 删除 Redis 中的 Refresh Token（登出/强制注销）
func RevokeRefreshToken(redisClient *redis.Redis, userId int64, refreshToken string) error {
	parts := splitRefreshToken(refreshToken)
	if len(parts) != 2 {
		return errors.New("invalid refresh token format")
	}
	refreshKey := fmt.Sprintf("user:refresh:%d:%s", userId, parts[0])
	_, err := redisClient.Del(refreshKey)
	return err
}

// splitRefreshToken 拆分 refresh token: "uuid.userId" → [uuid, userId]
func splitRefreshToken(token string) []string {
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] == '.' {
			return []string{token[:i], token[i+1:]}
		}
	}
	return nil
}

// signPayload 将 payload HMAC-SHA256 签名后返回 token 和过期时间
// expireAt 直接传入，避免 json.Marshal 不改变 Go 运行时类型导致 int64→float64 断言失败
func signPayload(payload map[string]interface{}, expireAt int64, secret string) (string, int64, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signatureB64 := base64.RawURLEncoding.EncodeToString(hmacSha256(headerB64+"."+payloadB64, secret))

	return headerB64 + "." + payloadB64 + "." + signatureB64, expireAt, nil
}

func hmacSha256(message, secret string) []byte {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return h.Sum(nil)
}
