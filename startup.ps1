$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $repoRoot

function Write-ArtBlock {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Title,

        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string[]]$Art,

        [ConsoleColor]$Color = "Cyan"
    )

    $width = 72
    Write-Host ""
    Write-Host ("+" + ("=" * $width) + "+") -ForegroundColor $Color
    Write-Host ("| " + $Title.PadRight($width - 2) + " |") -ForegroundColor $Color
    Write-Host ("+" + ("=" * $width) + "+") -ForegroundColor $Color
    foreach ($line in $Art) {
        Write-Host ("  " + $line) -ForegroundColor $Color
    }
    Write-Host ("+" + ("=" * $width) + "+") -ForegroundColor $Color
    Write-Host ""
}

function Write-LoadingFrame {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Label,

        [Parameter(Mandatory = $true)]
        [int]$Tick,

        [Parameter(Mandatory = $true)]
        [datetime]$StartedAt
    )

    $barWidth = 32
    $windowWidth = 8
    $maxStart = $barWidth - $windowWidth
    $position = $Tick % ($maxStart + 1)
    $barChars = New-Object char[] $barWidth

    for ($i = 0; $i -lt $barWidth; $i++) {
        $barChars[$i] = "."
    }
    for ($i = $position; $i -lt ($position + $windowWidth); $i++) {
        $barChars[$i] = "#"
    }

    $bar = -join $barChars
    $spinner = @("|", "/", "-", "\")[$Tick % 4]
    $elapsed = [int]((Get-Date) - $StartedAt).TotalSeconds
    Write-Host ("`r  {0} [{1}] {2} {3}s" -f $spinner, $bar, $Label, $elapsed) -NoNewline -ForegroundColor Cyan
}

function Invoke-AnimatedDocker {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Label,

        [Parameter(Mandatory = $true)]
        [string[]]$DockerArgs
    )

    $stdoutFile = New-TemporaryFile
    $stderrFile = New-TemporaryFile

    try {
        Write-Host ""
        Write-Host (":: " + $Label) -ForegroundColor Yellow
        Write-Host ("   docker " + ($DockerArgs -join " ")) -ForegroundColor DarkGray

        $process = Start-Process `
            -FilePath "docker" `
            -ArgumentList $DockerArgs `
            -WorkingDirectory $repoRoot `
            -RedirectStandardOutput $stdoutFile.FullName `
            -RedirectStandardError $stderrFile.FullName `
            -NoNewWindow `
            -PassThru

        $tick = 0
        $startedAt = Get-Date
        while (-not $process.HasExited) {
            Write-LoadingFrame -Label $Label -Tick $tick -StartedAt $startedAt
            Start-Sleep -Milliseconds 120
            $tick++
        }

        $process.WaitForExit()
        $process.Refresh()
        $exitCode = $process.ExitCode
        Write-Host ("`r  OK [{0}] {1} complete                    " -f ("#" * 32), $Label) -ForegroundColor Green

        $stdout = ""
        $stderr = ""
        if ((Get-Item -LiteralPath $stdoutFile.FullName).Length -gt 0) {
            $stdout = Get-Content -Raw -LiteralPath $stdoutFile.FullName
        }
        if ((Get-Item -LiteralPath $stderrFile.FullName).Length -gt 0) {
            $stderr = Get-Content -Raw -LiteralPath $stderrFile.FullName
        }

        if (-not [string]::IsNullOrWhiteSpace($stdout)) {
            Write-Host ""
            Write-Host "  output:" -ForegroundColor DarkGray
            Write-Host $stdout
        }

        if (-not [string]::IsNullOrWhiteSpace($stderr)) {
            Write-Host ""
            Write-Host "  diagnostics:" -ForegroundColor DarkGray
            Write-Host $stderr
        }

        if ($null -ne $exitCode -and $exitCode -ne 0) {
            throw "docker $($DockerArgs -join ' ') failed with exit code $exitCode"
        }
    }
    finally {
        Remove-Item -LiteralPath $stdoutFile.FullName -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $stderrFile.FullName -Force -ErrorAction SilentlyContinue
    }
}

try {
    Write-ArtBlock -Title "SAGE NEXUS BOOT SEQUENCE" -Color Cyan -Art @(
        '   _________                         _   _                         ',
        '  /  ______/                        | | | |                        ',
        '  | |______    __ _   __ _   ___    | |_| |   ___  __  __  _   _  ',
        '  \______  \  / _  | / _  | / _ \   |  _  |  / _ \ \ \/ / | | | | ',
        '   ______| | | (_| || (_| ||  __/   | | | | |  __/  >  <  | |_| | ',
        '  /________/  \__,_| \__, | \___|   |_| |_|  \___| /_/\_\  \__,_| ',
        '                      __/ |                                        ',
        '                     |___/                                         ',
        '',
        '  sequence: down -> build --no-cache -> up -d',
        '  display : animated loading bars'
    )

    Invoke-AnimatedDocker -Label "1/3 stopping compose stack" -DockerArgs @("compose", "down")
    Invoke-AnimatedDocker -Label "2/3 building fresh images" -DockerArgs @("compose", "build", "--no-cache")
    Invoke-AnimatedDocker -Label "3/3 starting stack detached" -DockerArgs @("compose", "up", "-d")

    Write-ArtBlock -Title "SAGE NEXUS STARTUP COMPLETE" -Color Green -Art @(
        '  [################################] 100%',
        '',
        '   ______ _    _  _____ _  __  __     __ ______          _    _ ',
        '  |  ____| |  | |/ ____| |/ /  \ \   / /|  ____|   /\   | |  | |',
        '  | |__  | |  | | |    | '' /    \ \_/ / | |__     /  \  | |__| |',
        '  |  __| | |  | | |    |  <      \   /  |  __|   / /\ \ |  __  |',
        '  | |    | |__| | |____| . \      | |   | |____ / ____ \| |  | |',
        '  |_|     \____/ \_____|_|\_\     |_|   |______/_/    \_\_|  |_|',
        '',
        '  status: FUCK YEAH',
        '',
        '  next checks:',
        '    docker compose ps',
        '    http://localhost:5174',
        '    http://localhost:8090/health'
    )
}
finally {
    Pop-Location
}
