#!/usr/bin/env bash
# agentd 部署：构建后端 → 构建前端 → 重启 → 健康检查
# 用法：scripts/deploy.sh [--no-front]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$HOME/.local/bin/agentd"
TMP="/tmp/agentd.new.$$"
UNIT="agentd"
PORT=18400

say() { printf '  %s\n' "$*"; }

# 1) 后端
say "构建后端…"
cd "$ROOT"
export PATH="$PATH:/usr/local/go/bin"
go build -o "$TMP" ./cmd/agentd
say "  ok: $(du -h "$TMP" | cut -f1)"

# 2) 前端（可选跳过）
if [ "${1:-}" != "--no-front" ]; then
  say "构建前端…"
  cd "$ROOT/web"
  # 只有确实需要联网安装时才用代理；构建本身不需要
  npm run build 2>&1 | tail -2 | sed 's/^/  /'
fi

# 3) 替换二进制并重启
say "部署二进制…"
systemctl --user stop "$UNIT"
cp "$TMP" "$BIN"
chmod +x "$BIN"
rm -f "$TMP"
systemctl --user start "$UNIT"
sleep 2

# 4) 健康检查
code=$(curl -s --noproxy '*' -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/api/health" || true)
if [ "$code" = "200" ]; then
  say "健康检查通过（$PORT → $code）✓"
else
  say "健康检查失败（$PORT → $code）✗"
  systemctl --user status "$UNIT" --no-pager -n 15 || true
  exit 1
fi
