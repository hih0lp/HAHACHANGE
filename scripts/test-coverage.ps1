param(
    [string]$OutputDir = (Join-Path ([IO.Path]::GetTempPath()) 'bobby-test-coverage'),
    [switch]$GoOnly
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$OutputDir = [IO.Path]::GetFullPath($OutputDir)
$tag = [guid]::NewGuid().ToString('N').Substring(0, 10)
$pg = "bobby-coverage-pg-$tag"
$redis = "bobby-coverage-redis-$tag"
$previousDatabase = $env:TEST_DATABASE_URL
$previousRedis = $env:TEST_REDIS_ADDR

function Invoke-Checked([string]$Command, [string[]]$Arguments) {
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Command failed with exit code $LASTEXITCODE" }
}

function Wait-Service([string]$Container, [string[]]$Probe) {
    for ($attempt = 0; $attempt -lt 40; $attempt++) {
        docker exec $Container @Probe | Out-Null
        if ($LASTEXITCODE -eq 0) { return }
        Start-Sleep -Milliseconds 500
    }
    throw "$Container did not become ready"
}

New-Item -ItemType Directory -Force $OutputDir | Out-Null
Push-Location $root
try {
    Invoke-Checked docker @('run', '--rm', '-d', '--name', $pg, '-e', 'POSTGRES_PASSWORD=bobby-coverage-test',
        '-p', '127.0.0.1::5432', 'postgres:17.5') | Out-Null
    Invoke-Checked docker @('run', '--rm', '-d', '--name', $redis, '-p', '127.0.0.1::6379', 'redis:7.4-alpine') | Out-Null
    Wait-Service $pg @('pg_isready', '-h', '127.0.0.1', '-U', 'postgres')
    Wait-Service $redis @('redis-cli', 'ping')
    $pgPort = ((Invoke-Checked docker @('port', $pg, '5432/tcp')) -split ':')[-1]
    $redisPort = ((Invoke-Checked docker @('port', $redis, '6379/tcp')) -split ':')[-1]
    $env:TEST_DATABASE_URL = "postgres://postgres:bobby-coverage-test@127.0.0.1:${pgPort}/postgres?sslmode=disable"
    $env:TEST_REDIS_ADDR = "127.0.0.1:$redisPort"
    $profile = Join-Path $OutputDir 'go.cover'
    $packages = @(Invoke-Checked go @('list', './cmd/...', './internal/...', './pkg/...'))
    $application = ($packages | Where-Object { $_ -notmatch '/internal/testsupport$' }) -join ','
    Invoke-Checked go (@('test', '-count=1', "-coverpkg=$application", "-coverprofile=$profile") + $packages)
    Invoke-Checked go @('tool', 'cover', "-func=$profile") | Tee-Object -FilePath (Join-Path $OutputDir 'go-functions.txt') | Select-Object -Last 1
    Invoke-Checked go @('tool', 'cover', "-html=$profile", '-o', (Join-Path $OutputDir 'go.html'))
    $lines = Invoke-Checked go @('run', './scripts/coverage_lines.go', $profile)
    $lines | Set-Content -Encoding utf8 (Join-Path $OutputDir 'go-lines.json')
    $totals = ($lines | ConvertFrom-Json).total
    Write-Output ('Go lines: {0:P2} ({1}/{2})' -f ($totals.covered / $totals.total), $totals.covered, $totals.total)
    Invoke-Checked go @('vet', './...')
} finally {
    docker container inspect $redis 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) { docker stop --time 2 $redis | Out-Null }
    docker container inspect $pg 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) { docker stop --time 2 $pg | Out-Null }
    $env:TEST_DATABASE_URL = $previousDatabase
    $env:TEST_REDIS_ADDR = $previousRedis
    Pop-Location
}

if (!$GoOnly) {
    Push-Location (Join-Path $root 'frontend')
    try {
        Invoke-Checked npm @('ci')
        Invoke-Checked npm @('run', 'check')
        Invoke-Checked npm @('test')
        if (!$env:BOBBY_CHROME) { Invoke-Checked npx @('playwright', 'install', 'chromium') }
        Invoke-Checked npm @('run', 'test:coverage:browser', '--', (Join-Path $OutputDir 'frontend'))
    } finally {
        Pop-Location
    }
}
Write-Output "Coverage reports in $OutputDir"
