# ============================================
#  Seckill System - User Service Test Script
#  Tests all user management APIs via Gateway
# ============================================
#
# API Endpoints:
#   POST /api/v1/user/register      - Register (no auth)
#   POST /api/v1/user/login         - Login (no auth)
#   GET  /api/v1/user/info          - Get user info (auth required)
#   PUT  /api/v1/user/info          - Update user info (auth required)
#   POST /api/v1/user/password      - Change password (auth required)
# ============================================

$ErrorActionPreference = "Continue"
$BASE_URL = "http://localhost:8888"

# Use unique username so tests are idempotent
$TIMESTAMP = [int64](Get-Date -UFormat "%s")
$TEST_USER = "testuser_$TIMESTAMP"
$TEST_PASS = "Test1234"
$TEST_EMAIL = "test_$TIMESTAMP@example.com"
$TEST_PHONE = "13800138000"

$GLOBAL:USER_ID = $null
$GLOBAL:AUTH_TOKEN = $null

function Write-TestHeader {
    param($Title)
    Write-Host ""
    Write-Host ("=" * 60) -ForegroundColor Cyan
    Write-Host "  $Title" -ForegroundColor Cyan
    Write-Host ("=" * 60) -ForegroundColor Cyan
}

function Write-TestCase {
    param($Name)
    Write-Host ""
    Write-Host "[TEST] $Name" -ForegroundColor White
    Write-Host ("-" * 50) -ForegroundColor Gray
}

function Write-Result {
    param([bool]$Passed, $Message)
    if ($Passed) {
        Write-Host "  [PASS] $Message" -ForegroundColor Green
    } else {
        Write-Host "  [FAIL] $Message" -ForegroundColor Red
    }
}

function Write-Skip {
    param($Message)
    Write-Host "  [SKIP] $Message" -ForegroundColor Yellow
}

function Test-ApiCall {
    param(
        [string]$Method,
        [string]$Url,
        [hashtable]$HeadersParam = @{},
        [string]$BodyParam = ""
    )

    try {
        $params = @{
            Uri         = $Url
            Method      = $Method
            ContentType = "application/json"
            Headers     = $HeadersParam
            TimeoutSec  = 15
        }
        if ($BodyParam) {
            $params.Body = $BodyParam
        }

        $resp = Invoke-WebRequest @params -UseBasicParsing 2>$null
        $json = $null
        try { $json = $resp.Content | ConvertFrom-Json } catch { }

        return @{
            Success    = $true
            StatusCode = $resp.StatusCode
            Json       = $json
            Raw        = $resp.Content
        }
    } catch {
        $json = $null
        try {
            $json = $_.ErrorDetails.Message | ConvertFrom-Json
        } catch { }

        return @{
            Success    = $false
            StatusCode = $_.Exception.Response.StatusCode
            Json       = $json
            Raw        = $_.ErrorDetails.Message
            ErrorMsg   = $_.Exception.Message
        }
    }
}

function Test-Success {
    param($result)
    return ($result.Success -and ($result.Json.code -eq 0 -or $result.Json.code -eq 200))
}

# ============================================
# START
# ============================================
Write-TestHeader "Seckill System - User Service API Test"

Write-Host ""
Write-Host "Gateway URL: $BASE_URL" -ForegroundColor Yellow
Write-Host "Test User: $TEST_USER" -ForegroundColor Yellow

# ============================================
# 1. Health Check
# ============================================
Write-TestHeader "1. Health Check"
Write-TestCase "GET /health"

$result = Test-ApiCall -Method "GET" -Url "$BASE_URL/health"

if ($result.Success) {
    Write-Result -Passed $true -Message "Gateway is running"
    Write-Host "  Response: $($result.Raw)" -ForegroundColor Gray
} else {
    Write-Result -Passed $false -Message "Gateway is NOT reachable"
    Write-Host "  Error: $($result.ErrorMsg)" -ForegroundColor Red
    Write-Host ""
    Write-Host "Please ensure all services are running (scripts/start-all.ps1)" -ForegroundColor Yellow
    exit 1
}

# ============================================
# 2. User Registration
# ============================================
Write-TestHeader "2. User Registration"
Write-TestCase "POST /api/v1/user/register"

$body = @{
    username = $TEST_USER
    password = $TEST_PASS
    email    = $TEST_EMAIL
    phone    = $TEST_PHONE
} | ConvertTo-Json

Write-Host "  Payload:" -ForegroundColor Gray
Write-Host "  $body" -ForegroundColor DarkGray

$result = Test-ApiCall -Method "POST" -Url "$BASE_URL/api/v1/user/register" -BodyParam $body

