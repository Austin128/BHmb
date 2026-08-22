#!/usr/bin/env bash
# NovaPanel 一键安装脚本（Linux + systemd）。
#
#   curl -fsSL https://raw.githubusercontent.com/Austin128/BHmb/main/scripts/install.sh | sudo bash
#
# 可用环境变量：
#   NOVA_HOME=/opt/novapanel   安装目录
#   NOVA_PORT=34567            面板端口
#   NOVA_VERSION=v0.1.0        指定版本（默认取最新 release）
#   NOVA_PKG=/path/pkg.tar.gz  用本地发布包安装（离线环境）
#   NOVA_FROM_SOURCE=1         在仓库目录内从源码构建安装（需 Go 与 pnpm）
#   NOVA_UPGRADE=1             只替换程序，不动配置与数据
set -euo pipefail

NOVA_HOME="${NOVA_HOME:-/opt/novapanel}"
NOVA_PORT="${NOVA_PORT:-34567}"
NOVA_VERSION="${NOVA_VERSION:-}"
NOVA_PKG="${NOVA_PKG:-}"
NOVA_FROM_SOURCE="${NOVA_FROM_SOURCE:-0}"
NOVA_UPGRADE="${NOVA_UPGRADE:-0}"
REPO="Austin128/BHmb"
SERVICE="novapanel"
UNIT_PATH="/etc/systemd/system/${SERVICE}.service"
BH_PATH="/usr/local/bin/bh"

C_OK=$'\033[32m'
C_WARN=$'\033[33m'
C_ERR=$'\033[31m'
C_KEY=$'\033[36m'
C_OFF=$'\033[0m'
step() { printf '%s==>%s %s\n' "$C_KEY" "$C_OFF" "$*"; }
ok() { printf '%s✓%s %s\n' "$C_OK" "$C_OFF" "$*"; }
warn() { printf '%s!%s %s\n' "$C_WARN" "$C_OFF" "$*" >&2; }
die() {
  printf '%s✗%s %s\n' "$C_ERR" "$C_OFF" "$*" >&2
  exit 1
}

WORK_DIR=""
cleanup() { [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]] && rm -rf "$WORK_DIR" || true; }
trap cleanup EXIT

# ---------- 环境检查 ----------
precheck() {
  [[ "$(id -u)" -eq 0 ]] || die "请用 root 执行（sudo bash install.sh）"
  [[ "$(uname -s)" == "Linux" ]] || die "仅支持 Linux，当前系统：$(uname -s)"
  command -v systemctl >/dev/null 2>&1 || die "需要 systemd，未找到 systemctl"
  [[ "$NOVA_PORT" =~ ^[0-9]+$ ]] || die "NOVA_PORT 必须是数字"

  case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *) die "暂不支持的架构：$(uname -m)（仅 amd64 / arm64）" ;;
  esac

  if command -v ss >/dev/null 2>&1 && ss -ltn "sport = :$NOVA_PORT" 2>/dev/null | grep -q LISTEN; then
    [[ "$NOVA_UPGRADE" == "1" ]] || die "端口 $NOVA_PORT 已被占用，请用 NOVA_PORT=其它端口 重试"
  fi
  ok "系统检查通过（linux/${ARCH}）"
}

