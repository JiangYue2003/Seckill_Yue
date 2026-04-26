package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

type UserToken struct {
	UserID int64
	Token  string
}

func BuildJWTPool(startUserID int64, count int64, secret string) []UserToken {
	now := time.Now().Unix()
	exp := now + 3600 // 1 hour
	pool := make([]UserToken, 0, count)
	for i := int64(0); i < count; i++ {
		userID := startUserID + i
		token := generateJWT(userID, now, exp, secret)
		pool = append(pool, UserToken{
			UserID: userID,
			Token:  token,
		})
	}
	return pool
}

func generateJWT(userID int64, iat, exp int64, secret string) string {
	header := `{"alg":"HS256","typ":"JWT"}`
	payload := `{"userId":` + strconv.FormatInt(userID, 10) +
		`,"exp":` + strconv.FormatInt(exp, 10) +
		`,"iat":` + strconv.FormatInt(iat, 10) +
		`,"jti":"bench_` + strconv.FormatInt(userID, 10) + `_` + strconv.FormatInt(iat, 10) + `"}`

	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(header))
	payloadB64 := base64.RawURLEncoding.EncodeToString([]byte(payload))
	msg := headerB64 + "." + payloadB64

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(msg))
	sig := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return fmt.Sprintf("%s.%s", msg, sigB64)
}
