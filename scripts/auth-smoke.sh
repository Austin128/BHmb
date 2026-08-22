#!/usr/bin/env bash
# 认证链路端到端冒烟：真实启动 panel 进程，走 HTTPS（自签证书）验证
#   1) 健康检查与统一响应信封
#   2) 登录下发 accessToken 与 HttpOnly 的 nova_rt Cookie（响应体内不含刷新令牌明文）
#   3) profile 权限与菜单
#   4) refresh 轮换 Cookie，旧刷新令牌复用被拒（110021）
#   5) 文件管理：白名单内列目录/编辑/上传/Range 下载/删除，白名单外被拒（400001）
#   5b) 分片上传：断点续传、缺片 400006、合并校验、秒传、放弃会话
#   6) logout 后 accessToken 立即失效
#   7) 内置 SPA 与前端资源可访问（未嵌入前端时该步骤跳过）
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
# 文件管理只开放这一个临时根，冒烟不会碰到宿主真实路径。
# 必须取解析软链后的真实路径：macOS 的 /var 是 /private/var 的软链，
# 守卫会按真实路径判定白名单，否则请求会被判为越界（400001）。
FILE_ROOT="$(cd "$WORK_DIR" && pwd -P)/www"
mkdir -p "$FILE_ROOT"

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
  NOVA_FILE_ALLOW_ROOTS="$FILE_ROOT" \
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
[[ "$code" == "200" ]] || fail "登录 HTTP 状态异常：${code}（$(cat "$WORK_DIR/login.json")）"
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
[[ "$code" == "200" ]] || fail "refresh HTTP 状态异常：${code}（$(cat "$WORK_DIR/refresh.json")）"
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

# 5) 文件管理：白名单内完整读写链路与白名单外拒绝
ACCESS_TOKEN="$NEW_TOKEN"
code="$(api GET "/file/list?path=${FILE_ROOT}" "$WORK_DIR/file-list.json")"
[[ "$code" == "200" ]] || fail "file/list HTTP 状态异常：${code}（$(cat "$WORK_DIR/file-list.json")）"
[[ "$(jget "$WORK_DIR/file-list.json" data.path)" == "$FILE_ROOT" ]] || fail "file/list 返回的目录不符"

# 白名单外的绝对路径必须被守卫拦下
code="$(api GET "/file/list?path=%2Fetc" "$WORK_DIR/file-escape.json")"
[[ "$code" == "403" ]] || fail "白名单外路径应返回 403，实际 $code"
[[ "$(jget "$WORK_DIR/file-escape.json" code)" == "400001" ]] || fail "路径逃逸错误码应为 400001"

# 新建 + 读取 + 乐观锁保存
code="$(api POST /file/file "$WORK_DIR/file-create.json" "{\"path\":\"${FILE_ROOT}/app.conf\"}")"
[[ "$code" == "200" ]] || fail "file/file HTTP 状态异常：${code}（$(cat "$WORK_DIR/file-create.json")）"
# 覆盖既有文件必须带上读取时拿到的 etag，否则按并发冲突拒绝
code="$(api GET "/file/content?path=${FILE_ROOT}/app.conf" "$WORK_DIR/file-read.json")"
[[ "$code" == "200" ]] || fail "读取内容失败：${code}（$(cat "$WORK_DIR/file-read.json")）"
READ_ETAG="$(jget "$WORK_DIR/file-read.json" data.etag)"
[[ -n "$READ_ETAG" ]] || fail "读取未返回 etag"
# ETag 形如 "<hex>"（含双引号），拼进 JSON 前必须转义
READ_ETAG_JSON="${READ_ETAG//\"/\\\"}"
code="$(api PUT /file/content "$WORK_DIR/file-save.json" "{\"path\":\"${FILE_ROOT}/app.conf\",\"content\":\"port=80\\n\",\"etag\":\"${READ_ETAG_JSON}\"}")"
[[ "$code" == "200" ]] || fail "首次保存失败：${code}（$(cat "$WORK_DIR/file-save.json")）"
ETAG="$(jget "$WORK_DIR/file-save.json" data.etag)"
[[ -n "$ETAG" ]] || fail "保存未返回 etag"