if (Test-Success $result) {
    Write-Result -Passed $true -Message "Registration successful"
    Write-Host "  User ID: $($result.Json.data.id)" -ForegroundColor Green
    Write-Host "  Username: $($result.Json.data.username)" -ForegroundColor Green
    Write-Host "  Email: $($result.Json.data.email)" -ForegroundColor Green
    $GLOBAL:USER_ID = $result.Json.data.id
} else {
    Write-Host "  Status: $($result.StatusCode)" -ForegroundColor Gray
    Write-Host "  Response: $($result.Raw)" -ForegroundColor Gray

    # Business error: user already exists (e.g. code=400 "username already exists")
    if ($result.Json.code -eq 400 -or $result.StatusCode -eq 400) {
        Write-Host ""
        Write-Host "  [INFO] User already exists (code=400), will try login instead." -ForegroundColor Yellow
        $GLOBAL:USER_ID = $null
    } else {
        Write-Result -Passed $false -Message "Registration failed"
        $GLOBAL:USER_ID = $null
    }
}

# ============================================
# 3. User Login
# ============================================
Write-TestHeader "3. User Login"
Write-TestCase "POST /api/v1/user/login"

$body = @{
    username = $TEST_USER
    password = $TEST_PASS
} | ConvertTo-Json

Write-Host "  Payload:" -ForegroundColor Gray
Write-Host "  $body" -ForegroundColor DarkGray

$result = Test-ApiCall -Method "POST" -Url "$BASE_URL/api/v1/user/login" -BodyParam $body

if (Test-Success $result) {
    Write-Result -Passed $true -Message "Login successful"
    Write-Host "  User ID: $($result.Json.data.userId)" -ForegroundColor Green
    Write-Host "  Username: $($result.Json.data.username)" -ForegroundColor Green
    Write-Host "  Token: $($result.Json.data.token)" -ForegroundColor DarkGreen
    $GLOBAL:AUTH_TOKEN = $result.Json.data.token
    if (-not $GLOBAL:USER_ID) {
        $GLOBAL:USER_ID = $result.Json.data.userId
    }
} else {
    Write-Result -Passed $false -Message "Login failed"
    Write-Host "  Status: $($result.StatusCode), Code: $($result.Json.code)" -ForegroundColor Gray
    Write-Host "  Response: $($result.Raw)" -ForegroundColor Gray
    $GLOBAL:AUTH_TOKEN = $null
}

# ============================================
# 4. Get User Info
# ============================================
Write-TestHeader "4. Get User Info"
Write-TestCase "GET /api/v1/user/info"

if ($GLOBAL:AUTH_TOKEN) {
    $headers = @{ "Authorization" = "Bearer $($GLOBAL:AUTH_TOKEN)" }
    $result = Test-ApiCall -Method "GET" -Url "$BASE_URL/api/v1/user/info" -HeadersParam $headers
} else {
    Write-Skip "No token available"
    $result = $null
}

if ($result -and (Test-Success $result)) {
    Write-Result -Passed $true -Message "Get user info successful"
    $d = $result.Json.data
    Write-Host "  ID:       $($d.id)" -ForegroundColor Gray
    Write-Host "  Username: $($d.username)" -ForegroundColor Gray
    Write-Host "  Email:    $($d.email)" -ForegroundColor Gray
    Write-Host "  Phone:    $($d.phone)" -ForegroundColor Gray
    Write-Host "  Status:   $($d.status)" -ForegroundColor Gray
} else {
    Write-Result -Passed $false -Message "Get user info failed"
    if ($result) {
        Write-Host "  Status: $($result.StatusCode)" -ForegroundColor Gray
        Write-Host "  Response: $($result.Raw)" -ForegroundColor Gray
    }
}

# ============================================
# 5. Update User Info
# ============================================
Write-TestHeader "5. Update User Info"
Write-TestCase "PUT /api/v1/user/info"

if ($GLOBAL:AUTH_TOKEN) {
    $headers = @{ "Authorization" = "Bearer $($GLOBAL:AUTH_TOKEN)" }
    $body = @{
        email = "newemail_$TIMESTAMP@example.com"
        phone = "13900139000"
    } | ConvertTo-Json

    Write-Host "  Payload:" -ForegroundColor Gray
    Write-Host "  $body" -ForegroundColor DarkGray

    $result = Test-ApiCall -Method "PUT" -Url "$BASE_URL/api/v1/user/info" -HeadersParam $headers -BodyParam $body
} else {
    Write-Skip "No token available"
    $result = $null
}

if ($result -and (Test-Success $result)) {
    Write-Result -Passed $true -Message "Update user info successful"
    Write-Host "  Message: $($result.Json.data.message)" -ForegroundColor Green

    # Verify
    $headers = @{ "Authorization" = "Bearer $($GLOBAL:AUTH_TOKEN)" }
    $verify = Test-ApiCall -Method "GET" -Url "$BASE_URL/api/v1/user/info" -HeadersParam $headers
    if ($verify -and (Test-Success $verify)) {
        $d = $verify.Json.data
        $expectedEmail = "newemail_$TIMESTAMP@example.com"
        $emailOk = $d.email -eq $expectedEmail
        $phoneOk = $d.phone -eq "13900139000"
        Write-Result -Passed $emailOk -Message "Email updated correctly"
        Write-Result -Passed $phoneOk -Message "Phone updated correctly"
    }
} else {
    Write-Result -Passed $false -Message "Update user info failed"
    if ($result) {
        Write-Host "  Status: $($result.StatusCode)" -ForegroundColor Gray
        Write-Host "  Response: $($result.Raw)" -ForegroundColor Gray
    }
}