ensure_deps() {
  local missing=()
  command -v tar >/dev/null 2>&1 || missing+=(tar)
  command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 || missing+=(curl)
  [[ ${#missing[@]} -eq 0 ]] && return 0

  step "安装依赖：${missing[*]}"
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update -qq && apt-get install -y -qq "${missing[@]}"
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y -q "${missing[@]}"
  elif command -v yum >/dev/null 2>&1; then
    yum install -y -q "${missing[@]}"
  else
    die "缺少 ${missing[*]}，且未识别包管理器，请手动安装后重试"
  fi
}

fetch() {
  local url="$1" out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  else
    wget -qO "$out" "$url"
  fi
}

# ---------- 取得安装内容到 $WORK_DIR/payload ----------
prepare_payload() {
  WORK_DIR="$(mktemp -d)"
  local payload="$WORK_DIR/payload"
  mkdir -p "$payload"

  if [[ -n "$NOVA_PKG" ]]; then
    step "使用本地发布包：$NOVA_PKG"
    [[ -f "$NOVA_PKG" ]] || die "本地包不存在：$NOVA_PKG"
    tar -xzf "$NOVA_PKG" -C "$payload"
  elif [[ "$NOVA_FROM_SOURCE" == "1" ]]; then
    build_from_source "$payload"
  elif download_release "$payload"; then
    :
  else
    warn "在线发布包不可用，尝试从源码构建"
    build_from_source "$payload"
  fi

  [[ -x "$payload/novapanel" || -x "$payload/bin/novapanel" ]] ||
    die "安装内容里没有 novapanel 可执行文件"
  PAYLOAD="$payload"
}

download_release() {
  local payload="$1" tag="$NOVA_VERSION" url pkg
  step "获取发布包"
  if [[ -z "$tag" ]]; then
    fetch "https://api.github.com/repos/${REPO}/releases/latest" "$WORK_DIR/latest.json" 2>/dev/null || return 1
    tag="$(grep -m1 '"tag_name"' "$WORK_DIR/latest.json" | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  fi
  [[ -n "$tag" ]] || return 1

  pkg="novapanel-${tag}-linux-${ARCH}.tar.gz"
  url="https://github.com/${REPO}/releases/download/${tag}/${pkg}"
  fetch "$url" "$WORK_DIR/$pkg" 2>/dev/null || return 1

  # 校验和存在则必须匹配，避免装上被篡改的包
  if fetch "https://github.com/${REPO}/releases/download/${tag}/SHA256SUMS" "$WORK_DIR/SHA256SUMS" 2>/dev/null &&
    command -v sha256sum >/dev/null 2>&1; then
    (cd "$WORK_DIR" && grep " ${pkg}\$" SHA256SUMS | sha256sum -c -) ||
      die "发布包校验和不匹配，已中止安装"
    ok "发布包校验通过"
  else
    warn "未获取到 SHA256SUMS，跳过校验和比对"
  fi

  tar -xzf "$WORK_DIR/$pkg" -C "$payload"
  ok "已下载 $tag"
}

build_from_source() {
  local payload="$1" root
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  [[ -f "$root/go.mod" ]] || die "未在仓库目录内，无法从源码构建；请改用 NOVA_PKG 指定本地发布包"
  command -v go >/dev/null 2>&1 || die "从源码构建需要 Go 1.24+"
  command -v pnpm >/dev/null 2>&1 || die "从源码构建需要 pnpm（前端产物要嵌入二进制）"

  step "从源码构建（${root}）"
  (cd "$root" && make web build)
  mkdir -p "$payload/bin" "$payload/conf" "$payload/scripts" "$payload/deploy/systemd"
  cp "$root/bin/novapanel" "$root/bin/novactl" "$payload/bin/"
  if [[ -f "$root/bin/nova-agent" ]]; then cp "$root/bin/nova-agent" "$payload/bin/"; fi
  cp "$root/conf/panel.example.yaml" "$payload/conf/"
  cp "$root/scripts/bh" "$root/scripts/install.sh" "$root/scripts/uninstall.sh" "$payload/scripts/"
  cp "$root/deploy/systemd/novapanel.service" "$payload/deploy/systemd/"
  ok "源码构建完成"
}

# ---------- 安装 ----------
install_files() {
  step "安装到 $NOVA_HOME"
  install -d -m 0755 "$NOVA_HOME"/{bin,conf,scripts,deploy/systemd}
  install -d -m 0750 "$NOVA_HOME"/{data,logs}
  install -d -m 0700 "$NOVA_HOME/conf/certs"

  local bindir="$PAYLOAD/bin"
  [[ -d "$bindir" ]] || bindir="$PAYLOAD" # 兼容 tar 内直接平铺二进制的情况

  install -m 0755 "$bindir/novapanel" "$NOVA_HOME/bin/novapanel"
  install -m 0755 "$bindir/novactl" "$NOVA_HOME/bin/novactl"
  if [[ -f "$bindir/nova-agent" ]]; then
    install -m 0755 "$bindir/nova-agent" "$NOVA_HOME/bin/nova-agent"
  fi

  # 示例配置与管理脚本每次安装都刷新
  if [[ -f "$PAYLOAD/conf/panel.example.yaml" ]]; then
    install -m 0644 "$PAYLOAD/conf/panel.example.yaml" "$NOVA_HOME/conf/panel.example.yaml"
  fi
  for s in bh install.sh uninstall.sh; do
    if [[ -f "$PAYLOAD/scripts/$s" ]]; then
      install -m 0755 "$PAYLOAD/scripts/$s" "$NOVA_HOME/scripts/$s"
    fi
  done
  if [[ -f "$PAYLOAD/deploy/systemd/novapanel.service" ]]; then
    install -m 0644 "$PAYLOAD/deploy/systemd/novapanel.service" "$NOVA_HOME/deploy/systemd/novapanel.service"
  fi

  ln -sf "$NOVA_HOME/scripts/bh" "$BH_PATH"
  ok "程序与管理命令已就位（bh -> $NOVA_HOME/scripts/bh）"
}

# 只改 server 段的第一处 port，避开 agent.grpc_port / redis 等其它段
set_conf_port() {
  local conf="$1" port="$2"
  awk -v p="$port" '
    /^server:[[:space:]]*$/ { inside = 1; print; next }
    inside && $1 == "port:" && !done { sub(/port:.*/, "port: " p); done = 1 }
    /^[a-z_]+:[[:space:]]*$/ && !/^server:/ { inside = 0 }
    { print }
  ' "$conf" >"$conf.tmp" && mv "$conf.tmp" "$conf"
}

# 从现有配置里读 server.port
get_conf_port() {
  awk '/^server:[[:space:]]*$/{i=1;next} i && $1=="port:"{print $2; exit}' "$1"
}

write_config() {
  local conf="$NOVA_HOME/conf/panel.yaml"
  if [[ -f "$conf" ]]; then
    ok "沿用已有配置 $conf"
    return
  fi
  step "生成配置 $conf"
  [[ -f "$NOVA_HOME/conf/panel.example.yaml" ]] || die "缺少 conf/panel.example.yaml，无法生成配置"

  sed -e "s#/opt/novapanel#${NOVA_HOME}#g" "$NOVA_HOME/conf/panel.example.yaml" >"$conf"
  set_conf_port "$conf" "$NOVA_PORT"
  chmod 0640 "$conf"
  ok "配置已生成（端口 ${NOVA_PORT}，TLS 自签证书自动生成）"
}

write_unit() {
  step "注册 systemd 服务"
  local tpl="$NOVA_HOME/deploy/systemd/novapanel.service"
  [[ -f "$tpl" ]] || die "缺少服务单元模板：$tpl"
  sed "s#__NOVA_HOME__#${NOVA_HOME}#g" "$tpl" >"$UNIT_PATH"
  chmod 0644 "$UNIT_PATH"
  systemctl daemon-reload
  systemctl enable "$SERVICE" >/dev/null 2>&1 || true
  ok "服务已注册并设为开机自启"
}

# 首次安装时用 EnvironmentFile 注入初始口令，服务起来后立刻清空该文件
seed_admin_password() {
  ADMIN_PASSWORD=""
  [[ "$FRESH_INSTALL" == "1" ]] || return 0
  ADMIN_PASSWORD="$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 16 || true)"
  [[ -n "$ADMIN_PASSWORD" ]] || die "无法生成初始口令"
  umask 077
  printf 'NOVA_INITIAL_ADMIN_PASSWORD=%s\n' "$ADMIN_PASSWORD" >"$NOVA_HOME/conf/panel.env"
  chmod 0600 "$NOVA_HOME/conf/panel.env"
}

clear_password_env() {
  [[ -f "$NOVA_HOME/conf/panel.env" ]] || return 0
  : >"$NOVA_HOME/conf/panel.env"
  chmod 0600 "$NOVA_HOME/conf/panel.env"
}

start_service() {
  step "启动面板"
  systemctl restart "$SERVICE"

  local scheme="https" i
  if grep -qE '^\s{4}enabled:\s*false' "$NOVA_HOME/conf/panel.yaml"; then scheme="http"; fi
  for i in $(seq 1 30); do
    if command -v curl >/dev/null 2>&1 &&
      curl -fsk --max-time 2 "${scheme}://127.0.0.1:${NOVA_PORT}/api/v1/health" >/dev/null 2>&1; then
      ok "健康检查通过（第 ${i} 次探测）"
      SCHEME="$scheme"
      return 0
    fi
    systemctl is-active --quiet "$SERVICE" || break
    sleep 1
  done

  SCHEME="$scheme"
  warn "健康检查未通过，请执行 journalctl -u ${SERVICE} -n 100 查看原因"
}

open_firewall() {
  if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    firewall-cmd --permanent --add-port="${NOVA_PORT}/tcp" >/dev/null && firewall-cmd --reload >/dev/null
    ok "firewalld 已放行 ${NOVA_PORT}/tcp"
  elif command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow "${NOVA_PORT}/tcp" >/dev/null
    ok "ufw 已放行 ${NOVA_PORT}/tcp"
  else
    warn "未检测到活动防火墙，若使用云厂商安全组请手动放行 ${NOVA_PORT}/tcp"
  fi
}

# 取本机内网 IP：最小安装的系统可能既没 ip 也没 hostname，因此全路径容错
lan_ip() {
  local addr=""
  if command -v ip >/dev/null 2>&1; then
    addr="$(ip -4 route get 1.1.1.1 2>/dev/null |
      awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}' || true)"
  fi
  if [[ -z "$addr" ]] && command -v hostname >/dev/null 2>&1; then
    addr="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
  fi
  printf '%s' "$addr"
}

summary() {
  local ip
  ip="$(lan_ip)"
  [[ -n "$ip" ]] || ip="<服务器IP>"

  printf '\n%s================ NovaPanel 安装完成 ================%s\n' "$C_OK" "$C_OFF"
  printf '面板地址： %s%s://%s:%s/%s\n' "$C_KEY" "${SCHEME:-https}" "$ip" "$NOVA_PORT" "$C_OFF"
  printf '管理账号： %sadmin%s\n' "$C_KEY" "$C_OFF"
  if [[ -n "${ADMIN_PASSWORD:-}" ]]; then
    printf '初始口令： %s%s%s   （仅显示这一次，请立刻保存并登录后修改）\n' "$C_KEY" "$ADMIN_PASSWORD" "$C_OFF"
  else
    printf '初始口令： 沿用原有口令（忘记可执行 %sbh passwd%s 重置）\n' "$C_KEY" "$C_OFF"
  fi
  printf '管理命令： %sbh%s（等同宝塔的 bt，直接运行进入菜单，bh help 看全部子命令）\n' "$C_KEY" "$C_OFF"
  printf '安装目录： %s\n' "$NOVA_HOME"
  printf '\n'
  warn "面板默认监听 0.0.0.0 并使用自签证书：浏览器会提示证书不受信；生产环境请换受信证书，并用安全组或 security.ip_whitelist 限制来源 IP"
  warn "主密钥 $NOVA_HOME/conf/master.key 丢失将导致所有会话失效，请纳入备份"
  printf '\n'
}

main() {
  precheck
  ensure_deps

  FRESH_INSTALL=1
  if [[ -f "$NOVA_HOME/conf/panel.yaml" ]]; then
    FRESH_INSTALL=0
    step "检测到已有安装，执行升级（配置与数据保持不变）"
    NOVA_PORT="$(get_conf_port "$NOVA_HOME/conf/panel.yaml")"
    [[ -n "$NOVA_PORT" ]] || die "无法从现有配置解析端口"
  fi

  prepare_payload
  install_files
  write_config
  write_unit
  seed_admin_password
  start_service
  clear_password_env
  open_firewall
  summary
}

# 被 source 时只导出函数，便于 scripts/deploy-test.sh 单测配置写入逻辑
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
