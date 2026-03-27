package logic

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"seckill-mall/user-service/internal/model"
	"seckill-mall/user-service/internal/svc"
	"seckill-mall/user-service/user"

	"github.com/zeromicro/go-zero/core/logx"
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

// Login 用户登录
func (l *LoginLogic) Login(in *user.LoginRequest) (*user.LoginResponse, error) {
	// 参数校验
	if in.Username == "" || in.Password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}

	// 查询用户
	existingUser, err := l.svcCtx.UserModel.FindOneByUsername(l.ctx, in.Username)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("用户不存在")
		}
		l.Logger.Errorf("查询用户失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 检查用户状态
	if existingUser.Status != 1 {
		return nil, errors.New("账号已被禁用")
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(existingUser.Password), []byte(in.Password)); err != nil {
		l.Logger.Infof("密码错误: username=%s", in.Username)
		return nil, errors.New("密码错误")
	}

	// 生成 JWT Token
	token, expireAt, err := l.generateToken(existingUser.ID)
	if err != nil {
		l.Logger.Errorf("生成Token失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 将 Token 加入 Redis 黑名单（用于后续登出功能）
	tokenKey := "user:token:" + token
	l.svcCtx.Redis.Setex(tokenKey, "1", int(l.svcCtx.Config.JWTConfig.AccessExpire))

	l.Logger.Infof("用户登录成功: userId=%d, username=%s", existingUser.ID, existingUser.Username)

	return &user.LoginResponse{
		UserId:   existingUser.ID,
		Username: existingUser.Username,
		Email:    existingUser.Email,
		Token:    token,
		ExpireAt: expireAt,
	}, nil
}

// generateToken 生成 JWT Token
// 使用简单的自定义 JWT 实现
func (l *LoginLogic) generateToken(userId int64) (string, int64, error) {
	now := time.Now().Unix()
	expireAt := now + l.svcCtx.Config.JWTConfig.AccessExpire

	// header
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	// payload
	payload := map[string]interface{}{
		"userId": userId,
		"iat":    now,
		"exp":    expireAt,
	}
	payloadJSON, _ := json.Marshal(payload)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	// signature
	signature := hmacSha256(headerB64+"."+payloadB64, l.svcCtx.Config.JWTConfig.AccessSecret)
	signatureB64 := base64.RawURLEncoding.EncodeToString(signature)

	// token
	token := headerB64 + "." + payloadB64 + "." + signatureB64

	return token, expireAt, nil
}

// hmacSha256 计算 HMAC-SHA256
func hmacSha256(message, secret string) []byte {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return h.Sum(nil)
}

// parseToken 解析并验证 JWT Token
func parseToken(tokenString, secret string) (map[string]interface{}, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	headerB64, payloadB64, signatureB64 := parts[0], parts[1], parts[2]

	// 验证签名
	expectedSig := hmacSha256(headerB64+"."+payloadB64, secret)
	actualSig, err := base64.RawURLEncoding.DecodeString(signatureB64)
	if err != nil {
		return nil, errors.New("invalid signature encoding")
	}
	if !hmac.Equal(expectedSig, actualSig) {
		return nil, errors.New("invalid signature")
	}

	// 解析 payload
	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, errors.New("invalid payload encoding")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, errors.New("invalid payload format")
	}

	// 检查过期时间
	if exp, ok := payload["exp"].(float64); ok {
		if int64(exp) < time.Now().Unix() {
			return nil, errors.New("token expired")
		}
	}

	return payload, nil
}
