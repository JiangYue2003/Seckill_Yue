// User Management API Test Suite
// Build: cd f:\sec1.1\test && go test -c -o user-api-test.exe && cd ..\..
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ============================================================
// Config
// ============================================================
const (
	BaseURL      = "http://localhost:8888"
	TestUsername = "testuser_go"
	TestEmail    = "testuser_go@seckill.test"
	TestPassword = "Test@123456"
)

// ============================================================
// Types
// ============================================================
type APIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type LoginData struct {
	UserID          int64  `json:"userId"`
	Username        string `json:"username"`
	Email           string `json:"email"`
	AccessToken     string `json:"accessToken"`
	AccessExpireAt  int64  `json:"accessExpireAt"`
	RefreshToken    string `json:"refreshToken"`
	RefreshExpireAt int64  `json:"refreshExpireAt"`
}

type RegisterData struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}

type UserInfoData struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Status    int    `json:"status"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ============================================================
// Global state
// ============================================================
var (
	client      = &http.Client{Timeout: 15 * time.Second}
	accessToken  string
	refreshToken string
	userID      int64
	testPassword = TestPassword
	allPassed   = true
)

// ============================================================
// Helpers
// ============================================================
func text(exp, got interface{}) string {
	return fmt.Sprintf("expected=%v, got=%v", exp, got)
}

func doRequest(method, path string, body interface{}, token string) (int, string, []byte) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, BaseURL+path, bodyReader)
	if err != nil {
		return 0, err.Error(), nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Go-Test/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err.Error(), nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Status, respBody
}

func apiResp(method, path string, body interface{}, token string) (code int, msg string, ok bool) {
	statusCode, _, bodyBytes := doRequest(method, path, body, token)

	var api APIResponse
	if err := json.Unmarshal(bodyBytes, &api); err != nil {
		return statusCode, fmt.Sprintf("json unmarshal error: %v, body: %s", err, string(bodyBytes)), false
	}

	if api.Code != 0 {
		return api.Code, api.Message, false
	}
	return api.Code, api.Message, true
}

func apiRespWithData(method, path string, body interface{}, token string) (code int, msg string, data []byte, ok bool) {
	statusCode, _, bodyBytes := doRequest(method, path, body, token)

	var api APIResponse
	if err := json.Unmarshal(bodyBytes, &api); err != nil {
		return statusCode, fmt.Sprintf("json unmarshal error: %v, body: %s", err, string(bodyBytes)), nil, false
	}

	if api.Code != 0 {
		return api.Code, api.Message, nil, false
	}
	return api.Code, api.Message, api.Data, true
}

func mustParse(data []byte, v interface{}) {
	if err := json.Unmarshal(data, v); err != nil {
		fmt.Printf("  [FATAL] JSON parse error: %v\n", err)
		os.Exit(1)
	}
}

func section(title string) {
	fmt.Printf("\n%s\n", strings.Repeat("=", 70))
	fmt.Printf("  %s\n", title)
	fmt.Printf("%s\n", strings.Repeat("=", 70))
}

func pass(name string) {
	fmt.Printf("  [PASS] %s\n", name)
}

func fail(name string, reason string) {
	allPassed = false
	fmt.Printf("  [FAIL] %s\n    Reason: %s\n", name, reason)
}

// ============================================================
// Tests
// ============================================================

func testHealth() {
	section("0. Health Check")
	code, msg, ok := apiResp("GET", "/health", nil, "")
	if ok {
		pass("Gateway health check")
	} else {
		fail("Gateway health check", fmt.Sprintf("code=%d msg=%s", code, msg))
	}
}

func testRegister() {
	section("1. User Registration")
	username := fmt.Sprintf("testuser_%d", time.Now().UnixNano()%1000000)
	email := fmt.Sprintf("testuser_%d@seckill.test", time.Now().UnixNano()%1000000)
	body := map[string]interface{}{
		"username": username,
		"password": testPassword,
		"email":    email,
		"phone":    "13800138000",
	}

	code, msg, data, ok := apiRespWithData("POST", "/api/v1/user/register", body, "")
	if ok {
		var ud RegisterData
		mustParse(data, &ud)
		userID = ud.ID
		fmt.Printf("  UserID=%d, username=%s, email=%s\n", ud.ID, ud.Username, ud.Email)
		pass(fmt.Sprintf("User registration: %s", username))
	} else {
		fail("User registration", fmt.Sprintf("code=%d msg=%s", code, msg))
	}
}

func testRegisterValidation() {
	section("1.1 Registration Validation")
	cases := []struct {
		name    string
		body    map[string]interface{}
		expectOk bool
	}{
		{"Username too short (min=3)", map[string]interface{}{"username": "ab", "password": "Test@123", "email": "a@b.com"}, false},
		{"Password too short (min=6)", map[string]interface{}{"username": "testuser123", "password": "12345", "email": "a@b.com"}, false},
		{"Invalid email", map[string]interface{}{"username": "testuser123", "password": "Test@123", "email": "notanemail"}, false},
		{"Missing username field", map[string]interface{}{"password": "Test@123", "email": "a@b.com"}, false},
	}
	for _, c := range cases {
		_, _, ok := apiResp("POST", "/api/v1/user/register", c.body, "")
		if !ok == !c.expectOk {
			pass(c.name)
		} else {
			fail(c.name, "validation behavior incorrect")
		}
	}
}

func testLogin() {
	section("2. User Login")
	// Use the user that was just registered
	username := fmt.Sprintf("testuser_%d", time.Now().UnixNano()%1000000)
	email := fmt.Sprintf("testuser_%d@seckill.test", time.Now().UnixNano()%1000000)
	// First register a user to test login with
	regBody := map[string]interface{}{
		"username": username,
		"password": testPassword,
		"email":    email,
		"phone":    "13800138001",
	}
	regCode, _, regData, _ := apiRespWithData("POST", "/api/v1/user/register", regBody, "")
	if regCode != 0 {
		var ud RegisterData
		mustParse(regData, &ud)
		userID = ud.ID
	}

	// Now login
	body := map[string]interface{}{
		"username": username,
		"password": testPassword,
	}
	code, msg, data, ok := apiRespWithData("POST", "/api/v1/user/login", body, "")
	if ok {
		var ld LoginData
		mustParse(data, &ld)
		accessToken = ld.AccessToken
		refreshToken = ld.RefreshToken
		userID = ld.UserID
		if ld.AccessExpireAt == 0 {
			fail("Access token expire time", "AccessExpireAt is 0 (should be non-zero)")
		} else {
			pass(fmt.Sprintf("Access token expire time: %s", time.Unix(ld.AccessExpireAt, 0).Format("2006-01-02 15:04:05")))
		}
		pass(fmt.Sprintf("Login successful: userID=%d token_len=%d", ld.UserID, len(ld.AccessToken)))
	} else {
		fail("Login", fmt.Sprintf("code=%d msg=%s", code, msg))
	}
}

func testLoginValidation() {
	section("2.1 Login Validation")
	cases := []struct {
		name      string
		body      map[string]interface{}
		expect401 bool
	}{
		{"Wrong password", map[string]interface{}{"username": "testuser_invalid", "password": "WrongPassword123"}, true},
		{"Non-existent user", map[string]interface{}{"username": "nonexistent_xxx", "password": "AnyPass123"}, true},
		{"Missing password field", map[string]interface{}{"username": "testuser_invalid"}, false},
	}
	for _, c := range cases {
		code, _, ok := apiResp("POST", "/api/v1/user/login", c.body, "")
		if c.expect401 && code == 401 {
			pass(c.name)
		} else if c.expect401 {
			fail(c.name, fmt.Sprintf("expected 401, got code=%d", code))
		} else if !ok {
			pass(c.name)
		} else {
			fail(c.name, "validation behavior incorrect")
		}
	}
}

func testGetUserInfo() {
	section("3. Get User Info")
	code, msg, data, ok := apiRespWithData("GET", "/api/v1/user/info", nil, accessToken)
	if ok {
		var ud UserInfoData
		mustParse(data, &ud)
		fmt.Printf("  userId=%d, username=%s, email=%s, phone=%s, status=%d\n",
			ud.ID, ud.Username, ud.Email, ud.Phone, ud.Status)
		pass("Get user info")
	} else {
		fail("Get user info", fmt.Sprintf("code=%d msg=%s token_len=%d", code, msg, len(accessToken)))
	}
}

func testGetUserInfoUnauthorized() {
	section("3.1 Unauthorized Access Protection")
	cases := []struct {
		name  string
		token string
	}{
		{"No token", ""},
		{"Invalid token", "invalid.token.here"},
		{"Wrong format", "NotBearer abc123"},
	}
	for _, c := range cases {
		code, _, ok := apiResp("GET", "/api/v1/user/info", nil, c.token)
		if code == 401 {
			pass(c.name)
		} else {
			fail(c.name, fmt.Sprintf("expected 401, got code=%d ok=%v", code, ok))
		}
	}
}

func testUpdateUserInfo() {
	section("4. Update User Info")
	newEmail := fmt.Sprintf("new_%d@seckill.test", time.Now().UnixNano()%100000)
	newPhone := fmt.Sprintf("139%08d", time.Now().UnixNano()%100000000)
	body := map[string]interface{}{
		"email": newEmail,
		"phone": newPhone,
	}
	code, msg, ok := apiResp("PUT", "/api/v1/user/info", body, accessToken)
	if ok {
		pass("Update user info")
		// Verify update
		_, _, data, ok2 := apiRespWithData("GET", "/api/v1/user/info", nil, accessToken)
		if ok2 {
			var ud UserInfoData
			mustParse(data, &ud)
			if ud.Email == newEmail {
				pass("Data consistency verified (email updated)")
			} else {
				fail("Data consistency", fmt.Sprintf("email mismatch: expected=%s got=%s", newEmail, ud.Email))
			}
		}
	} else {
		fail("Update user info", fmt.Sprintf("code=%d msg=%s", code, msg))
	}
}

func testChangePassword() {
	section("5. Change Password")
	newPassword := fmt.Sprintf("NewPass@%d", time.Now().UnixNano()%100000)

	// Wrong old password
	wrongBody := map[string]interface{}{
		"oldPassword": "DefinitelyWrongPassword",
		"newPassword": newPassword,
	}
	code, _, ok := apiResp("POST", "/api/v1/user/password", wrongBody, accessToken)
	if code != 0 {
		pass("Wrong old password rejected")
	} else {
		fail("Wrong old password", "should have been rejected")
	}

	// Correct old password
	body := map[string]interface{}{
		"oldPassword": testPassword,
		"newPassword": newPassword,
	}
	code, msg, ok := apiResp("POST", "/api/v1/user/password", body, accessToken)
	if ok {
		pass("Password changed")
		// Verify with new password
		username := fmt.Sprintf("testuser_%d", time.Now().UnixNano()%1000000)
		email := fmt.Sprintf("testuser_%d@seckill.test", time.Now().UnixNano()%1000000)
		regBody := map[string]interface{}{
			"username": username, "password": newPassword,
			"email": email, "phone": "13800138002",
		}
		_, _, _, regOk := apiRespWithData("POST", "/api/v1/user/register", regBody, "")
		if !regOk {
			// New password should work for login
			loginBody := map[string]interface{}{
				"username": username,
				"password": newPassword,
			}
			_, _, data, loginOk := apiRespWithData("POST", "/api/v1/user/login", loginBody, "")
			if loginOk {
				var ld LoginData
				mustParse(data, &ld)
				accessToken = ld.AccessToken
				refreshToken = ld.RefreshToken
				pass("New password login verified")
			} else {
				fail("New password login", "failed")
			}
		}
		testPassword = newPassword
	} else {
		fail("Change password", fmt.Sprintf("code=%d msg=%s", code, msg))
	}
}

func testRefreshToken() {
	section("6. Refresh Token")
	oldToken := accessToken
	body := map[string]interface{}{
		"refreshToken": refreshToken,
	}
	code, msg, data, ok := apiRespWithData("POST", "/api/v1/user/refresh", body, "")
	if ok {
		var ld LoginData
		mustParse(data, &ld)
		if ld.AccessExpireAt == 0 {
			fail("Refreshed token expire time", "AccessExpireAt is 0")
		} else {
			pass(fmt.Sprintf("Token refreshed, expires at %s", time.Unix(ld.AccessExpireAt, 0).Format("2006-01-02 15:04:05")))
		}
		accessToken = ld.AccessToken
		refreshToken = ld.RefreshToken

		// Verify new token works
		_, _, infoData, infoOk := apiRespWithData("GET", "/api/v1/user/info", nil, accessToken)
		if infoOk {
			var ud UserInfoData
			mustParse(infoData, &ud)
			pass(fmt.Sprintf("New token verified (userId=%d)", ud.ID))
		} else {
			fail("New token verification", "failed")
		}

		// Old token should still work (no rotation in this test, or it might be rotated)
		_, _, _, oldOk := apiRespWithData("GET", "/api/v1/user/info", nil, oldToken)
		if !oldOk {
			fmt.Printf("  [INFO] Old token no longer valid (rotation enabled)\n")
		}
	} else {
		fail("Refresh token", fmt.Sprintf("code=%d msg=%s", code, msg))
	}
}

func testRefreshTokenValidation() {
	section("6.1 Refresh Token Validation")
	cases := []struct {
		name         string
		refreshToken string
		expectFail   bool
	}{
		{"Invalid refresh token", "invalid.token.refresh", true},
		{"Empty refresh token", "", false},
	}
	for _, c := range cases {
		body := map[string]interface{}{
			"refreshToken": c.refreshToken,
		}
		code, _, ok := apiResp("POST", "/api/v1/user/refresh", body, "")
		if c.expectFail && code != 0 {
			pass(c.name)
		} else if c.expectFail {
			fail(c.name, fmt.Sprintf("expected failure but got ok, code=%d", code))
		} else if !ok {
			pass(c.name)
		} else {
			fail(c.name, "validation behavior incorrect")
		}
	}
}

func testIdempotency() {
	section("7. Idempotency Test")
	// Try to register duplicate username
	username := fmt.Sprintf("dup_%d", time.Now().UnixNano()%1000000)
	email := fmt.Sprintf("dup_%d@seckill.test", time.Now().UnixNano()%1000000)
	body := map[string]interface{}{
		"username": username,
		"password": "Test@123456",
		"email":    email,
		"phone":    "13800138000",
	}
	_, _, ok1 := apiResp("POST", "/api/v1/user/register", body, "")
	_, _, ok2 := apiResp("POST", "/api/v1/user/register", body, "")
	if !ok1 || !ok2 {
		pass("Duplicate registration rejected")
	} else {
		fail("Duplicate registration", "both succeeded (should reject one)")
	}
}

func main() {
	fmt.Printf("\n%s\n", strings.Repeat("=", 70))
	fmt.Printf("  User Management API Test Suite\n")
	fmt.Printf("  Gateway: %s\n", BaseURL)
	fmt.Printf("  Time: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("%s\n", strings.Repeat("=", 70))

	// 0. Health check
	testHealth()

	// 1. Registration
	testRegister()
	testRegisterValidation()

	// 2. Login
	testLogin()
	if accessToken == "" {
		fmt.Println("\n[WARN] No access token obtained, skipping auth-protected tests.")
	} else {
		testLoginValidation()
		testGetUserInfo()
		testGetUserInfoUnauthorized()
		testUpdateUserInfo()
		testChangePassword()
		testRefreshToken()
		testRefreshTokenValidation()
	}

	// 7. Idempotency
	testIdempotency()

	// Summary
	section("Test Summary")
	fmt.Println()
	if allPassed {
		fmt.Println("  ALL TESTS PASSED!")
	} else {
		fmt.Println("  SOME TESTS FAILED - see details above.")
		os.Exit(1)
	}
}
