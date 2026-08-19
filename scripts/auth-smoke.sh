#!/usr/bin/env bash
# 认证链路端到端冒烟：真实启动 panel 进程，走 HTTPS（自签证书）验证
#   1) 健康检查与统一响应信封
#   2) 登录下发 accessToken 与 HttpOnly 的 nova_rt Cookie（响应体内不含刷新令牌明文）
#   3) profile 权限与菜单
#   4) refresh 轮换 Cookie，旧刷新令牌复用被拒（110021）
#   5) logout 后 accessToken 立即失效
#   6) 内置 SPA 与前端资源可访问（未嵌入前端时该步骤跳过）
# 脚本自建临时数据目录与证书，不触碰 /opt 与仓库现有数据。
set -euo pipefail

cd "$(dirname "$0")/.."

command -v curl >/dev/null 2>&1 || { echo "缺少 curl" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "缺少 python3" >&2; exit 1; }

WORK_DIR="$(mktemp -d)"
PORT="${PORT:-38567}"
BASE="https://127.0.0.1:${PORT}"
API="${BASE}/api/v1"
ADMIN_PASSWORD='Sm0ke-Test!2026'
JAR="$WORK_DIR/cookies.txt"
SERVER_LOG="$WORK_DIR/panel.log"
PANEL_PID=""

