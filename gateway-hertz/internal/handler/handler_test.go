package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	seckillpb "seckill-mall/common/seckill"
	userpb "seckill-mall/common/user"
	"seckill-mall/gateway-hertz/internal/handler"
	"seckill-mall/gateway-hertz/internal/middleware"
	"seckill-mall/gateway-hertz/internal/mock"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---- 测试辅助函数 ----

// buildTestToken 构造一个签名正确的 JWT access token，用于测试
func buildTestToken(secret string, userId int64, jti string, ttlSeconds int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	exp := time.Now().Unix() + ttlSeconds
	payloadJSON := fmt.Sprintf(`{"userId":%d,"jti":"%s","exp":%d}`, userId, jti, exp)
	payload := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	msg := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return msg + "." + sig
}

// newTestRedis 启动 miniredis，返回 redis.Client
func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr
}

// doRequest 向 Hertz 引擎发送请求，返回状态码和解析后的 JSON body
func doRequest(t *testing.T, h *server.Hertz, method, path string, body interface{}, headers map[string]string) (int, map[string]interface{}) {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		require.NoError(t, err)
	}
	hdrs := []ut.Header{{Key: "Content-Type", Value: "application/json"}}
	for k, v := range headers {
		hdrs = append(hdrs, ut.Header{Key: k, Value: v})
	}
	w := ut.PerformRequest(h.Engine, method, path,
		&ut.Body{Body: bytes.NewReader(bodyBytes), Len: len(bodyBytes)},
		hdrs...,
	)
	resp := w.Result()
	var result map[string]interface{}
	_ = json.Unmarshal(resp.Body(), &result)
	return resp.StatusCode(), result
}

// authHeader 生成 Bearer token header
func authHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// ---- 健康检查 ----

func TestHealthCheck(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/health", handler.HealthHandler)

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/health", nil)
	assert.Equal(t, consts.StatusOK, w.Result().StatusCode())

	var result map[string]string
	_ = json.Unmarshal(w.Result().Body(), &result)
	assert.Equal(t, "ok", result["status"])
	assert.Equal(t, "gateway-hertz", result["service"])
}

// ---- CORS 中间件 ----

func TestCORS_OptionsRequest(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.Use(middleware.CORS())
	h.GET("/api/v1/test", func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"ok": "true"})
	})

	w := ut.PerformRequest(h.Engine, http.MethodOptions, "/api/v1/test", nil,
		ut.Header{Key: "Origin", Value: "http://localhost:3000"},
	)
	assert.Equal(t, consts.StatusNoContent, w.Result().StatusCode())
	assert.Equal(t, "*", string(w.Result().Header.Get("Access-Control-Allow-Origin")))
}

func TestCORS_NormalRequest(t *testing.T) {
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.Use(middleware.CORS())
	h.GET("/api/v1/test", func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"ok": "true"})
	})

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/api/v1/test", nil,
		ut.Header{Key: "Origin", Value: "http://localhost:3000"},
	)
	assert.Equal(t, consts.StatusOK, w.Result().StatusCode())
	assert.Equal(t, "*", string(w.Result().Header.Get("Access-Control-Allow-Origin")))
}

// ---- JWT 中间件 ----

func TestJWTAuth_NoToken(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/protected", middleware.JWTAuth("secret", rdb), func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"ok": "true"})
	})

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/protected", nil)
	assert.Equal(t, consts.StatusUnauthorized, w.Result().StatusCode())
}

func TestJWTAuth_InvalidToken(t *testing.T) {
	rdb, _ := newTestRedis(t)
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/protected", middleware.JWTAuth("secret", rdb), func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"ok": "true"})
	})

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/protected", nil,
		ut.Header{Key: "Authorization", Value: "Bearer not.a.valid.token"},
	)
	assert.Equal(t, consts.StatusUnauthorized, w.Result().StatusCode())
}

