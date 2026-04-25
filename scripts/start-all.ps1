# ============================================
#  Seckill System - One-Click Start (Windows)
# ============================================
# Prerequisites (must be started manually):
#   - Etcd       (port 2379)
#   - MySQL 8.0+ (port 3306, password root123456)
#   - Redis      (port 6379)
#   - RabbitMQ   (port 5672, guest/guest)
# ============================================

$ErrorActionPreference = "Stop"
$BASE_DIR = Split-Path -Parent $PSScriptRoot

function Write-ColorOutput {
    param($Message, $Color = "White")
    Write-Host $Message -ForegroundColor $Color
}

function Stop-AllServices {
    Write-ColorOutput "`n[STOP] Shutting down all services..." "Yellow"

    $names = @(
        "gateway",
        "order-service",
        "seckill-service",
        "product-service",
        "user-service"
    )

    foreach ($name in $names) {
        $proc = Get-Process -Name $name -ErrorAction SilentlyContinue
        if ($proc) {
            $proc.Kill()
            Write-ColorOutput "  [x] $name stopped" "DarkGray"
        }
        $proc2 = Get-Process -Name ($name + "-service") -ErrorAction SilentlyContinue
        if ($proc2) {
            $proc2.Kill()
            Write-ColorOutput "  [x] $name-service stopped" "DarkGray"
        }
    }

    Write-ColorOutput "[STOP] All services shut down`n" "Yellow"
}

function Test-Port {
    param($Port, $Name)
    $conn = Get-NetTCPConnection -LocalPort $Port -ErrorAction SilentlyContinue
    if ($conn) {
        Write-ColorOutput "  [OK] $Name ($Port) - already running" "Green"
        return $true
    } else {
        Write-ColorOutput "  [!] $Name ($Port) - NOT running" "Red"
        return $false
    }
}

function Test-BuildRequired {
    param(
        $Dir,
        $ExePath
    )

    if (-not (Test-Path $ExePath)) {
        return $true
    }

    $exeTime = (Get-Item $ExePath).LastWriteTimeUtc
    $commonDir = Join-Path $BASE_DIR "common"
    $rootBuildFiles = @(
        (Join-Path $BASE_DIR "go.work"),
        (Join-Path $BASE_DIR "go.work.sum")
    )

    $sourceFiles = @()
    $sourceFiles += Get-ChildItem -Path $Dir -Recurse -File -Include *.go,go.mod,go.sum,*.proto -ErrorAction SilentlyContinue
    if (Test-Path $commonDir) {
        $sourceFiles += Get-ChildItem -Path $commonDir -Recurse -File -Include *.go,go.mod,go.sum,*.proto -ErrorAction SilentlyContinue
    }
    foreach ($file in $rootBuildFiles) {
        if (Test-Path $file) {
            $sourceFiles += Get-Item $file
        }
    }

    foreach ($file in $sourceFiles) {
        if ($file.LastWriteTimeUtc -gt $exeTime) {
            return $true
        }
    }

    return $false
}

