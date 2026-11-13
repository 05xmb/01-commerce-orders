#!/bin/sh
# 容器入口：按 ROLE 选择运行角色
#   server  → 交易系统 API（:8888）
#   channel → 模拟支付渠道（:8902）
#   web     → nginx 托管 SPA 并反代 /api、/channel（:80）
set -e

case "$ROLE" in
  server)
    exec /app/trade-api -f /app/etc/server.yaml
    ;;
  channel)
    exec /app/pay-channel -f /app/etc/channel.yaml
    ;;
  web)
    exec nginx -g 'daemon off;' -c /app/etc/nginx.conf
    ;;
  *)
    echo "未知 ROLE: $ROLE（可选 server/channel/web）" >&2
    exit 1
    ;;
esac
