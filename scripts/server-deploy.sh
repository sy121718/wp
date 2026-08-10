#!/bin/bash
set -e
WORKDIR="/www/wwwroot/erp"
cd "$WORKDIR"

APP_ARC=$(ls *_amd64.tar.gz 2>/dev/null | head -1)
if [ -z "$APP_ARC" ]; then
  echo "错误: 未找到 *_amd64.tar.gz"
  exit 1
fi

echo "[1/5] 解压项目压缩包: $APP_ARC"
tar xzf "$APP_ARC" || bsdtar xzf "$APP_ARC"
chmod +x app

echo "[2/5] 解压配置文件..."
tar xzf "config.yaml_iFr7r.tar.gz"
[ -f config.yaml ] && cp config.yaml "config.yaml.bak.$(date +%s)"
echo "  配置文件已替换"

echo "[3/5] 归档旧版本..."
mkdir -p old
mv "$APP_ARC" old/

echo "[4/5] 停止旧进程(端口8080)..."
OLD_PID=$(ss -ltnp | grep ":8080 " | grep -oP "pid=\K[0-9]+" | head -1)
if [ -n "$OLD_PID" ]; then
  echo "  旧进程 PID=$OLD_PID"
  kill "$OLD_PID" 2>/dev/null
  sleep 1
  kill -0 "$OLD_PID" 2>/dev/null && kill -9 "$OLD_PID" 2>/dev/null || true
else
  echo "  无旧进程运行"
fi

echo "[5/5] nohup 启动新版本..."
nohup ./app > app.log 2>&1 &
echo "  新进程 PID=$!"
echo "部署完成"