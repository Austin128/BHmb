#!/usr/bin/env bash
# 在带 systemd 的 Linux 容器里真机演练一键安装：install.sh → 服务启动 → 登录 → bh 子命令 → 卸载。
# 需要 Docker，且容器必须 --privileged 才能跑 systemd（仅本地/CI 验证用，不影响生产）。
#
#   ./scripts/install-verify.sh            # 自动 make release 取包
#   NOVA_PKG=dist/xxx.tar.gz ./scripts/install-verify.sh
#   NOVA_VERIFY_PLATFORM=linux/amd64 ./scripts/install-verify.sh   # 模拟 CI runner 的架构
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_IMAGE="${NOVA_VERIFY_BASE_IMAGE:-debian:12}"
IMAGE="${NOVA_VERIFY_IMAGE:-novapanel-systemd-test:debian12}"
CONTAINER="${NOVA_VERIFY_CONTAINER:-nova-install-verify}"
BUILD_CONTAINER="${CONTAINER}-build"
PORT=34567

PASS=0
FAIL=0
t_pass() {
  printf '\033[32m✓\033[0m %s\n' "$1"
  PASS=$((PASS + 1))
}
t_fail() {
  printf '\033[31m✗\033[0m %s\n' "$1" >&2
  FAIL=$((FAIL + 1))
}
step() { printf '\033[36m==>\033[0m %s\n' "$1"; }
dexec() { docker exec "$CONTAINER" bash -lc "$1"; }

cleanup() { docker rm -f "$CONTAINER" "$BUILD_CONTAINER" >/dev/null 2>&1 || true; }
trap cleanup EXIT

command -v docker >/dev/null || {
  echo "需要 docker" >&2
  exit 1
}
docker info >/dev/null 2>&1 || {
  echo "docker 守护进程不可用" >&2
  exit 1
}

# 默认跟随本机 Docker 架构；显式指定 NOVA_VERIFY_PLATFORM 可在 arm64 机器上模拟 amd64 runner
PLATFORM="${NOVA_VERIFY_PLATFORM:-}"
if [[ -n "$PLATFORM" ]]; then
  ARCH="${PLATFORM#*/}"
  IMAGE="${IMAGE}-${ARCH}"
  DOCKER_PLATFORM_ARGS=(--platform "$PLATFORM")
else
  ARCH="$(docker version --format '{{.Server.Arch}}')"
  DOCKER_PLATFORM_ARGS=()
fi
PKG="${NOVA_PKG:-}"
if [[ -z "$PKG" ]]; then
  PKG="$(ls "$ROOT"/dist/novapanel-*-linux-"$ARCH".tar.gz 2>/dev/null | head -1 || true)"
fi
if [[ -z "$PKG" || ! -f "$PKG" ]]; then
  step "未找到 linux-$ARCH 发布包，执行 make release"
  (cd "$ROOT" && make release >/dev/null)
  PKG="$(ls "$ROOT"/dist/novapanel-*-linux-"$ARCH".tar.gz | head -1)"
fi
step "使用发布包 $(basename "$PKG")"

# 官方 debian 镜像不带 systemd，这里现场构一个带 systemd 的验证镜像（有缓存就复用）
# 注意：故意不装 curl，用来验证 install.sh 的依赖自动安装分支
if [[ "${NOVA_VERIFY_REBUILD:-0}" == "1" ]] || ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  step "构建验证镜像 ${IMAGE}（基于 ${BASE_IMAGE}）"
  if [[ -z "$PLATFORM" ]]; then
    docker build -t "$IMAGE" - >/dev/null <<EOF
