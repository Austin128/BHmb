#!/usr/bin/env bash
# 部署脚本的单元测试：验证配置解析/改写逻辑与 systemd 模板渲染。
# 不需要 root，也不需要 Linux——只测纯文本处理，因此能在 CI 与开发机上直接跑。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAIL=0
# 函数名加 t_ 前缀：被测脚本自己也定义了 ok/warn/die，source 后会把同名函数覆盖掉
t_pass() {
  printf '✓ %s\n' "$1"
  PASS=$((PASS + 1))
}
t_fail() {
  printf '✗ %s\n' "$1" >&2
  FAIL=$((FAIL + 1))
}
t_eq() {
  local desc="$1" want="$2" got="$3"
  if [[ "$want" == "$got" ]]; then t_pass "$desc"; else t_fail "${desc}（期望 [$want]，实际 [$got]）"; fi
}
t_contains() {
  local desc="$1" haystack="$2" needle="$3"
  if [[ "$haystack" == *"$needle"* ]]; then t_pass "$desc"; else t_fail "${desc}（缺少 [$needle]）"; fi
}

# 被测脚本以 source 方式载入，只拿函数不跑主流程
export NOVA_HOME="$WORK/opt/novapanel"
mkdir -p "$NOVA_HOME/conf"
CONF="$NOVA_HOME/conf/panel.yaml"
cp "$ROOT/conf/panel.example.yaml" "$CONF"

# shellcheck source=/dev/null
source "$ROOT/scripts/install.sh"
# install.sh 自带 trap cleanup EXIT，会抢掉测试的清理，这里重新接回来
trap 'rm -rf "$WORK"' EXIT
install_set_conf_port() { set_conf_port "$@"; }
install_get_conf_port() { get_conf_port "$@"; }

echo "== install.sh 配置改写 =="

install_set_conf_port "$CONF" 8443
t_eq "server.port 被改写" "8443" "$(install_get_conf_port "$CONF")"
t_eq "agent.grpc_port 未被误改" "34568" \
  "$(awk '/^agent:[[:space:]]*$/{i=1;next} i && $1=="grpc_port:"{print $2; exit}' "$CONF")"
t_eq "monitor.collect_interval 未被误改" "15s" \
  "$(awk '/^monitor:[[:space:]]*$/{i=1;next} i && $1=="collect_interval:"{print $2; exit}' "$CONF")"
t_eq "改写后行数不变" "$(wc -l <"$ROOT/conf/panel.example.yaml")" "$(wc -l <"$CONF")"
t_eq "临时文件已清理" "0" "$(ls "$CONF.tmp" 2>/dev/null | wc -l | tr -d ' ')"

# 路径替换是安装脚本生成配置的另一半：NOVA_HOME 非默认值时必须整体改写
sed -e "s#/opt/novapanel#$NOVA_HOME#g" "$ROOT/conf/panel.example.yaml" >"$WORK/relocated.yaml"
if grep -q "^\s*path: /opt/novapanel" "$WORK/relocated.yaml"; then
  t_fail "自定义 NOVA_HOME 后仍残留 /opt/novapanel 路径"
else
  t_pass "自定义 NOVA_HOME 后路径已整体替换"
fi

echo "== bh 配置读取 =="

# bh 依赖 $NOVA_HOME 定位配置，需在 source 前设好
# shellcheck source=/dev/null
source "$ROOT/scripts/bh"

t_eq "conf_get 读 server.port" "8443" "$(conf_get port)"
t_eq "conf_get 读 server.default_locale" "zh-CN" "$(conf_get default_locale)"
t_eq "conf_get 限定段落：database 段没有 port 键" "" "$(conf_get port database || true)"
t_eq "panel_scheme 默认 https" "https" "$(panel_scheme)"

# 关掉 TLS 后应识别为 http
sed -i.bak 's/^    enabled: true/    enabled: false/' "$CONF" && rm -f "$CONF.bak"
t_eq "TLS 关闭后 panel_scheme 为 http" "http" "$(panel_scheme)"

echo "== bh 与 install.sh 端口改写行为一致 =="

cp "$ROOT/conf/panel.example.yaml" "$WORK/a.yaml"
cp "$ROOT/conf/panel.example.yaml" "$WORK/b.yaml"
(
  # shellcheck source=/dev/null
  source "$ROOT/scripts/install.sh"
  set_conf_port "$WORK/a.yaml" 9443
)
(
  # shellcheck source=/dev/null
  source "$ROOT/scripts/bh"
  set_conf_port "$WORK/b.yaml" 9443
)
if diff -q "$WORK/a.yaml" "$WORK/b.yaml" >/dev/null; then
  t_pass "两个脚本改写结果一致"
else
  t_fail "两个脚本改写结果不一致"
fi

echo "== bh 帮助与子命令同步 =="

help_text="$(cmd_help)"
for sub in start stop restart reload status log logf info passwd port update uninstall version help; do
  t_contains "help 覆盖子命令 $sub" "$help_text" "bh $sub"
done

echo "== systemd 单元渲染 =="

unit="$WORK/novapanel.service"
sed "s#__NOVA_HOME__#$NOVA_HOME#g" "$ROOT/deploy/systemd/novapanel.service" >"$unit"
unit_text="$(cat "$unit")"
if grep -q "__NOVA_HOME__" "$unit"; then
  t_fail "渲染后仍存在未替换的占位符"
else
  t_pass "占位符已全部替换"
fi
t_contains "ExecStart 指向安装目录" "$unit_text" "ExecStart=$NOVA_HOME/bin/novapanel -config $NOVA_HOME/conf/panel.yaml"
t_contains "EnvironmentFile 可选加载" "$unit_text" "EnvironmentFile=-$NOVA_HOME/conf/panel.env"
t_contains "开机自启目标" "$unit_text" "WantedBy=multi-user.target"

echo "== 变量引用与中文标点 =="

# bash 会把紧跟变量名的 UTF-8 字节当成变量名的一部分：
# "$ARCH）" 会被解析成变量 ARCH）并触发 unbound variable，必须写成 "${ARCH}）"
bad_refs="$(grep -nE '\$[A-Za-z_][A-Za-z0-9_]*(）|（|，|。|、|：|；)' \
  "$ROOT"/scripts/bh "$ROOT"/scripts/*.sh | grep -vE ':[0-9]+:[[:space:]]*#' || true)"
if [[ -z "$bad_refs" ]]; then
  t_pass "没有变量名直接紧跟全角标点的写法"
else
  t_fail "存在需要改写成 \${VAR} 的引用：
$bad_refs"
fi

printf '\n通过 %d 项，失败 %d 项\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1