function Start-ServiceProcess {
    param(
        $Name,
        $Dir,
        $ExeName,
        $Port
    )

    $runtimeLogDir = Join-Path $BASE_DIR "logs\\runtime"
    if (-not (Test-Path $runtimeLogDir)) {
        New-Item -ItemType Directory -Path $runtimeLogDir -Force | Out-Null
    }

    $exePath = Join-Path $Dir $ExeName
    if (Test-BuildRequired -Dir $Dir -ExePath $exePath) {
        Write-ColorOutput "  [!] $Name binary missing or stale, compiling..." "DarkYellow"
        Push-Location $Dir
        try {
            $buildOutput = go build -o $ExeName . 2>&1
            if ($LASTEXITCODE -ne 0) {
                $errStr = if ($buildOutput) { $buildOutput -join " " } else { "unknown error" }
                Write-ColorOutput "  [x] $Name compile failed: $errStr" "Red"
                return $null
            }
        } catch {
            Write-ColorOutput "  [x] $Name compile failed: $_" "Red"
            return $null
        } finally {
            Pop-Location
        }
    }

    Write-ColorOutput "  [>] Starting $Name ..." "Cyan"
    $proc = Start-Process -FilePath $exePath `
        -WorkingDirectory $Dir `
        -NoNewWindow `
        -PassThru `
        -RedirectStandardOutput (Join-Path $runtimeLogDir "$Name-stdout.log") `
        -RedirectStandardError (Join-Path $runtimeLogDir "$Name-stderr.log")

    Start-Sleep -Milliseconds 800

    if ($proc.HasExited) {
        $stderrPath = Join-Path $runtimeLogDir "$Name-stderr.log"
        $err = Get-Content $stderrPath -ErrorAction SilentlyContinue | Select-Object -First 5
        $errStr = if ($err) { $err -join " " } else { "unknown error" }
        Write-ColorOutput "  [x] $Name failed to start: $errStr" "Red"
        return $null
    } else {
        Write-ColorOutput "  [OK] $Name started (PID: $($proc.Id))" "Green"
        return $proc
    }
}

# ============================================
# MAIN
# ============================================

Write-ColorOutput "`n============================================" "White"
Write-ColorOutput "    Seckill System - One-Click Start" "Cyan"
Write-ColorOutput "============================================`n" "White"

Stop-AllServices

Write-ColorOutput "[CHECK] Infrastructure...`n" "White"

$infraPorts = @(
    @{ Port = 2379; Name = "Etcd" },
    @{ Port = 3306; Name = "MySQL" },
    @{ Port = 6379; Name = "Redis" },
    @{ Port = 5672; Name = "RabbitMQ" }
)

$infraOk = $true
foreach ($infra in $infraPorts) {
    $ok = Test-Port -Port $infra.Port -Name $infra.Name
    if (-not $ok) { $infraOk = $false }
}

if (-not $infraOk) {
    Write-ColorOutput "`n[WARN] Some infrastructure not running. Please start:" "DarkYellow"
    Write-ColorOutput "   - Etcd      localhost:2379" "DarkYellow"
    Write-ColorOutput "   - MySQL     localhost:3306" "DarkYellow"
    Write-ColorOutput "   - Redis     localhost:6379" "DarkYellow"
    Write-ColorOutput "   - RabbitMQ  localhost:5672 guest/guest`n" "DarkYellow"
    $cont = Read-Host "Continue launching RPC services? (y/N)"
    if ($cont -ne "y" -and $cont -ne "Y") {
        Write-ColorOutput "Cancelled." "Gray"
        exit 0
    }
}

Write-ColorOutput "`n[START] Launching RPC services...`n" "White"

$services = @(
    @{ Name = "user-service";    Dir = Join-Path $BASE_DIR "user-service";    ExeName = "user-service.exe";    Port = 9081 },
    @{ Name = "product-service"; Dir = Join-Path $BASE_DIR "product-service"; ExeName = "product-service.exe"; Port = 9082 },
    @{ Name = "seckill-service"; Dir = Join-Path $BASE_DIR "seckill-service"; ExeName = "seckill-service.exe"; Port = 9083 },
    @{ Name = "order-service";  Dir = Join-Path $BASE_DIR "order-service";   ExeName = "order-service.exe";   Port = 9084 }
)

$runningProcesses = @()
foreach ($svc in $services) {
    $proc = Start-ServiceProcess -Name $svc.Name -Dir $svc.Dir -ExeName $svc.ExeName -Port $svc.Port
    if ($proc) { $runningProcesses += $proc }
    Start-Sleep -Milliseconds 300
}

Write-ColorOutput "`n[START] Launching Gateway...`n" "White"

$gatewayProc = Start-ServiceProcess -Name "gateway" -Dir (Join-Path $BASE_DIR "gateway") -ExeName "gateway.exe" -Port 8888
if ($gatewayProc) { $runningProcesses += $gatewayProc }

# ============================================
# SUMMARY
# ============================================

Write-ColorOutput "`n============================================" "White"
Write-ColorOutput "         All Services Started" "Green"
Write-ColorOutput "============================================" "White"

Write-ColorOutput "`nService Ports:" "White"
Write-ColorOutput "  Gateway   HTTP  -> http://localhost:8888" "Cyan"
Write-ColorOutput "  User      RPC   -> etcd: user.rpc" "Cyan"
Write-ColorOutput "  Product   RPC   -> etcd: product.rpc" "Cyan"
Write-ColorOutput "  Seckill   RPC   -> etcd: seckill.rpc" "Cyan"
Write-ColorOutput "  Order     RPC   -> etcd: order.rpc" "Cyan"
Write-ColorOutput "  Etcd            -> localhost:2379" "Cyan"
Write-ColorOutput "  Redis           -> localhost:6379" "Cyan"
Write-ColorOutput "  RabbitMQ       -> localhost:5672" "Cyan"
Write-ColorOutput "  MySQL          -> localhost:3306`n" "Cyan"

Write-ColorOutput "Running processes ($($runningProcesses.Count)):" "White"
foreach ($p in $runningProcesses) {
    Write-ColorOutput "  [$($p.Id)] $($p.ProcessName)" "Gray"
}

Write-ColorOutput "`nPress Ctrl+C to stop all services.`n" "DarkGray"

try {
    while ($true) {
        Start-Sleep -Seconds 3
        $exited = $runningProcesses | Where-Object { $_.HasExited }
        if ($exited) {
            Write-ColorOutput "`n[!] Process exited:" "DarkYellow"
            foreach ($e in $exited) {
                Write-ColorOutput "    $($e.ProcessName) (PID: $($e.Id)) exited with code $($e.ExitCode)" "Red"
                $runningProcesses = @($runningProcesses | Where-Object { $_ -ne $e })
            }
            if ($runningProcesses.Count -eq 0) {
                Write-ColorOutput "`nAll services stopped." "Yellow"
                break
            }
        }
    }
} finally {
    Stop-AllServices
}