FROM ${BASE_IMAGE}
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq \\
 && apt-get install -y -qq --no-install-recommends systemd systemd-sysv dbus procps \\
 && rm -rf /var/lib/apt/lists/*
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
EOF
  else
    # legacy builder 会忽略 --platform（本机 docker 未装 buildx），改用 pull → run → commit
    docker pull -q --platform "$PLATFORM" "$BASE_IMAGE" >/dev/null
    docker rm -f "$BUILD_CONTAINER" >/dev/null 2>&1 || true
    docker run --platform "$PLATFORM" --name "$BUILD_CONTAINER" -e DEBIAN_FRONTEND=noninteractive \
      "$BASE_IMAGE" bash -c 'apt-get update -qq &&
        apt-get install -y -qq --no-install-recommends systemd systemd-sysv dbus procps &&
        rm -rf /var/lib/apt/lists/*' >/dev/null
    docker commit --change 'STOPSIGNAL SIGRTMIN+3' --change 'CMD ["/sbin/init"]' \
      "$BUILD_CONTAINER" "$IMAGE" >/dev/null
    docker rm -f "$BUILD_CONTAINER" >/dev/null
  fi
fi

step "启动 systemd 容器（$IMAGE, ${ARCH}）"
cleanup
docker run -d --name "$CONTAINER" ${DOCKER_PLATFORM_ARGS[@]+"${DOCKER_PLATFORM_ARGS[@]}"} --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
  "$IMAGE" >/dev/null

for _ in $(seq 1 30); do
  if dexec 'systemctl is-system-running --wait 2>/dev/null || systemctl is-system-running' 2>/dev/null |
    grep -qE 'running|degraded'; then break; fi
  sleep 1
done
dexec 'systemctl --version >/dev/null' && t_pass "容器内 systemd 可用" || t_fail "容器内 systemd 不可用"

# 故意不预装 curl/tar：验证 install.sh 的依赖自动安装分支
step "拷入发布包并执行安装"
docker cp "$PKG" "$CONTAINER:/tmp/pkg.tar.gz" >/dev/null
dexec 'mkdir -p /tmp/nova && tar -xzf /tmp/pkg.tar.gz -C /tmp/nova' ||
  dexec 'apt-get update -qq && apt-get install -y -qq tar && mkdir -p /tmp/nova && tar -xzf /tmp/pkg.tar.gz -C /tmp/nova'

install_log="$(dexec 'NOVA_PKG=/tmp/pkg.tar.gz bash /tmp/nova/scripts/install.sh' 2>&1)" || {
  printf '%s\n' "$install_log" >&2
  t_fail "install.sh 执行失败"
  printf '\n通过 %d 项，失败 %d 项\n' "$PASS" "$FAIL"
  exit 1
}
printf '%s\n' "$install_log" | sed 's/^/    | /'
t_pass "install.sh 执行成功"

# 安装输出带 ANSI 颜色序列，先剔掉再取口令
install_log_plain="$(printf '%s\n' "$install_log" | sed $'s/\033\[[0-9;]*m//g')"
ADMIN_PW="$(printf '%s\n' "$install_log_plain" |
  awk -F'初始口令：' '/初始口令：/ {print $2; exit}' | awk '{print $1}')"
if [[ "$ADMIN_PW" =~ ^[A-Za-z0-9]{16}$ ]]; then
  t_pass "安装输出包含 16 位初始口令"
else
  t_fail "未能从安装输出解析初始口令（得到 [${ADMIN_PW}]）"
fi

step "校验服务与接口"

# 安装脚本返回后服务可能仍在收尾（首启要生成自签证书、跑迁移与种子），
# 这里再等一轮就绪，否则慢机器上后面的断言会连锁假失败。
wait_health() {
  local i code
  for i in $(seq 1 30); do
    code="$(dexec "curl -sk -o /dev/null -w '%{http_code}' --max-time 3 https://127.0.0.1:$PORT/api/v1/health" 2>/dev/null || true)"
    [[ "$code" == "200" ]] && return 0
    sleep 1
  done
  return 1
}
wait_health || true

dexec "systemctl is-active --quiet novapanel" && t_pass "systemd 服务处于 active" || t_fail "服务未运行"
dexec "systemctl is-enabled --quiet novapanel" && t_pass "已设为开机自启" || t_fail "未设为开机自启"

health="$(dexec "curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:$PORT/api/v1/health" || true)"
[[ "$health" == "200" ]] && t_pass "健康检查返回 200" || t_fail "健康检查返回 $health"

spa="$(dexec "curl -sk https://127.0.0.1:$PORT/ | head -c 200" || true)"
[[ "$spa" == *"<!DOCTYPE html>"* || "$spa" == *"<!doctype html>"* ]] &&
  t_pass "内置前端可访问" || t_fail "内置前端未返回 HTML"

if [[ -n "$ADMIN_PW" ]]; then
  login="$(dexec "curl -sk -X POST https://127.0.0.1:$PORT/api/v1/auth/login \
    -H 'Content-Type: application/json' \
    -d '{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}'" || true)"
  [[ "$login" == *'"accessToken"'* ]] && t_pass "用安装输出的口令登录成功并拿到访问令牌" ||
    t_fail "登录失败：$(printf '%s' "$login" | head -c 200)"
  [[ "$login" != *'"refreshToken"'* ]] && t_pass "响应体不含刷新令牌（只走 HttpOnly Cookie）" ||
    t_fail "响应体泄露了刷新令牌"
fi

env_content="$(dexec 'cat /opt/novapanel/conf/panel.env' || true)"
[[ -z "$(printf '%s' "$env_content" | tr -d '[:space:]')" ]] &&
  t_pass "安装后 panel.env 已清空（初始口令不留盘）" || t_fail "panel.env 仍残留内容"

perm="$(dexec "stat -c '%a' /opt/novapanel/conf/master.key" || true)"
[[ "$perm" == "600" ]] && t_pass "master.key 权限 600" || t_fail "master.key 权限为 $perm"

# ldflags 的变量名写错（大小写不符）会让发布二进制一直报 dev，装上后才看得出来
ver="$(dexec '/opt/novapanel/bin/novapanel -version' || true)"
if [[ "$ver" == *"novapanel "* && "$ver" != *"novapanel dev"* && "$ver" != *"commit none"* ]]; then
  t_pass "二进制已注入版本信息（${ver}）"
else
  t_fail "二进制版本信息未注入：${ver}"
fi

step "校验 bh 子命令"
dexec 'bh info | grep -q 面板地址' && t_pass "bh info 输出面板地址" || t_fail "bh info 异常"
dexec 'bh status >/dev/null' && t_pass "bh status 可用" || t_fail "bh status 异常"
dexec 'bh log 20 >/dev/null' && t_pass "bh log 可用" || t_fail "bh log 异常"
dexec 'bh restart >/dev/null && systemctl is-active --quiet novapanel' &&
  t_pass "bh restart 后服务仍 active" || t_fail "bh restart 异常"

newpw="$(dexec 'bh passwd admin' 2>&1 || true)"
if [[ -n "$ADMIN_PW" ]]; then
  if dexec "curl -sk -X POST https://127.0.0.1:$PORT/api/v1/auth/login -H 'Content-Type: application/json' \
    -d '{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}' | grep -q 110004"; then
    t_pass "bh passwd 重置后旧口令登录返回 110004"
  else
    t_fail "bh passwd 后旧口令仍可用或返回异常（bh passwd 输出：$(printf '%s' "$newpw" | tail -1)）"
  fi

  # 新口令应立即可用；口令含引号时无法安全拼进 shell 字符串，跳过这条
  newpw_plain="$(printf '%s\n' "$newpw" | sed $'s/\033\[[0-9;]*m//g' |
    awk -F'新口令：' '/新口令：/ {print $2; exit}' | tr -d '[:space:]')"
  if [[ "$newpw_plain" == *"'"* || "$newpw_plain" == *'"'* || "$newpw_plain" == *'\'* ]]; then
    printf '  (新口令含引号，跳过新口令登录断言)\n'
  elif [[ -n "$newpw_plain" ]] && dexec "curl -sk -X POST https://127.0.0.1:$PORT/api/v1/auth/login \
    -H 'Content-Type: application/json' -d '{\"username\":\"admin\",\"password\":\"$newpw_plain\"}' |
    grep -q accessToken"; then
    t_pass "bh passwd 生成的新口令可登录"
  else
    t_fail "新口令无法登录（解析得到 [${newpw_plain}]）"
  fi
fi

step "校验 bh 诊断与信息命令"
check_out="$(dexec 'bh check' 2>&1 || true)"
for kw in "systemd 服务 active" "健康检查 200" "master.key 权限 600" "配置校验通过"; do
  [[ "$check_out" == *"$kw"* ]] && t_pass "bh check 输出「${kw}」" ||
    t_fail "bh check 缺少「${kw}」：$(printf '%s' "$check_out" | tr '\n' ' ' | head -c 200)"
done
dexec 'bh confcheck | grep -q 配置校验通过' && t_pass "bh confcheck 通过" || t_fail "bh confcheck 异常"
dexec 'bh cert info | grep -q 有效期至' && t_pass "bh cert info 显示有效期" || t_fail "bh cert info 异常"
dexec 'bh migrate status >/dev/null' &&
  t_pass "bh migrate status 可用" || t_fail "bh migrate status 异常"

step "校验 bh 账号与会话运维"
dexec 'bh users | grep -q admin' && t_pass "bh users 列出 admin" || t_fail "bh users 未列出 admin"
dexec 'bh unlock admin | grep -q 已解锁' && t_pass "bh unlock 可解锁账号" || t_fail "bh unlock 异常"
dexec 'bh 2fa off admin >/dev/null' && t_pass "bh 2fa off 可执行" || t_fail "bh 2fa off 异常"
dexec 'bh sessions | grep -q 用户ID' && t_pass "bh sessions 输出会话表头" || t_fail "bh sessions 异常"
dexec 'bh kick admin | grep -q 已吊销' && t_pass "bh kick 吊销会话" || t_fail "bh kick 异常"

# 锁定 → 解锁闭环：连续错误口令打到 login_fail_limit(5) 会锁号
# 直接看 bh users 的锁定列，而不依赖登录错误码（将来加验证码会换成另一个码）
for _ in 1 2 3 4 5 6; do
  dexec "curl -sk -o /dev/null -X POST https://127.0.0.1:$PORT/api/v1/auth/login \
    -H 'Content-Type: application/json' -d '{\"username\":\"admin\",\"password\":\"wrong-pw\"}'" || true
done
locked_col="$(dexec "bh users | awk '\$2==\"admin\" {print \$5}'" | tr -d '[:space:]' || true)"
if [[ -n "$locked_col" && "$locked_col" != "-" ]]; then
  t_pass "连续失败后 bh users 锁定列为 $locked_col"
else
  t_fail "账号未锁定（锁定列=[${locked_col}]）"
fi
dexec 'bh unlock admin >/dev/null'
unlocked_col="$(dexec "bh users | awk '\$2==\"admin\" {print \$5}'" | tr -d '[:space:]' || true)"
if [[ "$unlocked_col" == "-" ]]; then
  t_pass "bh unlock 后锁定列已清空"
else
  t_fail "bh unlock 后仍锁定（锁定列=[${unlocked_col}]）"
fi

step "校验 bh 配置类命令"
# 批量改配置时用 NOVA_NO_RESTART=1，最后统一重启
dexec 'NOVA_NO_RESTART=1 bh whitelist add 10.9.8.7 >/dev/null'
dexec 'bh whitelist list | grep -q 10.9.8.7' && t_pass "bh whitelist add 写入生效" || t_fail "白名单未写入"
dexec 'bh confcheck >/dev/null' && t_pass "写入白名单后配置仍合法" || t_fail "写入白名单后配置非法"
dexec 'NOVA_NO_RESTART=1 bh whitelist del 10.9.8.7 >/dev/null'
dexec 'bh whitelist list | grep -q 未设置' && t_pass "bh whitelist del 已移除" || t_fail "白名单未移除"

dexec 'NOVA_NO_RESTART=1 bh loglevel debug >/dev/null'
dexec "grep -A2 '^log:' /opt/novapanel/conf/panel.yaml | grep -q 'level: debug'" &&
  t_pass "bh loglevel 改写生效" || t_fail "bh loglevel 未生效"
dexec 'bh loglevel info >/dev/null'
dexec 'systemctl is-active --quiet novapanel' && t_pass "改日志级别后服务仍 active" || t_fail "改日志级别后服务异常"

# systemd 默认 10s 内只允许启动 5 次；连续改配置/重启不能把服务卡在 failed
for _ in 1 2 3 4 5 6; do dexec 'bh restart >/dev/null 2>&1' || true; done
dexec 'systemctl is-active --quiet novapanel' &&
  t_pass "连续 6 次重启未被 systemd 限流卡住" ||
  t_fail "连续重启后服务未运行：$(dexec 'systemctl is-active novapanel' || true)"

dexec 'bh disable >/dev/null' && ! dexec 'systemctl is-enabled --quiet novapanel' &&
  t_pass "bh disable 关闭开机自启" || t_fail "bh disable 未生效"
dexec 'bh enable >/dev/null' && dexec 'systemctl is-enabled --quiet novapanel' &&
  t_pass "bh enable 恢复开机自启" || t_fail "bh enable 未生效"

dexec 'bh cleanlog 0 >/dev/null' && t_pass "bh cleanlog 可执行" || t_fail "bh cleanlog 异常"

step "校验 bh 备份与恢复"
backup_out="$(dexec 'bh backup' 2>&1 || true)"
backup_file="$(printf '%s\n' "$backup_out" | sed $'s/\033\[[0-9;]*m//g' |
  grep -oE '/opt/novapanel/backup/novapanel-backup-[0-9-]+\.tar\.gz' | head -1)"
if [[ -n "$backup_file" ]] && dexec "test -s '$backup_file'"; then
  t_pass "bh backup 生成备份包"
else
  t_fail "bh backup 未生成备份：$(printf '%s' "$backup_out" | tr '\n' ' ' | head -c 200)"
fi
dexec "test \"\$(stat -c '%a' '$backup_file')\" = 600" &&
  t_pass "备份包权限 600（含主密钥）" || t_fail "备份包权限不安全"
dexec "tar -tzf '$backup_file' | grep -q '^conf/panel.yaml'" &&
  t_pass "备份包含配置文件" || t_fail "备份缺少配置文件"

# 改一处配置再恢复，验证备份真的能覆盖回去
dexec 'bh loglevel warn >/dev/null'
dexec "bh restore '$backup_file' -y >/dev/null" && t_pass "bh restore 执行成功" || t_fail "bh restore 失败"
dexec "grep -A2 '^log:' /opt/novapanel/conf/panel.yaml | grep -q 'level: info'" &&
  t_pass "恢复后配置回到备份时状态" || t_fail "恢复后配置未回滚"
dexec 'systemctl is-active --quiet novapanel' && t_pass "恢复后服务自动拉起" || t_fail "恢复后服务未运行"
health_restore=""
for _ in $(seq 1 15); do
  health_restore="$(dexec "curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:$PORT/api/v1/health" || true)"
  [[ "$health_restore" == "200" ]] && break
  sleep 1
done
[[ "$health_restore" == "200" ]] && t_pass "恢复后健康检查 200" || t_fail "恢复后健康检查 $health_restore"

dexec 'bh port 8443 >/dev/null 2>&1' || true
sleep 2
health8443="$(dexec "curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:8443/api/v1/health" || true)"
[[ "$health8443" == "200" ]] && t_pass "bh port 改端口后 8443 可用" || t_fail "改端口后 8443 返回 $health8443"

step "校验升级与卸载"
upgrade_log="$(dexec 'NOVA_PKG=/tmp/pkg.tar.gz bash /tmp/nova/scripts/install.sh' 2>&1 || true)"
[[ "$upgrade_log" == *"检测到已有安装"* ]] && t_pass "重复安装识别为升级" || t_fail "重复安装未走升级分支"
dexec "curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:8443/api/v1/health" |
  grep -q 200 && t_pass "升级后沿用原端口且服务正常" || t_fail "升级后服务异常"

dexec 'bh uninstall -y --purge >/dev/null'
dexec 'test ! -e /opt/novapanel' && t_pass "--purge 后安装目录已删除" || t_fail "安装目录仍存在"
dexec 'test ! -e /usr/local/bin/bh' && t_pass "bh 命令已移除" || t_fail "bh 命令仍存在"
dexec 'test ! -e /etc/systemd/system/novapanel.service' && t_pass "systemd 单元已移除" || t_fail "单元仍存在"

printf '\n通过 %d 项，失败 %d 项\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1
