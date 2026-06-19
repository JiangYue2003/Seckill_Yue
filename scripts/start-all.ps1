# ============================================
#  Seckill System - One-Click Start (Windows)
# ============================================
# Prerequisites (must be started manually):
#   - Etcd       (port 2379)
#   - MySQL 8.0+ (port 3306, password root123456)
#   - Redis      (port 6379)
#   - RabbitMQ   (port 5672)
# Default launch profile:
#   - seckill-service replicas = 2
#   - order-service replicas   = 2
# Override with:
#   .\scripts\start-all.ps1 -SeckillReplicas 1 -OrderReplicas 1
# ============================================

param(
    [int]$SeckillReplicas = 2,
    [int]$OrderReplicas = 2,
    [switch]$SkipMain
)

$ErrorActionPreference = "Stop"
$BASE_DIR = Split-Path -Parent $PSScriptRoot

function Write-ColorOutput {
    param($Message, $Color = "White")
    Write-Host $Message -ForegroundColor $Color
}

function Get-ReplicaValue {
    param(
        [int]$Value,
        [int]$DefaultValue
    )

    if ($Value -le 0) {
        return $DefaultValue
    }
    return $Value
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

function New-ServiceInstance {
    param(
        [string]$BaseName,
        [string]$Dir,
        [string]$ExeName,
        [int]$BasePort,
        [int]$BaseMetricsPort,
        [int]$InstanceNumber
    )

    $instanceName = $BaseName
    $port = $BasePort
    $metricsPort = $BaseMetricsPort
    $args = @()

    if ($InstanceNumber -gt 1) {
        $instanceName = "$BaseName-$InstanceNumber"
        $port = Get-ReplicaPort -BasePort $BasePort -InstanceNumber $InstanceNumber
        $metricsPort = Get-ReplicaPort -BasePort $BaseMetricsPort -InstanceNumber $InstanceNumber
        $args += "--port=$port"
        $args += "--metrics-port=$metricsPort"
    }

    [PSCustomObject]@{
        Name        = $instanceName
        BaseName    = $BaseName
        Dir         = $Dir
        ExeName     = $ExeName
        Port        = $port
        MetricsPort = $metricsPort
        Args        = $args
    }
}

function Get-ReplicaPort {
    param(
        [int]$BasePort,
        [int]$InstanceNumber
    )

    if ($InstanceNumber -le 1) {
        return $BasePort
    }

    return $BasePort + (10000 * ($InstanceNumber - 1))
}

function Add-ServiceReplicas {
    param(
        [System.Collections.ArrayList]$Plan,
        [string]$BaseName,
        [string]$Dir,
        [string]$ExeName,
        [int]$BasePort,
        [int]$BaseMetricsPort,
        [int]$Replicas
    )

    for ($instance = 1; $instance -le $Replicas; $instance++) {
        [void]$Plan.Add((New-ServiceInstance `
            -BaseName $BaseName `
            -Dir $Dir `
            -ExeName $ExeName `
            -BasePort $BasePort `
            -BaseMetricsPort $BaseMetricsPort `
            -InstanceNumber $instance))
    }
}

function Get-ServiceLaunchPlan {
    param(
        [int]$SeckillReplicas = 2,
        [int]$OrderReplicas = 2
    )

    $plan = [System.Collections.ArrayList]::new()

    Add-ServiceReplicas -Plan $plan -BaseName "user-service" -Dir (Join-Path $BASE_DIR "user-service") -ExeName "user-service.exe" -BasePort 9081 -BaseMetricsPort 9181 -Replicas 1
    Add-ServiceReplicas -Plan $plan -BaseName "product-service" -Dir (Join-Path $BASE_DIR "product-service") -ExeName "product-service.exe" -BasePort 9082 -BaseMetricsPort 9182 -Replicas 1
    Add-ServiceReplicas -Plan $plan -BaseName "seckill-service" -Dir (Join-Path $BASE_DIR "seckill-service") -ExeName "seckill-service.exe" -BasePort 9083 -BaseMetricsPort 9183 -Replicas (Get-ReplicaValue -Value $SeckillReplicas -DefaultValue 1)
    Add-ServiceReplicas -Plan $plan -BaseName "order-service" -Dir (Join-Path $BASE_DIR "order-service") -ExeName "order-service.exe" -BasePort 9084 -BaseMetricsPort 9184 -Replicas (Get-ReplicaValue -Value $OrderReplicas -DefaultValue 1)

    return $plan
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
        $Port,
        [string[]]$ArgumentList = @()
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
        -ArgumentList $ArgumentList `
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

function Invoke-Main {
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
        Write-ColorOutput "   - RabbitMQ  localhost:5672`n" "DarkYellow"
        $cont = Read-Host "Continue launching RPC services? (y/N)"
        if ($cont -ne "y" -and $cont -ne "Y") {
            Write-ColorOutput "Cancelled." "Gray"
            exit 0
        }
    }

    Write-ColorOutput "`n[START] Launching RPC services...`n" "White"

    $services = Get-ServiceLaunchPlan -SeckillReplicas $SeckillReplicas -OrderReplicas $OrderReplicas

    $runningProcesses = @()
    foreach ($svc in $services) {
        $proc = Start-ServiceProcess -Name $svc.Name -Dir $svc.Dir -ExeName $svc.ExeName -Port $svc.Port -ArgumentList $svc.Args
        if ($proc) { $runningProcesses += $proc }
        Start-Sleep -Milliseconds 300
    }

    Write-ColorOutput "`n[START] Launching Gateway...`n" "White"

    $gatewayProc = Start-ServiceProcess -Name "gateway" -Dir (Join-Path $BASE_DIR "gateway") -ExeName "gateway.exe" -Port 8888
    if ($gatewayProc) { $runningProcesses += $gatewayProc }

    Write-ColorOutput "`n============================================" "White"
    Write-ColorOutput "         All Services Started" "Green"
    Write-ColorOutput "============================================" "White"

    Write-ColorOutput "`nService Ports:" "White"
    Write-ColorOutput "  Gateway   HTTP  -> http://localhost:8888" "Cyan"
    foreach ($svc in $services) {
        Write-ColorOutput ("  {0,-17} -> {1}" -f $svc.Name, "127.0.0.1:$($svc.Port)") "Cyan"
    }
    Write-ColorOutput "  Etcd             -> localhost:2379" "Cyan"
    Write-ColorOutput "  Redis            -> localhost:6379" "Cyan"
    Write-ColorOutput "  RabbitMQ         -> localhost:5672" "Cyan"
    Write-ColorOutput "  MySQL            -> localhost:3306`n" "Cyan"

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
}

if (-not $SkipMain) {
    Invoke-Main
}