func TestJWTAuth_ExpiredToken(t *testing.T) {
	rdb, _ := newTestRedis(t)
	token := buildTestToken("secret", 1, "jti-expired", -10) // 已过期

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/protected", middleware.JWTAuth("secret", rdb), func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"ok": "true"})
	})

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/protected", nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	assert.Equal(t, consts.StatusUnauthorized, w.Result().StatusCode())
}

func TestJWTAuth_BlacklistedToken(t *testing.T) {
	rdb, mr := newTestRedis(t)
	jti := "blacklisted-jti"
	mr.Set("user:blacklist:"+jti, "1")

	token := buildTestToken("secret", 1, jti, 900)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/protected", middleware.JWTAuth("secret", rdb), func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"ok": "true"})
	})

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/protected", nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	assert.Equal(t, consts.StatusUnauthorized, w.Result().StatusCode())
}

func TestJWTAuth_ValidToken_SetsUserId(t *testing.T) {
	rdb, _ := newTestRedis(t)
	token := buildTestToken("secret", 42, "jti-valid", 900)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/protected", middleware.JWTAuth("secret", rdb), func(_ context.Context, c *app.RequestContext) {
		uid := middleware.GetUserIdFromContext(c)
		c.JSON(consts.StatusOK, map[string]interface{}{"userId": uid})
	})

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/protected", nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	assert.Equal(t, consts.StatusOK, w.Result().StatusCode())
	var result map[string]interface{}
	_ = json.Unmarshal(w.Result().Body(), &result)
	assert.Equal(t, float64(42), result["userId"])
}

func TestJWTAuth_WrongSecret(t *testing.T) {
	rdb, _ := newTestRedis(t)
	token := buildTestToken("wrong-secret", 1, "jti-x", 900)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/protected", middleware.JWTAuth("correct-secret", rdb), func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"ok": "true"})
	})

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/protected", nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	assert.Equal(t, consts.StatusUnauthorized, w.Result().StatusCode())
}

// ---- 用户注册 ----

func TestRegister_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().
		Register(gomock.Any(), gomock.Any()).
		Return(&userpb.UserInfo{Id: 1, Username: "testuser", Email: "test@example.com"}, nil)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/register", handler.NewUserHandler(mockUser).Register)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/user/register", map[string]interface{}{
		"username": "testuser",
		"password": "password123",
		"email":    "test@example.com",
	}, nil)

	assert.Equal(t, consts.StatusOK, code)
	assert.Equal(t, float64(0), resp["code"])
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "testuser", data["username"])
}

func TestRegister_MissingFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().Register(gomock.Any(), gomock.Any()).Times(0)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/register", handler.NewUserHandler(mockUser).Register)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/user/register", map[string]interface{}{
		"username": "testuser",
		// 缺少 password 和 email
	}, nil)

	assert.Equal(t, consts.StatusBadRequest, code)
	assert.Equal(t, float64(400), resp["code"])
}

func TestRegister_RPCError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().
		Register(gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.AlreadyExists, "用户名已存在"))

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/register", handler.NewUserHandler(mockUser).Register)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/user/register", map[string]interface{}{
		"username": "testuser",
		"password": "password123",
		"email":    "test@example.com",
	}, nil)

	assert.Equal(t, consts.StatusBadRequest, code)
	assert.NotEqual(t, float64(0), resp["code"])
}

// ---- 用户登录 ----

func TestLogin_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().
		Login(gomock.Any(), &userpb.LoginRequest{Username: "testuser", Password: "password123"}).
		Return(&userpb.LoginResponse{
			UserId:          1,
			Username:        "testuser",
			Email:           "test@example.com",
			AccessToken:     "access.token.here",
			AccessExpireAt:  time.Now().Add(15 * time.Minute).Unix(),
			RefreshToken:    "refresh-token-uuid",
			RefreshExpireAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
		}, nil)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/login", handler.NewUserHandler(mockUser).Login)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/user/login", map[string]interface{}{
		"username": "testuser",
		"password": "password123",
	}, nil)

	assert.Equal(t, consts.StatusOK, code)
	assert.Equal(t, float64(0), resp["code"])
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "access.token.here", data["accessToken"])
	assert.Equal(t, "refresh-token-uuid", data["refreshToken"])
}

