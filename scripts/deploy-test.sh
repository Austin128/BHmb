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

echo "== bh 配置改写（标量 / 嵌套 / 数组）=="

cp "$ROOT/conf/panel.example.yaml" "$WORK/c.yaml"

set_conf_scalar "$WORK/c.yaml" server host 127.0.0.1
t_eq "server.host 被改写" "127.0.0.1" \
  "$(awk '/^server:[[:space:]]*$/{i=1;next} i && $1=="host:"{print $2; exit}' "$WORK/c.yaml")"
t_eq "kv.redis.addr 未被误改" "127.0.0.1:6379" \
  "$(awk '/^kv:[[:space:]]*$/{i=1;next} i && $1=="addr:"{print $2; exit}' "$WORK/c.yaml")"

set_conf_scalar "$WORK/c.yaml" log level debug
t_eq "log.level 被改写" "debug" \
  "$(awk '/^log:[[:space:]]*$/{i=1;next} i && $1=="level:"{print $2; exit}' "$WORK/c.yaml")"
t_eq "database.log_level 未被误改" "warn" \
  "$(awk '/^database:[[:space:]]*$/{i=1;next} i && $1=="log_level:"{print $2; exit}' "$WORK/c.yaml")"

set_conf_scalar "$WORK/c.yaml" server entrance /nova_test
t_eq "server.entrance 被改写" "/nova_test" \
  "$(awk '/^server:[[:space:]]*$/{i=1;next} i && $1=="entrance:"{print $2; exit}' "$WORK/c.yaml")"

set_conf_nested "$WORK/c.yaml" server tls enabled false
t_eq "server.tls.enabled 被改写" "false" \
  "$(awk '/^[[:space:]]*tls:[[:space:]]*$/{i=1;next} i && $1=="enabled:"{print $2; exit}' "$WORK/c.yaml")"
t_eq "database.auto_migrate 未被误改" "true" \
  "$(awk '/^database:[[:space:]]*$/{i=1;next} i && $1=="auto_migrate:"{print $2; exit}' "$WORK/c.yaml")"
t_eq "改写后行数不变" "$(wc -l <"$ROOT/conf/panel.example.yaml")" "$(wc -l <"$WORK/c.yaml")"

# 白名单是数组，行内 [] 与多行列表两种形态都要能读写
CONF="$WORK/c.yaml"
t_eq "空白名单读出为空" "" "$(whitelist_items)"
write_whitelist "$CONF" "10.0.0.1 192.168.1.0/24"
t_eq "写入两条后可读回" "10.0.0.1 192.168.1.0/24" "$(whitelist_items | tr '\n' ' ' | sed 's/ *$//')"
t_eq "写入白名单不破坏后续键" "NovaPanel" \
  "$(awk '/^security:[[:space:]]*$/{i=1;next} i && $1=="totp_issuer:"{print $2; exit}' "$CONF")"
write_whitelist "$CONF" ""
t_eq "清空后回到行内空数组" "" "$(whitelist_items)"
t_contains "清空后写成 []" "$(grep 'ip_whitelist' "$CONF")" "ip_whitelist: []"

cert_out="$(cert_files)"
t_contains "cert_files 解析出证书路径" "$cert_out" "panel.crt"
t_contains "cert_files 解析出私钥路径" "$cert_out" "panel.key"

# 后续用例仍按原配置断言，这里恢复指向
CONF="$NOVA_HOME/conf/panel.yaml"

echo "== bh 备份包校验 =="

# 正常包：曾因 pipefail 下 grep -q 早退让 tar 收到 SIGPIPE，正常备份被误判为损坏
mkdir -p "$WORK/pkg/conf" "$WORK/pkg/data"
cp "$ROOT/conf/panel.example.yaml" "$WORK/pkg/conf/panel.yaml"
head -c 64 /dev/urandom >"$WORK/pkg/data/panel.db"
tar -czf "$WORK/good.tar.gz" -C "$WORK/pkg" conf data
if (check_backup "$WORK/good.tar.gz") >/dev/null 2>&1; then
  t_pass "check_backup 接受正常备份包"
else
  t_fail "check_backup 误判正常备份包"
fi

printf 'not a tarball' >"$WORK/broken.tar.gz"
if (check_backup "$WORK/broken.tar.gz") >/dev/null 2>&1; then
  t_fail "check_backup 放过了损坏文件"
else
  t_pass "check_backup 拒绝损坏文件"
fi

mkdir -p "$WORK/nocfg/data"
tar -czf "$WORK/nocfg.tar.gz" -C "$WORK/nocfg" data
if (check_backup "$WORK/nocfg.tar.gz") >/dev/null 2>&1; then
  t_fail "check_backup 放过了缺少 conf/ 的包"
