$ErrorActionPreference = "Continue"
$BASE = "http://localhost:8888"

# Login
$loginBody = '{"username":"testuser_236024","password":"Test@123456"}'
$r = Invoke-RestMethod -Uri "$BASE/api/v1/user/login" -Method POST `
    -Headers @{"Content-Type"="application/json"} `
    -Body $loginBody -TimeoutSec 10
$token = $r.data.accessToken
$userId = $r.data.userId
Write-Host "Token length: $($token.Length)"
Write-Host "UserId: $userId"

# Test 1: Direct Invoke-RestMethod with Authorization header (no function)
$headers1 = @{
    "Content-Type" = "application/json"
    "User-Agent" = "PowerShell-UserTest/1.0"
    "Authorization" = "Bearer $token"
}
Write-Host ""
Write-Host "Test 1: Direct hashtable with Authorization"
Write-Host "Authorization length: $($headers1['Authorization'].Length)"
$resp1 = Invoke-RestMethod -Uri "$BASE/api/v1/user/info" -Headers $headers1 -TimeoutSec 10
Write-Host "Result: code=$($resp1.code) message=$($resp1.message)"

# Test 2: Pass through function with @($Headers.Keys)
function TestViaFunc {
    param(
        [string]$Method = "GET",
        [string]$Endpoint,
        [hashtable]$ExtHeaders = @{}
    )

    $url = "$BASE$Endpoint"
    $headers = @{
        "Content-Type" = "application/json"
        "User-Agent"   = "PowerShell-UserTest/1.0"
    }
    foreach ($key in @($ExtHeaders.Keys)) {
        $headers[$key] = $ExtHeaders[$key]
    }

    Write-Host "  Function headers count: $($headers.Count)"
    Write-Host "  Function has Authorization: $($headers.ContainsKey('Authorization'))"
    if ($headers.ContainsKey('Authorization')) {
        Write-Host "  Authorization value length: $($headers['Authorization'].Length)"
    }

    $params = @{
        Uri        = $url
        Method     = $Method
        Headers    = $headers
        TimeoutSec = 10
    }

    $resp = Invoke-RestMethod @params
    Write-Host "  Response: code=$($resp.code) message=$($resp.message)"
    return $resp
}

Write-Host ""
Write-Host "Test 2: Via function with @()"
$authHeaders = @{ "Authorization" = "Bearer $token" }
Write-Host "  authHeaders Authorization length: $($authHeaders['Authorization'].Length)"
$result = TestViaFunc -Method "GET" -Endpoint "/api/v1/user/info" -ExtHeaders $authHeaders

# Test 3: Without @()
function TestViaFunc2 {
    param(
        [string]$Method = "GET",
        [string]$Endpoint,
        [hashtable]$ExtHeaders = @{}
    )

    $url = "$BASE$Endpoint"
    $headers = @{
        "Content-Type" = "application/json"
        "User-Agent"   = "PowerShell-UserTest/1.0"
    }
    foreach ($key in $ExtHeaders.Keys) {
        $headers[$key] = $ExtHeaders[$key]
    }

    Write-Host "  Function headers count: $($headers.Count)"
    Write-Host "  Function has Authorization: $($headers.ContainsKey('Authorization'))"

    $params = @{
        Uri        = $url
        Method     = $Method
        Headers    = $headers
        TimeoutSec = 10
    }

    $resp = Invoke-RestMethod @params
    Write-Host "  Response: code=$($resp.code) message=$($resp.message)"
    return $resp
}

Write-Host ""
Write-Host "Test 3: Via function without @()"
$authHeaders2 = @{ "Authorization" = "Bearer $token" }
Write-Host "  authHeaders Authorization length: $($authHeaders2['Authorization'].Length)"
$result2 = TestViaFunc2 -Method "GET" -Endpoint "/api/v1/user/info" -ExtHeaders $authHeaders2