# 用已过期的 etag 重放：必须被乐观锁拦下
code="$(api PUT /file/content "$WORK_DIR/file-stale.json" "{\"path\":\"${FILE_ROOT}/app.conf\",\"content\":\"port=8080\\n\",\"etag\":\"stale\"}")"
[[ "$code" == "409" ]] || fail "陈旧 etag 应返回 409，实际 $code"
[[ "$(jget "$WORK_DIR/file-stale.json" code)" == "400011" ]] || fail "乐观锁错误码应为 400011"

# 上传（multipart）+ Range 下载
printf 'a,b,c\n1,2,3\n' >"$WORK_DIR/report.csv"
code="$(curl -sk -o "$WORK_DIR/file-upload.json" -w '%{http_code}' -X POST \
  -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -F "path=${FILE_ROOT}" -F 'conflict=reject' -F "file=@${WORK_DIR}/report.csv" \
  "${API}/file/upload")"
[[ "$code" == "200" ]] || fail "上传 HTTP 状态异常：${code}（$(cat "$WORK_DIR/file-upload.json")）"
[[ "$(jget "$WORK_DIR/file-upload.json" data.name)" == "report.csv" ]] || fail "上传返回的文件名不符"

code="$(curl -sk -o "$WORK_DIR/range.txt" -w '%{http_code}' -H "Range: bytes=2-4" \
  -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  "${API}/file/download?path=${FILE_ROOT}/report.csv")"
[[ "$code" == "206" ]] || fail "Range 下载应返回 206，实际 $code"
[[ "$(cat "$WORK_DIR/range.txt")" == "b,c" ]] || fail "Range 下载内容不符：$(cat "$WORK_DIR/range.txt")"

# 批量删除：存在的成功、缺失的逐项报错
code="$(api DELETE /file/items "$WORK_DIR/file-delete.json" "{\"paths\":[\"${FILE_ROOT}/report.csv\",\"${FILE_ROOT}/missing.txt\"]}")"
[[ "$code" == "200" ]] || fail "删除 HTTP 状态异常：${code}（$(cat "$WORK_DIR/file-delete.json")）"
[[ "$(jget "$WORK_DIR/file-delete.json" data.succeeded)" == "1" ]] || fail "删除成功数不符"
[[ "$(jget "$WORK_DIR/file-delete.json" data.failed)" == "1" ]] || fail "删除失败数不符"
echo "✓ 文件管理列目录/保存/上传/Range 下载/删除，且白名单外被拒（400001）"

