#!/usr/bin/env bash
# NovaPanel 卸载脚本。默认保留 data/ 与 conf/，加 --purge 才彻底删除。
#   bh uninstall            停服务、删程序，保留数据与配置
#   bh uninstall --purge    连数据、配置、主密钥一起删除（不可恢复）
#   bh uninstall -y         跳过确认
set -euo pipefail

NOVA_HOME="${NOVA_HOME:-/opt/novapanel}"
SERVICE="novapanel"
UNIT_PATH="/etc/systemd/system/${SERVICE}.service"
BH_PATH="/usr/local/bin/bh"

PURGE=0
ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    --purge) PURGE=1 ;;
    -y | --yes) ASSUME_YES=1 ;;
    *)
      echo "未知参数：${arg}（可用 --purge / -y）" >&2
      exit 1
      ;;
  esac
done

C_OK=$'\033[32m'
C_WARN=$'\033[33m'
C_OFF=$'\033[0m'
ok() { printf '%s✓%s %s\n' "$C_OK" "$C_OFF" "$*"; }
warn() { printf '%s!%s %s\n' "$C_WARN" "$C_OFF" "$*" >&2; }

[[ "$(id -u)" -eq 0 ]] || {
  echo "请用 root 执行" >&2
  exit 1
}

if [[ "$PURGE" == "1" ]]; then
  warn "--purge 会删除 $NOVA_HOME 下的全部内容，包括数据库、配置与主密钥，且无法恢复"
else
  warn "将停止并移除 NovaPanel 程序，保留 $NOVA_HOME/{data,conf}"
fi

if [[ "$ASSUME_YES" != "1" ]]; then
  # curl | bash 时标准输入是脚本本身，交互确认必须直读终端
  if [[ -t 0 ]]; then
    read -r -p "确认继续？输入 yes 执行：" answer
  elif [[ -r /dev/tty ]]; then
    read -r -p "确认继续？输入 yes 执行：" answer </dev/tty
  else
    echo "非交互环境无法确认，请加 -y 后重试" >&2
    exit 1
  fi
  [[ "$answer" == "yes" ]] || {
    echo "已取消"
    exit 0
  }
fi

if systemctl list-unit-files | grep -q "^${SERVICE}.service"; then
  systemctl disable --now "$SERVICE" >/dev/null 2>&1 || true
  ok "服务已停止并取消开机自启"
fi

rm -f "$UNIT_PATH"
systemctl daemon-reload
ok "已移除 systemd 单元"

rm -f "$BH_PATH"
ok "已移除 bh 命令"

if [[ "$PURGE" == "1" ]]; then
  rm -rf "$NOVA_HOME"
  ok "已删除 $NOVA_HOME"
else
  rm -rf "$NOVA_HOME/bin" "$NOVA_HOME/scripts" "$NOVA_HOME/deploy"
  ok "程序已删除，数据与配置保留在 $NOVA_HOME"
  echo "  重新安装后会直接沿用原有配置与数据库"
fi
