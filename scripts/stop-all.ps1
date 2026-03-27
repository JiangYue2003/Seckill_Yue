#  Seckill System - One-Click Stop (Windows)

$ErrorActionPreference = "SilentlyContinue"

$serviceNames = @(
    "gateway",
    "order-service",
    "seckill-service",
    "product-service",
    "user-service"
)

Write-Host "Stopping all services..." -ForegroundColor Yellow

$found = $false
foreach ($name in $serviceNames) {
    $proc = Get-Process -Name $name -ErrorAction SilentlyContinue
    if ($proc) {
        $proc.Kill()
        Write-Host "  [x] $name stopped" -ForegroundColor Green
        $found = $true
    }
    $proc2 = Get-Process -Name ($name + "-service") -ErrorAction SilentlyContinue
    if ($proc2) {
        $proc2.Kill()
        Write-Host "  [x] $name-service stopped" -ForegroundColor Green
        $found = $true
    }
}

if (-not $found) {
    Write-Host "No running services found." -ForegroundColor Gray
} else {
    Write-Host "`nAll services stopped." -ForegroundColor Yellow
}
