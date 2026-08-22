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
