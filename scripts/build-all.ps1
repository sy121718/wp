# ERP 完整打包脚本
# 支持两种模式：embed（嵌入）和 separate（分离）

param(
    [Parameter(Mandatory=$true)]
    [ValidateSet("embed", "separate")]
    [string]$Mode
)

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent $PSScriptRoot

Write-Host "Building ERP in $Mode mode..." -ForegroundColor Cyan

if ($Mode -eq "embed") {
    # 嵌入模式：前后端合并打包
    Write-Host "Mode: Embed (frontend embedded in backend binary)" -ForegroundColor Yellow
    Write-Host "Output: dist/embed/" -ForegroundColor Yellow

    # 准备前端资源
    Write-Host "`nStep 1: Preparing frontend resources..." -ForegroundColor Cyan
    & $PSScriptRoot\prepare-embed.ps1

    # 后端依赖整理
    Write-Host "`nStep 2: Preparing backend dependencies..." -ForegroundColor Cyan
    cd $RootDir\erp_server
    go mod tidy
    go generate ./...

    # 运行 GoReleaser
    Write-Host "`nStep 3: Building with GoReleaser..." -ForegroundColor Cyan
    cd $RootDir
    goreleaser release --config .goreleaser.embed.yaml --snapshot --clean

    Write-Host "`n========================================" -ForegroundColor Green
    Write-Host "Build complete!" -ForegroundColor Green
    Write-Host "Output directory: dist/embed/" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "`nDeployment:" -ForegroundColor Cyan
    Write-Host "  1. Copy the archive to your server" -ForegroundColor White
    Write-Host "  2. Extract and run erp-server" -ForegroundColor White
    Write-Host "  3. Access http://localhost:8081/" -ForegroundColor White
    Write-Host "`nNote: Frontend is embedded in the binary." -ForegroundColor Yellow

} else {
    # 分离模式：前后端分别打包
    Write-Host "Mode: Separate (frontend and backend separate)" -ForegroundColor Yellow
    Write-Host "Output: dist/separate/" -ForegroundColor Yellow

    # 构建前端
    Write-Host "`nStep 1: Building frontend..." -ForegroundColor Cyan
    & $PSScriptRoot\build-frontend.ps1

    # 后端依赖整理
    Write-Host "`nStep 2: Preparing backend dependencies..." -ForegroundColor Cyan
    cd $RootDir\erp_server
    go mod tidy
    go generate ./...

    # 构建后端
    Write-Host "`nStep 3: Building backend with GoReleaser..." -ForegroundColor Cyan
    cd $RootDir
    goreleaser release --config .goreleaser.separate.yaml --snapshot --clean

    Write-Host "`n========================================" -ForegroundColor Green
    Write-Host "Build complete!" -ForegroundColor Green
    Write-Host "Output directory: dist/separate/" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "`nDeployment:" -ForegroundColor Cyan
    Write-Host "  Backend:" -ForegroundColor Yellow
    Write-Host "    1. Copy erp_backend_*.zip/tar.gz to your server" -ForegroundColor White
    Write-Host "    2. Extract and run erp-server" -ForegroundColor White
    Write-Host "  Frontend:" -ForegroundColor Yellow
    Write-Host "    1. Copy dist/separate/frontend/ to your web server" -ForegroundColor White
    Write-Host "    2. Configure nginx/apache to serve static files" -ForegroundColor White
    Write-Host "    3. Configure proxy to backend API at localhost:8081" -ForegroundColor White
}

Write-Host "`nDone!" -ForegroundColor Green
