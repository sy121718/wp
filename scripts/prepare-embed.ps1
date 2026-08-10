# 准备嵌入模式的前端资源
# 构建前端并复制到后端 web/dist 目录

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent $PSScriptRoot

Write-Host "Preparing frontend for embed mode..." -ForegroundColor Cyan

# 进入前端目录
cd $RootDir\erp_web

# 安装依赖
Write-Host "Installing frontend dependencies..." -ForegroundColor Yellow
npm install

# 构建
Write-Host "Building frontend..." -ForegroundColor Yellow
npm run build

# 创建后端 web/dist 目录
$WebDistDir = "$RootDir\erp_server\internal\handler\web\dist"
Write-Host "Copying to $WebDistDir..." -ForegroundColor Yellow

if (!(Test-Path $WebDistDir)) {
    New-Item -ItemType Directory -Path $WebDistDir -Force | Out-Null
}

# 清空旧文件
Remove-Item -Path "$WebDistDir\*" -Recurse -Force -ErrorAction SilentlyContinue

# 复制新文件
Copy-Item -Path "$RootDir\erp_web\dist\*" -Destination $WebDistDir -Recurse -Force

Write-Host "`nFrontend resources prepared successfully!" -ForegroundColor Green
Write-Host "Location: $WebDistDir" -ForegroundColor Green
