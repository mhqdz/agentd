#!/usr/bin/env bash
# agentd 部署：构建后端 → 构建前端 → 重启 → 健康检查
# 用法：scripts/deploy.sh [--no-front] [--no-plugins]
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

# 3.5) 插件清单：仓库模板 → ~/.data/agentd/tools.d（替换 @REPO@ 为实际仓库路径）
if [ "${1:-}" != "--no-plugins" ]; then
  say "同步插件清单…"
  DEST="$HOME/.data/agentd/tools.d"
  mkdir -p "$DEST"
  n=0
  for tpl in "$ROOT"/scripts/tools.d/*.json; do
    [ -e "$tpl" ] || continue
    name="$(basename "$tpl")"
    tmp="$(mktemp)"
    sed "s#@REPO@#$ROOT#g" "$tpl" > "$tmp"
    if [ -f "$DEST/$name" ] && ! cmp -s "$tmp" "$DEST/$name"; then
      cp -f "$DEST/$name" "$DEST/$name.bak-$(date +%Y%m%d-%H%M%S)"
      say "  更新 $name（旧文件已备份）"
    fi
    cp -f "$tmp" "$DEST/$name"
    rm -f "$tmp"
    n=$((n+1))
  done
  say "  清单 $n 个 → $DEST"
fi

# 4) 健康检查
code=$(curl -s --noproxy '*' -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/api/health" || true)
if [ "$code" = "200" ]; then
  say "健康检查通过（$PORT → $code）✓"
else
  say "健康检查失败（$PORT → $code）✗"
  systemctl --user status "$UNIT" --no-pager -n 15 || true
  exit 1
fi
