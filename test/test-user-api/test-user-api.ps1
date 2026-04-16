# ============================================================
# User Management API Test Script
# Dependency: Gateway must be running at http://localhost:8888
# Usage: .\test-user-api.ps1
# ============================================================
param(
    [switch]$SkipCleanup,
    [switch]$Verbose
)

$ErrorActionPreference = "Continue"
$BASE_URL = "http://localhost:8888"
$GREEN = "Green"
$RED = "Red"
$YELLOW = "Yellow"
$CYAN = "Cyan"

$script:TestsPassed = 0
$script:TestsFailed = 0
$script:AccessToken = ""
$script:RefreshToken = ""
$script:UserId = 0
$script:TestUsername = ""
$script:TestEmail = ""
$script:TestPassword = "Test@123456"

function Write-TestHeader {
    param([string]$Title)
    Write-Host ""
    Write-Host ("=" * 70) -ForegroundColor Cyan
    Write-Host "  $Title" -ForegroundColor Cyan
    Write-Host ("=" * 70) -ForegroundColor Cyan
}

function Write-TestStep {
    param([string]$Message)
    Write-Host "[TEST] $Message" -ForegroundColor White
}

function Write-Passed {
    param([string]$Message)
    $script:TestsPassed = $script:TestsPassed + 1
    Write-Host "[PASS] $Message" -ForegroundColor $GREEN
}

function Write-Failed {
    param([string]$Message)
    $script:TestsFailed = $script:TestsFailed + 1
    Write-Host "[FAIL] $Message" -ForegroundColor $RED
}

function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Cyan
}

function Write-Warn {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor $YELLOW
}

function Invoke-ApiRequest {
    param(
        [string]$Method = "GET",
        [string]$Endpoint,
        [hashtable]$Headers = @{},
        [object]$Body = $null,
        [string]$Description = ""
    )

    $url = "$BASE_URL$Endpoint"
    # Build a merged headers hashtable from scratch (avoid foreach-modify on input)
    $allKeys = @($Headers.Keys)
    $headers = @{
        "Content-Type" = "application/json"
        "User-Agent"   = "PowerShell-UserTest/1.0"
    }
    foreach ($key in $allKeys) {
        $headers[$key] = $Headers[$key]
    }

    $params = @{
        Uri        = $url
        Method     = $Method
        Headers    = $headers
        TimeoutSec = 15
    }

    if ($Body -ne $null) {
        $bodyJson = ConvertTo-Json -InputObject $Body -Compress
        $params.Body = $bodyJson
    }

    if ($Verbose) {
        Write-Host "       Request: $Method $url" -ForegroundColor DarkGray
        if ($Body -ne $null) {
            Write-Host "       Body: $bodyJson" -ForegroundColor DarkGray
        }
    }

    try {
        $resp = Invoke-RestMethod @params
        if ($Verbose) {
            $respJson = ConvertTo-Json -InputObject $resp -Compress
            Write-Host "       Response Code: $($resp.code)" -ForegroundColor DarkGray
            Write-Host "       Response: $respJson" -ForegroundColor DarkGray
        }
        return $resp
    }
    catch {
        $statusCode = 0
        $errMsg = $_.Exception.Message
        if ($_.Exception.Response) {
            $statusCode = [int]$_.Exception.Response.StatusCode
        }
        if ($Verbose) {
            Write-Host "       Error: $errMsg (HTTP $statusCode)" -ForegroundColor DarkGray
        }
        return @{
            code    = $statusCode
            message = $errMsg
            data    = $null
        }
    }
}

function Test-Health {
    Write-TestHeader "0. Health Check"

    $resp = Invoke-ApiRequest -Method "GET" -Endpoint "/health" -Description "Health check"
    if ($resp.code -eq 0 -or $resp.code -eq 200) {
        Write-Passed "Gateway health check passed"
        return $true
    }
    else {
        Write-Failed "Gateway not responding (http://localhost:8888)"
        return $false
    }
}