else
  t_pass "check_backup 拒绝缺少 conf/ 的包"
fi

# 路径穿越：解包目标是 $NOVA_HOME，../ 条目会写到安装目录之外
tar -czf "$WORK/evil.tar.gz" -C "$WORK/pkg" conf ../pkg/data 2>/dev/null ||
  tar -czf "$WORK/evil.tar.gz" -C "$WORK" pkg/conf ../"$(basename "$WORK")"/pkg/data 2>/dev/null || true
if [[ -f "$WORK/evil.tar.gz" ]] && tar -tzf "$WORK/evil.tar.gz" 2>/dev/null | grep -q '\.\.'; then
  if (check_backup "$WORK/evil.tar.gz") >/dev/null 2>&1; then
    t_fail "check_backup 放过了含 ../ 的包"
  else
    t_pass "check_backup 拒绝含 ../ 的包"
  fi
else
  printf '  (当前 tar 不保留 ../ 条目，跳过路径穿越用例)\n'
fi

echo "== bh 帮助与子命令同步 =="

help_text="$(cmd_help)"
for sub in start stop restart reload status enable disable log logf info check version \
  cert confcheck passwd users unlock 2fa sessions kick whitelist ssl port host entrance \
  conf loglevel cleanlog backup restore migrate update uninstall help; do
  t_contains "help 覆盖子命令 $sub" "$help_text" "bh $sub"
done

# help 里写了但 main 没接的命令等于骗人，反向也要查
dispatch="$(sed -n '/^main() {/,/^}/p' "$ROOT/scripts/bh")"
for sub in enable disable check users unlock 2fa sessions kick whitelist ssl host entrance \
  confcheck loglevel cleanlog backup restore migrate cert; do
  t_contains "main 分发含 $sub" "$dispatch" "$sub)"
done

# 交互菜单里列出的编号必须都有对应分支，否则选了没反应
menu_body="$(sed -n '/^menu() {/,/^}/p' "$ROOT/scripts/bh")"
menu_nums="$(printf '%s\n' "$menu_body" | grep -oE '^\s+\(([0-9]+)\)' | grep -oE '[0-9]+')"
missing_case=""
while read -r n; do
  [[ -n "$n" ]] || continue
  printf '%s\n' "$menu_body" | grep -qE "^[[:space:]]+${n}(\)| \|)" || missing_case="$missing_case $n"
done <<<"$menu_nums"
if [[ -z "$missing_case" ]]; then
  t_pass "菜单编号都有对应分支（共 $(printf '%s\n' "$menu_nums" | grep -c . ) 项）"
else
  t_fail "菜单编号缺少分支：$missing_case"
fi

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

echo "== curl | bash 管道执行 =="

# 管道执行时脚本来自标准输入，BASH_SOURCE 为空数组；set -u 下直接展开会报
# "BASH_SOURCE[0]: unbound variable" 并且 main 根本不会跑，安装静默失败。
for script in install.sh uninstall.sh; do
  piped="$(cat "$ROOT/scripts/$script" | bash 2>&1 || true)"
  if [[ "$piped" == *"unbound variable"* ]]; then
    t_fail "$script 管道执行报未绑定变量：$piped"
  else
    t_contains "$script 管道执行进入主流程（被 root 检查拦下）" "$piped" "root"
  fi
done

# 反过来，source 时不能跑主流程，否则 deploy-test 自己就会被 precheck 打断
sourced="$(bash -c "source '$ROOT/scripts/install.sh'; type -t main" 2>&1 || true)"
t_eq "install.sh 被 source 时只导出函数" "function" "$sourced"

echo "== 变量引用与中文标点 =="

# bash 会把紧跟变量名的 UTF-8 字节当成变量名的一部分：
# "$ARCH）" 会被解析成变量 ARCH）并触发 unbound variable，必须写成 "${ARCH}）"
bad_refs="$(grep -nE '\$[A-Za-z_][A-Za-z0-9_]*(）|（|，|。|、|：|；|」|「|【|】|“|”)' \
  "$ROOT"/scripts/bh "$ROOT"/scripts/*.sh | grep -vE ':[0-9]+:[[:space:]]*#' || true)"
if [[ -z "$bad_refs" ]]; then
  t_pass "没有变量名直接紧跟全角标点的写法"
else
  t_fail "存在需要改写成 \${VAR} 的引用：
$bad_refs"
fi

printf '\n通过 %d 项，失败 %d 项\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1
