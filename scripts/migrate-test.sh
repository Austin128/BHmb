#!/usr/bin/env bash
# 迁移演练：对 sqlite 执行 up -> down(全部) -> up，验证每个迁移都有可用的回滚。
# 若提供以下环境变量，则同样对 MySQL / PostgreSQL 演练（CI 里由 service 容器提供）：
#   MYSQL_DSN="nova:nova@tcp(127.0.0.1:3306)/nova_test?charset=utf8mb4&parseTime=True&loc=Local"
#   POSTGRES_DSN="host=127.0.0.1 user=nova password=nova dbname=nova_test port=5432 sslmode=disable"
# 注意：脚本会清空目标库中的业务表，只能指向一次性测试库。
set -euo pipefail

cd "$(dirname "$0")/.."

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

export NOVA_CONFIRM_DOWN=yes
export NOVA_LOG_CONSOLE=true
export NOVA_LOG_DIR="$WORK_DIR/logs"
export NOVA_KV_PATH="$WORK_DIR/kv.bolt"
export NOVA_SECURITY_MASTER_KEY_FILE="$WORK_DIR/master.key"
export NOVA_SERVER_TLS_ENABLED=false
mkdir -p "$NOVA_LOG_DIR"

# -c "" 表示不读配置文件，仅用默认值 + NOVA_ 环境变量（演练环境全部走临时目录）
nova() {
  go run ./cmd/novactl "$@" -c ""
}

# 统计迁移文件数量（*.up.sql），用于确认全部回滚
migration_count() {
  find migrations -name '*.up.sql' | wc -l | tr -d ' '
}

run_cycle() {
  local label="$1"

  echo "==> [$label] migrate up"
  nova migrate up

  echo "==> [$label] migrate status"
  nova migrate status

  local total
  total="$(migration_count)"
  echo "==> [$label] migrate down -step $total"
  nova migrate down -step "$total"

  # 回滚后不应残留已应用记录
  if nova migrate status | grep -q "已应用"; then
    echo "[$label] 回滚后仍存在已应用迁移" >&2
    return 1
  fi

  echo "==> [$label] migrate up（二次）"
  nova migrate up

  echo "==> [$label] seed"
  nova seed

  echo "[$label] 迁移演练通过"
}

# ---- sqlite ----
export NOVA_DATABASE_DRIVER=sqlite
export NOVA_DATABASE_PATH="$WORK_DIR/nova.db"
export NOVA_DATABASE_DSN=""
run_cycle sqlite

# ---- mysql（可选）----
if [[ -n "${MYSQL_DSN:-}" ]]; then
  export NOVA_DATABASE_DRIVER=mysql
  export NOVA_DATABASE_DSN="$MYSQL_DSN"
  run_cycle mysql
else
  echo "跳过 mysql：未设置 MYSQL_DSN"
fi

# ---- postgres（可选）----
if [[ -n "${POSTGRES_DSN:-}" ]]; then
  export NOVA_DATABASE_DRIVER=postgres
  export NOVA_DATABASE_DSN="$POSTGRES_DSN"
  run_cycle postgres
else
  echo "跳过 postgres：未设置 POSTGRES_DSN"
fi

echo "迁移演练全部通过"
