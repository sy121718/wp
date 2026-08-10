# 前端构建脚本（分离模式）
# 构建前端并输出到 dist/separate/frontend

$ErrorActionPreference = "Stop"

Write-Host "Building frontend..." -ForegroundColor Green

# 进入前端目录
cd $PSScriptRoot\..\erp_web

# 安装依赖
npm install

# 构建
npm run build

# 复制到输出目录
$OutputDir = "..\dist\separate\frontend"
if (!(Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
}

# 清空旧文件
Remove-Item -Path "$OutputDir\*" -Recurse -Force -ErrorAction SilentlyContinue

# 复制新文件
Copy-Item -Path "dist\*" -Destination $OutputDir -Recurse -Force

Write-Host "Frontend built successfully to $OutputDir" -ForegroundColor Green