function Test-UserRegister {
    Write-TestHeader "1. User Registration"

    $script:TestUsername = "testuser_$(Get-Random -Maximum 999999)"
    $script:TestEmail = "$TestUsername@seckill.test"
    $body = @{
        username = $TestUsername
        password = $TestPassword
        email    = $TestEmail
        phone    = "13800138000"
    }

    Write-Info "Registering: $TestUsername (email: $TestEmail)"

    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/register" `
        -Headers @{} -Body $body -Description "User registration"

    if ($resp.code -eq 0) {
        $script:UserId = $resp.data.id
        Write-Info "Registered successfully, UserID: $UserId"
        Write-Passed "User registration passed"
        return $true
    }
    else {
        Write-Failed "User registration failed: $($resp.message)"
        return $false
    }
}

function Test-RegisterValidation {
    Write-TestHeader "1.1 Registration Validation"

    # Short username (min=3)
    $v1Body = @{ username = "ab"; password = "Test@123"; email = "a@b.com" }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/register" `
        -Headers @{} -Body $v1Body -Description "Short username"
    if ($resp.code -ne 0) { Write-Passed "Short username - rejected" } else { Write-Failed "Short username - not rejected" }

    # Short password (min=6)
    $v2Body = @{ username = "testuser123"; password = "12345"; email = "a@b.com" }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/register" `
        -Headers @{} -Body $v2Body -Description "Short password"
    if ($resp.code -ne 0) { Write-Passed "Short password - rejected" } else { Write-Failed "Short password - not rejected" }

    # Invalid email
    $v3Body = @{ username = "testuser123"; password = "Test@123"; email = "notanemail" }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/register" `
        -Headers @{} -Body $v3Body -Description "Invalid email"
    if ($resp.code -ne 0) { Write-Passed "Invalid email - rejected" } else { Write-Failed "Invalid email - not rejected" }

    # Missing username field
    $v4Body = @{ password = "Test@123"; email = "a@b.com" }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/register" `
        -Headers @{} -Body $v4Body -Description "Missing username field"
    if ($resp.code -ne 0) { Write-Passed "Missing username - rejected" } else { Write-Failed "Missing username - not rejected" }
}

function Test-UserLogin {
    Write-TestHeader "2. User Login"

    if ($TestUsername -eq "") {
        $script:TestUsername = "testuser_$(Get-Random -Maximum 999999)"
        $script:TestEmail = "$TestUsername@seckill.test"

        $regBody = @{
            username = $TestUsername
            password = $TestPassword
            email    = $TestEmail
            phone    = "13800138001"
        }
        $regResp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/register" `
            -Headers @{} -Body $regBody -Description "Pre-register for login test"
        if ($regResp.code -ne 0) {
            Write-Failed "Pre-registration failed, skipping login test"
            return $false
        }
        $script:UserId = $regResp.data.id
        Write-Info "Pre-registered user ID: $UserId"
    }

    Write-Info "Logging in with username: $TestUsername"
    $body = @{
        username = $TestUsername
        password = $TestPassword
    }

    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/login" `
        -Headers @{} -Body $body -Description "User login"

    if ($resp.code -eq 0 -and $resp.data.accessToken) {
        $script:AccessToken = $resp.data.accessToken
        $script:RefreshToken = $resp.data.refreshToken
        $script:UserId = $resp.data.userId
        $tokenLen = $AccessToken.Length
        if ($tokenLen -gt 30) { $tokenPreview = $AccessToken.Substring(0, 30) } else { $tokenPreview = $AccessToken }
        $refreshLen = $RefreshToken.Length
        if ($refreshLen -gt 30) { $refreshPreview = $RefreshToken.Substring(0, 30) } else { $refreshPreview = $RefreshToken }
        Write-Info "Login success, AccessToken: $tokenPreview..."
        Write-Info "RefreshToken: $refreshPreview..."
        $ts = [System.DateTimeOffset]::FromUnixTimeSeconds($resp.data.accessExpireAt)
        Write-Info ("Token expires at: " + $ts.ToString("yyyy-MM-dd HH:mm:ss"))
        Write-Passed "User login passed"
        return $true
    }
    else {
        Write-Failed "User login failed: $($resp.message)"
        return $false
    }
}