func TestLogin_WrongPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().
		Login(gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.Unauthenticated, "用户名或密码错误"))

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/login", handler.NewUserHandler(mockUser).Login)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/user/login", map[string]interface{}{
		"username": "testuser",
		"password": "wrongpassword",
	}, nil)

	assert.Equal(t, consts.StatusUnauthorized, code)
	assert.Equal(t, float64(401), resp["code"])
}

func TestLogin_EmptyFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().Login(gomock.Any(), gomock.Any()).Times(0)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/login", handler.NewUserHandler(mockUser).Login)

	code, _ := doRequest(t, h, http.MethodPost, "/api/v1/user/login", map[string]interface{}{
		"username": "",
		"password": "",
	}, nil)

	assert.Equal(t, consts.StatusBadRequest, code)
}

// ---- Token 刷新 ----

func TestRefreshToken_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().
		RefreshToken(gomock.Any(), &userpb.RefreshTokenRequest{RefreshToken: "valid-refresh-token"}).
		Return(&userpb.RefreshTokenResponse{
			AccessToken:     "new.access.token",
			AccessExpireAt:  time.Now().Add(15 * time.Minute).Unix(),
			RefreshToken:    "new-refresh-token",
			RefreshExpireAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
		}, nil)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/refresh", handler.NewUserHandler(mockUser).RefreshToken)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/user/refresh", map[string]interface{}{
		"refreshToken": "valid-refresh-token",
	}, nil)

	assert.Equal(t, consts.StatusOK, code)
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "new.access.token", data["accessToken"])
	assert.Equal(t, "new-refresh-token", data["refreshToken"])
}

func TestRefreshToken_InvalidToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().
		RefreshToken(gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.Unauthenticated, "refresh token 无效"))

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/refresh", handler.NewUserHandler(mockUser).RefreshToken)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/user/refresh", map[string]interface{}{
		"refreshToken": "invalid-token",
	}, nil)

	assert.Equal(t, consts.StatusUnauthorized, code)
	assert.Equal(t, float64(401), resp["code"])
}

func TestRefreshToken_EmptyToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUser := mock.NewMockUserServiceClient(ctrl)
	mockUser.EXPECT().RefreshToken(gomock.Any(), gomock.Any()).Times(0)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/user/refresh", handler.NewUserHandler(mockUser).RefreshToken)

	code, _ := doRequest(t, h, http.MethodPost, "/api/v1/user/refresh", map[string]interface{}{
		"refreshToken": "",
	}, nil)

	assert.Equal(t, consts.StatusBadRequest, code)
}

// ---- 秒杀接口 ----

func TestSeckill_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSeckill := mock.NewMockSeckillServiceClient(ctrl)
	mockSeckill.EXPECT().
		Seckill(gomock.Any(), gomock.Any()).
		Return(&seckillpb.SeckillResponse{
			Success: true,
			Code:    "1",
			Message: "秒杀成功",
			OrderId: "S101_1234567890",
		}, nil)

	rdb, _ := newTestRedis(t)
	token := buildTestToken("secret", 9527, "jti-seckill", 900)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/seckill",
		middleware.JWTAuth("secret", rdb),
		handler.NewSeckillHandler(mockSeckill).Seckill,
	)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/seckill", map[string]interface{}{
		"seckillProductId": 101,
		"quantity":         1,
	}, authHeader(token))

	assert.Equal(t, consts.StatusOK, code)
	assert.Equal(t, float64(0), resp["code"])
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "S101_1234567890", data["orderId"])
}