# 5b) 分片上传：断点续传、缺片 400006、合并落地、放弃会话
# 服务端会把 chunkSize 夹到 64KB 起，取 64KB + 160000 字节负载正好切 3 片。
CHUNK_SIZE=65536
# SUMS[0] 为整文件哈希，SUMS[i+1] 为第 i 片的哈希（bash 3.2 无 mapfile，用读循环收集）
SUMS=()
while IFS= read -r line; do SUMS+=("$line"); done < <(python3 - "$WORK_DIR/big.bin" "$CHUNK_SIZE" <<'PY'
import hashlib, sys
path, chunk = sys.argv[1], int(sys.argv[2])
data = bytes((i * 7 + 11) % 256 for i in range(160000))
with open(path, 'wb') as fh:
    fh.write(data)
out = ['sha256:' + hashlib.sha256(data).hexdigest()]
for idx in range((len(data) + chunk - 1) // chunk):
    part = data[idx * chunk:(idx + 1) * chunk]
    with open(f'{path}.{idx}', 'wb') as fh:
        fh.write(part)
    out.append('sha256:' + hashlib.sha256(part).hexdigest())
print('\n'.join(out))
PY
)
[[ "${#SUMS[@]}" == "4" ]] || fail "分片测试数据准备失败（得到 ${#SUMS[@]} 个哈希）"
BIG_SIZE="$(wc -c <"$WORK_DIR/big.bin" | tr -d ' ')"

# put_chunk <序号>：上传单片并附带该片校验和
put_chunk() {
  local idx="$1"
  curl -sk -o "$WORK_DIR/chunk-${idx}.json" -w '%{http_code}' -X POST \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -F "uploadId=${UPLOAD_ID}" -F "index=${idx}" -F "checksum=${SUMS[$((idx + 1))]}" \
    -F "chunk=@${WORK_DIR}/big.bin.${idx}" \
    "${API}/file/upload/chunk"
}

code="$(api POST /file/upload/init "$WORK_DIR/chunk-init.json" \
  "{\"path\":\"${FILE_ROOT}\",\"filename\":\"big.bin\",\"size\":${BIG_SIZE},\"chunkSize\":${CHUNK_SIZE},\"hash\":\"${SUMS[0]}\"}")"
[[ "$code" == "200" ]] || fail "upload/init HTTP 状态异常：${code}（$(cat "$WORK_DIR/chunk-init.json")）"
UPLOAD_ID="$(jget "$WORK_DIR/chunk-init.json" data.uploadId)"
[[ -n "$UPLOAD_ID" ]] || fail "upload/init 未返回 uploadId"
[[ "$(jget "$WORK_DIR/chunk-init.json" data.totalChunks)" == "3" ]] || fail "分片数应为 3"
[[ "$(jget "$WORK_DIR/chunk-init.json" data.quickUpload)" == "False" ]] || fail "首次上传不应命中秒传"
[[ -n "$(jget "$WORK_DIR/chunk-init.json" data.expireAt)" ]] || fail "upload/init 未返回 expireAt"

# 先只传首尾两片，留出缺片
for idx in 0 2; do
  code="$(put_chunk "$idx")"
  [[ "$code" == "200" ]] || fail "分片 ${idx} 上传失败：${code}（$(cat "$WORK_DIR/chunk-${idx}.json")）"
done

# 缺片时合并必须被拒，并在 data.missing 指明待重传序号
code="$(api POST /file/upload/complete "$WORK_DIR/chunk-missing.json" "{\"uploadId\":\"${UPLOAD_ID}\"}")"
[[ "$code" == "400" ]] || fail "缺片合并应返回 400，实际 $code"
[[ "$(jget "$WORK_DIR/chunk-missing.json" code)" == "400006" ]] || fail "缺片错误码应为 400006"
[[ "$(jget "$WORK_DIR/chunk-missing.json" data.missing)" == "[1]" ]] || fail "缺片列表应为 [1]，实际 $(jget "$WORK_DIR/chunk-missing.json" data.missing)"

# 续传前对齐进度
code="$(api GET "/file/upload/status?uploadId=${UPLOAD_ID}" "$WORK_DIR/chunk-status.json")"
[[ "$code" == "200" ]] || fail "upload/status HTTP 状态异常：$code"
[[ "$(jget "$WORK_DIR/chunk-status.json" data.uploadedChunks)" == "[0, 2]" ]] || fail "已收分片应为 [0, 2]"
[[ "$(jget "$WORK_DIR/chunk-status.json" data.missingChunks)" == "[1]" ]] || fail "缺失分片应为 [1]"

# 重复 init 必须复用同一会话，这是刷新页面后能续传的前提
code="$(api POST /file/upload/init "$WORK_DIR/chunk-reinit.json" \
  "{\"path\":\"${FILE_ROOT}\",\"filename\":\"big.bin\",\"size\":${BIG_SIZE},\"chunkSize\":${CHUNK_SIZE},\"hash\":\"${SUMS[0]}\"}")"
[[ "$code" == "200" ]] || fail "重复 init HTTP 状态异常：$code"
[[ "$(jget "$WORK_DIR/chunk-reinit.json" data.uploadId)" == "$UPLOAD_ID" ]] || fail "重复 init 未复用会话"
[[ "$(jget "$WORK_DIR/chunk-reinit.json" data.uploadedChunks)" == "[0, 2]" ]] || fail "重复 init 未回传已收分片"

# 补齐缺片后合并落地
code="$(put_chunk 1)"
[[ "$code" == "200" ]] || fail "分片 1 上传失败：${code}（$(cat "$WORK_DIR/chunk-1.json")）"
code="$(api POST /file/upload/complete "$WORK_DIR/chunk-complete.json" \
  "{\"uploadId\":\"${UPLOAD_ID}\",\"hash\":\"${SUMS[0]}\"}")"
[[ "$code" == "200" ]] || fail "合并失败：${code}（$(cat "$WORK_DIR/chunk-complete.json")）"
[[ "$(jget "$WORK_DIR/chunk-complete.json" data.entry.name)" == "big.bin" ]] || fail "合并返回的文件名不符"
[[ "$(jget "$WORK_DIR/chunk-complete.json" data.size)" == "$BIG_SIZE" ]] || fail "合并后大小不符"
[[ "$(jget "$WORK_DIR/chunk-complete.json" data.hash)" == "${SUMS[0]}" ]] || fail "合并后哈希不符"

# 落盘内容必须与源文件逐字节一致
LOCAL_HASH="sha256:$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$FILE_ROOT/big.bin")"
[[ "$LOCAL_HASH" == "${SUMS[0]}" ]] || fail "落盘文件哈希与源文件不一致"