function Test-LoginValidation {
    Write-TestHeader "2.1 Login Validation"

    $wrongPassBody = @{ username = $TestUsername; password = "WrongPassword123" }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/login" `
        -Headers @{} -Body $wrongPassBody -Description "Wrong password"
    if ($resp.code -ne 0) { Write-Passed "Wrong password - rejected" } else { Write-Failed "Wrong password - not rejected" }

    $noUserBody = @{ username = "nonexistent_$(Get-Random)"; password = "AnyPass123" }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/login" `
        -Headers @{} -Body $noUserBody -Description "Non-existent user"
    if ($resp.code -ne 0) { Write-Passed "Non-existent user - rejected" } else { Write-Failed "Non-existent user - not rejected" }

    $noPassBody = @{ username = $TestUsername }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/login" `
        -Headers @{} -Body $noPassBody -Description "Missing password field"
    if ($resp.code -ne 0) { Write-Passed "Missing password - rejected" } else { Write-Failed "Missing password - not rejected" }
}

function Test-GetUserInfo {
    Write-TestHeader "3. Get User Info"

    $headers = @{ "Authorization" = "Bearer $AccessToken" }
    $resp = Invoke-ApiRequest -Method "GET" -Endpoint "/api/v1/user/info" `
        -Headers $headers -Description "Get user info"

    if ($resp.code -eq 0) {
        $uid = $resp.data.id
        $uname = $resp.data.username
        $email = $resp.data.email
        $phone = $resp.data.phone
        $status = $resp.data.status
        Write-Info "UserID:   $uid"
        Write-Info "Username: $uname"
        Write-Info "Email:    $email"
        Write-Info "Phone:    $phone"
        Write-Info "Status:   $status (1=active, 0=disabled)"
        if ($resp.data.createdAt) {
            $ts = [System.DateTimeOffset]::FromUnixTimeSeconds($resp.data.createdAt)
            Write-Info ("Created:  " + $ts.ToString("yyyy-MM-dd HH:mm:ss"))
        }
        Write-Passed "Get user info passed"
        return $true
    }
    else {
        Write-Failed "Get user info failed: $($resp.message)"
        return $false
    }
}

function Test-GetUserInfoUnauthorized {
    Write-TestHeader "3.1 Unauthorized Access Protection"

    # No token
    $resp = Invoke-ApiRequest -Method "GET" -Endpoint "/api/v1/user/info" `
        -Headers @{} -Description "No token"
    if ($resp.code -eq 401) { Write-Passed "No token - rejected (401)" } else { Write-Failed "No token - expected 401, got $($resp.code)" }

    # Invalid token
    $badHeaders = @{ "Authorization" = "Bearer invalid.token.here" }
    $resp = Invoke-ApiRequest -Method "GET" -Endpoint "/api/v1/user/info" `
        -Headers $badHeaders -Description "Invalid token"
    if ($resp.code -eq 401) { Write-Passed "Invalid token - rejected (401)" } else { Write-Failed "Invalid token - expected 401, got $($resp.code)" }

    # Wrong format
    $badHeaders2 = @{ "Authorization" = "NotBearer abc123" }
    $resp = Invoke-ApiRequest -Method "GET" -Endpoint "/api/v1/user/info" `
        -Headers $badHeaders2 -Description "Wrong token format"
    if ($resp.code -eq 401) { Write-Passed "Wrong token format - rejected (401)" } else { Write-Failed "Wrong token format - expected 401, got $($resp.code)" }
}

function Test-UpdateUserInfo {
    Write-TestHeader "4. Update User Info"

    $headers = @{ "Authorization" = "Bearer $AccessToken" }
    $newEmail = "newemail_$(Get-Random -Maximum 9999)@seckill.test"
    $newPhone = "139$(Get-Random -Minimum 10000000 -Maximum 99999999)"
    $body = @{
        email = $newEmail
        phone = $newPhone
    }

    $resp = Invoke-ApiRequest -Method "PUT" -Endpoint "/api/v1/user/info" `
        -Headers $headers -Body $body -Description "Update user info"

    if ($resp.code -eq 0) {
        Write-Info "Updated email: $newEmail"
        Write-Info "Updated phone: $newPhone"
        Write-Passed "Update user info passed"

        Write-Info "Verifying update..."
        $verifyResp = Invoke-ApiRequest -Method "GET" -Endpoint "/api/v1/user/info" `
            -Headers $headers -Description "Verify update"
        if ($verifyResp.code -eq 0 -and $verifyResp.data.email -eq $newEmail) {
            Write-Passed "Data consistency verified"
        }
        else {
            Write-Failed "Data consistency check failed"
        }
        return $true
    }
    else {
        Write-Failed "Update user info failed: $($resp.message)"
        return $false
    }
}