# ============================================
# 6. Change Password
# ============================================
Write-TestHeader "6. Change Password"
Write-TestCase "POST /api/v1/user/password"

if ($GLOBAL:AUTH_TOKEN) {
    $headers = @{ "Authorization" = "Bearer $($GLOBAL:AUTH_TOKEN)" }
    $body = @{
        oldPassword = $TEST_PASS
        newPassword = "NewPass123"
    } | ConvertTo-Json

    Write-Host "  Payload:" -ForegroundColor Gray
    Write-Host "  oldPassword: $TEST_PASS, newPassword: NewPass123" -ForegroundColor DarkGray

    $result = Test-ApiCall -Method "POST" -Url "$BASE_URL/api/v1/user/password" -HeadersParam $headers -BodyParam $body
} else {
    Write-Skip "No token available"
    $result = $null
}

if ($result -and (Test-Success $result)) {
    Write-Result -Passed $true -Message "Change password successful"
    Write-Host "  Message: $($result.Json.data.message)" -ForegroundColor Green

    # Test login with new password
    Write-Host ""
    Write-Host "  Testing login with new password..." -ForegroundColor Cyan
    $body = @{
        username = $TEST_USER
        password = "NewPass123"
    } | ConvertTo-Json
    $newLogin = Test-ApiCall -Method "POST" -Url "$BASE_URL/api/v1/user/login" -BodyParam $body

    if (Test-Success $newLogin) {
        Write-Result -Passed $true -Message "Login with new password works"
        $GLOBAL:AUTH_TOKEN = $newLogin.Json.data.token
        $TEST_PASS = "NewPass123"
    } else {
        Write-Result -Passed $false -Message "Login with new password failed"
    }
} else {
    Write-Result -Passed $false -Message "Change password failed"
    if ($result) {
        Write-Host "  Status: $($result.StatusCode)" -ForegroundColor Gray
        Write-Host "  Response: $($result.Raw)" -ForegroundColor Gray
    }
}

# ============================================
# 7. Unauthorized Access Test
# ============================================
Write-TestHeader "7. Unauthorized Access Test"
Write-TestCase "GET /api/v1/user/info (no token)"

$result = Test-ApiCall -Method "GET" -Url "$BASE_URL/api/v1/user/info"

$rejected = ($result.StatusCode -eq 401) -or ($result.Json.code -eq 401)
Write-Result -Passed $rejected -Message "Correctly rejected unauthenticated request (Status: $($result.StatusCode))"

# ============================================
# 8. Invalid Credentials Test
# ============================================
Write-TestHeader "8. Invalid Credentials Test"
Write-TestCase "POST /api/v1/user/login (wrong password)"

$body = @{
    username = $TEST_USER
    password = "wrongpassword"
} | ConvertTo-Json

$result = Test-ApiCall -Method "POST" -Url "$BASE_URL/api/v1/user/login" -BodyParam $body

$rejected = ($result.StatusCode -eq 401) -or ($result.Json.code -eq 401)
Write-Result -Passed $rejected -Message "Correctly rejected invalid credentials (Status: $($result.StatusCode))"

# ============================================
# 9. Input Validation Test
# ============================================
Write-TestHeader "9. Input Validation Test"
Write-TestCase "POST /api/v1/user/register (username too short)"

$body = @{ username = "ab" } | ConvertTo-Json

Write-Host "  Payload: username='ab' (too short)" -ForegroundColor Gray

$result = Test-ApiCall -Method "POST" -Url "$BASE_URL/api/v1/user/register" -BodyParam $body

if ($result.StatusCode -eq 400 -or $result.Json.code -eq 400) {
    Write-Result -Passed $true -Message "Correctly rejected invalid input (Status: $($result.StatusCode))"
} else {
    Write-Result -Passed $false -Message "Should have rejected invalid input (Status: $($result.StatusCode))"
    Write-Host "  Response: $($result.Raw)" -ForegroundColor Gray
}

# ============================================
# SUMMARY
# ============================================
Write-Host ""
Write-Host ("=" * 60) -ForegroundColor Cyan
Write-Host "  Test Complete" -ForegroundColor Cyan
Write-Host ("=" * 60) -ForegroundColor Cyan
Write-Host ""
Write-Host "Test User: $TEST_USER" -ForegroundColor White
Write-Host "User ID:   $GLOBAL:USER_ID" -ForegroundColor White
if ($GLOBAL:AUTH_TOKEN) {
    $tokenPreview = $GLOBAL:AUTH_TOKEN.Substring(0, [Math]::Min(50, $GLOBAL:AUTH_TOKEN.Length))
    Write-Host "Token:     ${tokenPreview}..." -ForegroundColor White
}
Write-Host ""
Write-Host "All user service API tests finished!" -ForegroundColor Green