func TestSeckill_Unauthorized(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSeckill := mock.NewMockSeckillServiceClient(ctrl)
	mockSeckill.EXPECT().Seckill(gomock.Any(), gomock.Any()).Times(0)

	rdb, _ := newTestRedis(t)
	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/seckill",
		middleware.JWTAuth("secret", rdb),
		handler.NewSeckillHandler(mockSeckill).Seckill,
	)

	code, _ := doRequest(t, h, http.MethodPost, "/api/v1/seckill", map[string]interface{}{
		"seckillProductId": 101,
	}, nil) // 不带 token

	assert.Equal(t, consts.StatusUnauthorized, code)
}

func TestSeckill_MissingProductId(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSeckill := mock.NewMockSeckillServiceClient(ctrl)
	mockSeckill.EXPECT().Seckill(gomock.Any(), gomock.Any()).Times(0)

	rdb, _ := newTestRedis(t)
	token := buildTestToken("secret", 9527, "jti-missing", 900)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/seckill",
		middleware.JWTAuth("secret", rdb),
		handler.NewSeckillHandler(mockSeckill).Seckill,
	)

	code, resp := doRequest(t, h, http.MethodPost, "/api/v1/seckill", map[string]interface{}{
		"quantity": 1,
		// 缺少 seckillProductId
	}, authHeader(token))

	assert.Equal(t, consts.StatusBadRequest, code)
	assert.Equal(t, float64(400), resp["code"])
}

func TestSeckill_RPCError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSeckill := mock.NewMockSeckillServiceClient(ctrl)
	mockSeckill.EXPECT().
		Seckill(gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.Internal, "内部错误"))

	rdb, _ := newTestRedis(t)
	token := buildTestToken("secret", 9527, "jti-rpcerr", 900)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.POST("/api/v1/seckill",
		middleware.JWTAuth("secret", rdb),
		handler.NewSeckillHandler(mockSeckill).Seckill,
	)

	code, _ := doRequest(t, h, http.MethodPost, "/api/v1/seckill", map[string]interface{}{
		"seckillProductId": 101,
		"quantity":         1,
	}, authHeader(token))

	assert.Equal(t, consts.StatusInternalServerError, code)
}

// ---- 秒杀结果查询 ----

func TestGetSeckillResult_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSeckill := mock.NewMockSeckillServiceClient(ctrl)
	mockSeckill.EXPECT().
		GetSeckillResult(gomock.Any(), &seckillpb.SeckillResultRequest{OrderId: "S101_9999"}).
		Return(&seckillpb.SeckillResultResponse{
			Success:     true,
			OrderId:     "S101_9999",
			ProductName: "测试商品",
			Quantity:    1,
			Amount:      9900,
			Status:      "success",
		}, nil)

	rdb, _ := newTestRedis(t)
	token := buildTestToken("secret", 9527, "jti-result", 900)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/api/v1/seckill/result",
		middleware.JWTAuth("secret", rdb),
		handler.NewSeckillHandler(mockSeckill).GetSeckillResult,
	)

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/api/v1/seckill/result?orderId=S101_9999", nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	assert.Equal(t, consts.StatusOK, w.Result().StatusCode())

	var result map[string]interface{}
	_ = json.Unmarshal(w.Result().Body(), &result)
	data := result["data"].(map[string]interface{})
	assert.Equal(t, "S101_9999", data["orderId"])
	assert.Equal(t, "success", data["status"])
}

func TestGetSeckillResult_MissingOrderId(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSeckill := mock.NewMockSeckillServiceClient(ctrl)
	mockSeckill.EXPECT().GetSeckillResult(gomock.Any(), gomock.Any()).Times(0)

	rdb, _ := newTestRedis(t)
	token := buildTestToken("secret", 9527, "jti-noid", 900)

	h := server.New(server.WithHostPorts("127.0.0.1:0"))
	h.GET("/api/v1/seckill/result",
		middleware.JWTAuth("secret", rdb),
		handler.NewSeckillHandler(mockSeckill).GetSeckillResult,
	)

	w := ut.PerformRequest(h.Engine, http.MethodGet, "/api/v1/seckill/result", nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token},
	)
	assert.Equal(t, consts.StatusBadRequest, w.Result().StatusCode())
}