function Test-ChangePassword {
    Write-TestHeader "5. Change Password"

    $headers = @{ "Authorization" = "Bearer $AccessToken" }
    $newPassword = "NewPass@$(Get-Random -Minimum 1000 -Maximum 9999)"

    # Wrong old password
    Write-Info "Testing with wrong old password..."
    $wrongBody = @{
        oldPassword = "DefinitelyWrongPassword123"
        newPassword = $newPassword
    }
    $wrongResp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/password" `
        -Headers $headers -Body $wrongBody -Description "Wrong old password"
    if ($wrongResp.code -ne 0) { Write-Passed "Wrong old password - rejected" } else { Write-Failed "Wrong old password - not rejected" }

    # Correct old password
    Write-Info "Testing with correct old password..."
    $body = @{
        oldPassword = $TestPassword
        newPassword = $newPassword
    }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/password" `
        -Headers $headers -Body $body -Description "Change password"

    if ($resp.code -eq 0) {
        Write-Info "Password changed, new password: $newPassword"
        Write-Passed "Change password passed"

        # Verify with new password
        Write-Info "Verifying new password with login..."
        $loginBody = @{
            username = $TestUsername
            password = $newPassword
        }
        $loginResp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/login" `
            -Headers @{} -Body $loginBody -Description "Login with new password"

        if ($loginResp.code -eq 0 -and $loginResp.data.accessToken) {
            $script:AccessToken = $loginResp.data.accessToken
            $script:RefreshToken = $loginResp.data.refreshToken
            Write-Passed "New password login verified"
        }
        else {
            Write-Failed "New password login failed: $($loginResp.message)"
        }

        # Restore for remaining tests
        $script:TestPassword = $newPassword
        return $true
    }
    else {
        Write-Failed "Change password failed: $($resp.message)"
        return $false
    }
}