cleanup() {
  if [[ -n "$PANEL_PID" ]] && kill -0 "$PANEL_PID" 2>/dev/null; then
    kill "$PANEL_PID" 2>/dev/null || true
    wait "$PANEL_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

fail() {
  echo "✗ $1" >&2
  echo "---- 服务日志 ----" >&2
  tail -30 "$SERVER_LOG" >&2 || true
  exit 1
}

# jget <json文件> <点号路径>：取出字段，缺失则输出空串
jget() {
  python3 - "$1" "$2" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as fh:
    node = json.load(fh)
for part in sys.argv[2].split('.'):
    if part == '':
        continue
    if isinstance(node, dict) and part in node:
        node = node[part]
    else:
        print('')
        raise SystemExit(0)
print('' if node is None else node)
PY
}

# api <方法> <路径> <输出文件> [数据] ：附带 Cookie jar 与统一头，输出 HTTP 状态码
api() {
  local method="$1" path="$2" out="$3" data="${4:-}"
  local args=(-sk -o "$out" -w '%{http_code}' -X "$method"
    -H 'Content-Type: application/json' -H "X-Request-Id: $(uuidgen 2>/dev/null || echo smoke-$RANDOM)"
    -b "$JAR" -c "$JAR")
  [[ -n "${ACCESS_TOKEN:-}" ]] && args+=(-H "Authorization: Bearer ${ACCESS_TOKEN}")
  [[ -n "$data" ]] && args+=(--data "$data")
  curl "${args[@]}" "${API}${path}"
}

echo "==> 构建 panel"
go build -o "$WORK_DIR/panel" ./cmd/panel

echo "==> 启动 panel（HTTPS 自签证书，端口 ${PORT}）"
env \
  NOVA_SERVER_HOST=127.0.0.1 \
  NOVA_SERVER_PORT="$PORT" \
  NOVA_SERVER_TLS_ENABLED=true \
  NOVA_SERVER_TLS_AUTO_SELF_SIGNED=true \
  NOVA_SERVER_TLS_CERT_FILE="$WORK_DIR/certs/panel.crt" \
  NOVA_SERVER_TLS_KEY_FILE="$WORK_DIR/certs/panel.key" \
  NOVA_DATABASE_DRIVER=sqlite \
  NOVA_DATABASE_PATH="$WORK_DIR/nova.db" \
  NOVA_KV_PATH="$WORK_DIR/kv.bolt" \
  NOVA_LOG_DIR="$WORK_DIR/logs" \
  NOVA_LOG_CONSOLE=true \
  NOVA_SECURITY_MASTER_KEY_FILE="$WORK_DIR/master.key" \
  NOVA_SECURITY_BCRYPT_COST=10 \
  NOVA_INITIAL_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
  "$WORK_DIR/panel" -config "" >"$SERVER_LOG" 2>&1 &
PANEL_PID=$!

for _ in $(seq 1 60); do
  code="$(curl -sk -o "$WORK_DIR/health.json" -w '%{http_code}' "${API}/health" || true)"
  [[ "$code" == "200" ]] && break
  kill -0 "$PANEL_PID" 2>/dev/null || fail "panel 进程已退出"
  sleep 0.5
done
[[ "${code:-}" == "200" ]] || fail "健康检查未就绪"

# 1) 健康检查信封
[[ "$(jget "$WORK_DIR/health.json" code)" == "0" ]] || fail "健康检查 code 不为 0"
[[ -n "$(jget "$WORK_DIR/health.json" traceId)" ]] || fail "响应缺少 traceId"
[[ "$(jget "$WORK_DIR/health.json" data.database.ok)" == "True" ]] || fail "数据库不可达"
[[ "$(jget "$WORK_DIR/health.json" data.status)" == "ok" ]] || fail "健康状态不为 ok"
echo "✓ 健康检查与响应信封"

# 2) 登录
ACCESS_TOKEN=""
code="$(api POST /auth/login "$WORK_DIR/login.json" "{\"username\":\"admin\",\"password\":\"${ADMIN_PASSWORD}\",\"remember\":false}")"
[[ "$code" == "200" ]] || fail "登录 HTTP 状态异常：$code（$(cat "$WORK_DIR/login.json")）"
ACCESS_TOKEN="$(jget "$WORK_DIR/login.json" data.accessToken)"
[[ -n "$ACCESS_TOKEN" ]] || fail "登录未返回 accessToken"
grep -q "nova_rt" "$JAR" || fail "登录未下发 nova_rt Cookie"
# curl 对 HttpOnly Cookie 会写成 "#HttpOnly_" 前缀行，据此校验属性未被弱化
grep -q "#HttpOnly_.*nova_rt" "$JAR" || fail "nova_rt 缺少 HttpOnly 属性"
# 响应体内不得出现刷新令牌明文
if grep -qi 'refreshtoken' "$WORK_DIR/login.json"; then
  fail "登录响应体泄露了 refreshToken"
fi
OLD_RT="$(awk '/nova_rt/ {print $NF}' "$JAR" | tail -1)"
[[ -n "$OLD_RT" ]] || fail "无法读取 nova_rt 值"
echo "✓ 登录下发 accessToken 与 HttpOnly 刷新 Cookie"

# 3) profile
code="$(api GET /auth/profile "$WORK_DIR/profile.json")"
[[ "$code" == "200" ]] || fail "profile HTTP 状态异常：$code"
[[ "$(jget "$WORK_DIR/profile.json" data.user.username)" == "admin" ]] || fail "profile 用户名不符"
[[ -n "$(jget "$WORK_DIR/profile.json" data.menus)" ]] || fail "profile 未返回菜单"
echo "✓ profile 返回身份、权限与菜单"

# 未带令牌应 401
SAVED_TOKEN="$ACCESS_TOKEN"
ACCESS_TOKEN=""
code="$(api GET /auth/profile "$WORK_DIR/anon.json")"
[[ "$code" == "401" ]] || fail "匿名访问 profile 应返回 401，实际 $code"
[[ "$(jget "$WORK_DIR/anon.json" code)" == "110001" ]] || fail "匿名访问错误码应为 110001"
ACCESS_TOKEN="$SAVED_TOKEN"
echo "✓ 匿名访问被拒（110001）"

# 4) refresh 轮换 + 旧令牌复用检测
cp "$JAR" "$WORK_DIR/cookies-old.txt"
code="$(api POST /auth/refresh "$WORK_DIR/refresh.json")"
[[ "$code" == "200" ]] || fail "refresh HTTP 状态异常：$code（$(cat "$WORK_DIR/refresh.json")）"
NEW_TOKEN="$(jget "$WORK_DIR/refresh.json" data.accessToken)"
[[ -n "$NEW_TOKEN" ]] || fail "refresh 未返回新 accessToken"
NEW_RT="$(awk '/nova_rt/ {print $NF}' "$JAR" | tail -1)"
[[ "$NEW_RT" != "$OLD_RT" ]] || fail "refresh 未轮换刷新令牌"

# 用旧 Cookie 再次刷新：必须被判定为复用
reuse_code="$(curl -sk -o "$WORK_DIR/reuse.json" -w '%{http_code}' -X POST \
  -b "$WORK_DIR/cookies-old.txt" "${API}/auth/refresh")"
[[ "$reuse_code" == "401" ]] || fail "旧刷新令牌复用应返回 401，实际 $reuse_code"
[[ "$(jget "$WORK_DIR/reuse.json" code)" == "110021" ]] || fail "复用错误码应为 110021"
echo "✓ 刷新令牌轮换且旧令牌复用被拒（110021）"

# 5) logout 后令牌失效
ACCESS_TOKEN="$NEW_TOKEN"
code="$(api POST /auth/logout "$WORK_DIR/logout.json")"
[[ "$code" == "200" ]] || fail "logout HTTP 状态异常：$code"
code="$(api GET /auth/profile "$WORK_DIR/after-logout.json")"
[[ "$code" == "401" ]] || fail "logout 后 accessToken 仍可用（HTTP $code）"
echo "✓ logout 撤销会话与 accessToken"

# 6) 内置 SPA
if [[ -f internal/web/dist/index.html ]]; then
  code="$(curl -sk -o "$WORK_DIR/index.html" -w '%{http_code}' "${BASE}/")"
  [[ "$code" == "200" ]] || fail "SPA 首页 HTTP 状态异常：$code"
  grep -qi '<div id="app"' "$WORK_DIR/index.html" || fail "首页不是前端产物"
  # 前端路由深链应回落到 index.html，而非 404
  code="$(curl -sk -o "$WORK_DIR/deep.html" -w '%{http_code}' "${BASE}/dashboard")"
  [[ "$code" == "200" ]] || fail "SPA 深链回落失败：$code"
  # API 未命中路由仍应返回 JSON 信封而非 index.html
  code="$(curl -sk -o "$WORK_DIR/api404.json" -w '%{http_code}' "${API}/not-exist")"
  [[ "$code" == "404" ]] || fail "未知 API 路由应返回 404，实际 $code"
  [[ "$(jget "$WORK_DIR/api404.json" code)" == "100003" ]] || fail "未知 API 路由应返回 100003"
  echo "✓ 内置 SPA 首页、深链回落与 API 404 信封"
else
  echo "! 跳过 SPA 校验：internal/web/dist/index.html 不存在（先执行 make web-build）"
fi

echo "认证链路冒烟全部通过"