# 同哈希再传一次必须命中秒传
code="$(api POST /file/upload/init "$WORK_DIR/chunk-quick.json" \
  "{\"path\":\"${FILE_ROOT}\",\"filename\":\"big.bin\",\"size\":${BIG_SIZE},\"chunkSize\":${CHUNK_SIZE},\"hash\":\"${SUMS[0]}\"}")"
[[ "$code" == "200" ]] || fail "秒传 init HTTP 状态异常：$code"
[[ "$(jget "$WORK_DIR/chunk-quick.json" data.quickUpload)" == "True" ]] || fail "同哈希文件应命中秒传"

# 放弃会话：清理分片且后续 status 查不到
code="$(api POST /file/upload/init "$WORK_DIR/chunk-abort-init.json" \
  "{\"path\":\"${FILE_ROOT}\",\"filename\":\"abort.bin\",\"size\":${BIG_SIZE},\"chunkSize\":${CHUNK_SIZE}}")"
[[ "$code" == "200" ]] || fail "放弃用例 init 失败：$code"
ABORT_ID="$(jget "$WORK_DIR/chunk-abort-init.json" data.uploadId)"
code="$(api DELETE "/file/upload/${ABORT_ID}" "$WORK_DIR/chunk-abort.json")"
[[ "$code" == "200" ]] || fail "放弃上传 HTTP 状态异常：$code"
code="$(api GET "/file/upload/status?uploadId=${ABORT_ID}" "$WORK_DIR/chunk-gone.json")"
[[ "$code" == "404" ]] || fail "已放弃的会话应返回 404，实际 $code"
[[ "$(jget "$WORK_DIR/chunk-gone.json" code)" == "400002" ]] || fail "已放弃会话错误码应为 400002"
[[ ! -f "$FILE_ROOT/abort.bin" ]] || fail "放弃上传后不应留下目标文件"
rm -f "$FILE_ROOT/big.bin"
echo "✓ 分片上传断点续传、缺片 400006、合并校验、秒传与放弃会话"

# 6) logout 后令牌失效
ACCESS_TOKEN="$NEW_TOKEN"
code="$(api POST /auth/logout "$WORK_DIR/logout.json")"
[[ "$code" == "200" ]] || fail "logout HTTP 状态异常：$code"
code="$(api GET /auth/profile "$WORK_DIR/after-logout.json")"
[[ "$code" == "401" ]] || fail "logout 后 accessToken 仍可用（HTTP ${code}）"
echo "✓ logout 撤销会话与 accessToken"

# 7) 内置 SPA
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

echo "认证与文件管理链路冒烟全部通过"