function Test-RefreshToken {
    Write-TestHeader "6. Refresh Token"

    Write-Info "Refreshing with RefreshToken..."
    $body = @{ refreshToken = $RefreshToken }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/refresh" `
        -Headers @{} -Body $body -Description "Refresh token"

    if ($resp.code -eq 0) {
        $script:AccessToken = $resp.data.accessToken
        $script:RefreshToken = $resp.data.refreshToken
        $accessLen = $AccessToken.Length
        if ($accessLen -gt 30) { $accessPreview = $AccessToken.Substring(0, 30) } else { $accessPreview = $AccessToken }
        $refreshLen2 = $RefreshToken.Length
        if ($refreshLen2 -gt 30) { $refreshPreview = $RefreshToken.Substring(0, 30) } else { $refreshPreview = $RefreshToken }
        Write-Info "New AccessToken: $accessPreview..."
        Write-Info "New RefreshToken: $refreshPreview..."
        $ts2 = [System.DateTimeOffset]::FromUnixTimeSeconds($resp.data.accessExpireAt)
        Write-Info ("Expires at: " + $ts2.ToString("yyyy-MM-dd HH:mm:ss"))
        Write-Passed "Token refresh passed"

        # Verify new token
        $verifyHeaders = @{ "Authorization" = "Bearer $AccessToken" }
        $verifyResp = Invoke-ApiRequest -Method "GET" -Endpoint "/api/v1/user/info" `
            -Headers $verifyHeaders -Description "Verify new token"
        if ($verifyResp.code -eq 0) {
            Write-Passed "New token verified, UserID: $($verifyResp.data.id)"
        }
        else {
            Write-Failed "New token verification failed"
        }
        return $true
    }
    else {
        Write-Failed "Token refresh failed: $($resp.message)"
        return $false
    }
}

function Test-RefreshTokenInvalid {
    Write-TestHeader "6.1 Refresh Token Validation"

    $badBody1 = @{ refreshToken = "invalid.token.refresh" }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/refresh" `
        -Headers @{} -Body $badBody1 -Description "Invalid refresh token"
    if ($resp.code -ne 0) { Write-Passed "Invalid refresh token - rejected" } else { Write-Failed "Invalid refresh token - not rejected" }

    $badBody2 = @{ refreshToken = "" }
    $resp = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/refresh" `
        -Headers @{} -Body $badBody2 -Description "Empty refresh token"
    if ($resp.code -ne 0) { Write-Passed "Empty refresh token - rejected" } else { Write-Failed "Empty refresh token - not rejected" }
}

function Test-Idempotency {
    Write-TestHeader "7. Idempotency Test"

    $body = @{
        username = $TestUsername
        password = "Test@123456"
        email    = "duplicate@seckill.test"
        phone    = "13800138000"
    }

    Write-Info "Attempting duplicate registration..."
    $resp1 = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/register" `
        -Headers @{} -Body $body -Description "First duplicate registration"
    $resp2 = Invoke-ApiRequest -Method "POST" -Endpoint "/api/v1/user/register" `
        -Headers @{} -Body $body -Description "Second duplicate registration"

    if ($resp1.code -ne 0 -and $resp2.code -ne 0) {
        Write-Passed "Duplicate registration rejected"
    }
    else {
        Write-Warn "Duplicate registration responses: 1st=$($resp1.code), 2nd=$($resp2.code)"
    }
}

function Show-TestSummary {
    Write-TestHeader "Test Summary"

    $total = $script:TestsPassed + $script:TestsFailed
    if ($total -gt 0) {
        $passRate = [math]::Round($script:TestsPassed / $total * 100, 1)
    }
    else {
        $passRate = 0
    }

    Write-Host "  Total tests: $total" -ForegroundColor White
    Write-Host "  Passed:      $script:TestsPassed" -ForegroundColor $GREEN
    Write-Host "  Failed:      $script:TestsFailed" -ForegroundColor $RED

    if ($passRate -ge 80) {
        $summaryColor = $GREEN
    }
    elseif ($passRate -ge 60) {
        $summaryColor = $YELLOW
    }
    else {
        $summaryColor = $RED
    }
    Write-Host ("  Pass rate:   " + $passRate.ToString() + "%") -ForegroundColor $summaryColor

    Write-Host ""
    if ($script:TestsFailed -eq 0) {
        Write-Host "  All tests passed!" -ForegroundColor $GREEN
    }
    else {
        Write-Host ("  " + $script:TestsFailed + " test(s) failed. Please check service status.") -ForegroundColor $RED
    }

    Write-Host ""
    Write-Host "  Test account:" -ForegroundColor Cyan
    Write-Host ("    Username:  " + $script:TestUsername) -ForegroundColor White
    Write-Host ("    Password:  " + $script:TestPassword) -ForegroundColor White
    Write-Host ("    Email:     " + $script:TestEmail) -ForegroundColor White
    Write-Host ("    UserID:    " + $script:UserId) -ForegroundColor White
    Write-Host ""
}

# ============================================================
# Main
# ============================================================

Write-Host ""
Write-Host ("=" * 70) -ForegroundColor Magenta
Write-Host "  User Management API Test" -ForegroundColor Magenta
Write-Host "  Gateway: $BASE_URL" -ForegroundColor Magenta
Write-Host ("=" * 70) -ForegroundColor Magenta

# 0. Health check
$healthOk = Test-Health
if (-not $healthOk) {
    Write-Host ""
    Write-Host "Gateway not available. Please start services first:" -ForegroundColor Red
    Write-Host "  cd gateway && go run gateway.go -f etc/gateway.yaml" -ForegroundColor Yellow
    Write-Host "  cd user-service && go run ." -ForegroundColor Yellow
    exit 1
}

# 1. User registration
$regOk = Test-UserRegister
if (-not $regOk) {
    Write-Warn "Registration failed, skipping some tests..."
}

Test-RegisterValidation

# 2. User login
$loginOk = Test-UserLogin
if (-not $loginOk) {
    Write-Warn "Login failed, skipping auth-related tests..."
}

if ($loginOk) {
    Test-LoginValidation

    if ($script:AccessToken -ne "") {
        Test-GetUserInfo
        Test-GetUserInfoUnauthorized
        Test-UpdateUserInfo
        Test-ChangePassword
        Test-RefreshToken
        Test-RefreshTokenInvalid
    }
}

Test-Idempotency

# Cleanup
if (-not $SkipCleanup -and $script:UserId -gt 0) {
    Write-TestHeader "Cleanup"
    Write-Info "Test user $script:TestUsername (ID: $script:UserId) kept in database"
    Write-Info "To clean up, manually delete the user record"
}

Show-TestSummary

if ($script:TestsFailed -gt 0) {
    exit 1
}
