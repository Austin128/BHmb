#!/usr/bin/env bash
# 校验错误码三处一致：
#   1) internal/pkg/errs/code.go 的常量定义
#   2) internal/pkg/errs/status.go 的 statusMap（HTTP 状态映射）
#   3) docs/05-API接口规范.md 的错误码表格
# 任一处缺失即视为不一致并以非 0 退出，供 CI 门禁使用。
set -euo pipefail

cd "$(dirname "$0")/.."

CODE_FILE="internal/pkg/errs/code.go"
STATUS_FILE="internal/pkg/errs/status.go"
DOC_FILE="docs/05-API接口规范.md"

for f in "$CODE_FILE" "$STATUS_FILE" "$DOC_FILE"; do
  [[ -f "$f" ]] || { echo "缺少文件：$f" >&2; exit 1; }
done

# 常量名 + 数值，形如：CodeInvalidParam     = 100001
# 不用 mapfile：macOS 自带 bash 3.2 不支持
entries=()
while IFS= read -r line; do
  entries+=("$line")
done < <(grep -Eo '^[[:space:]]+Code[A-Za-z0-9]+[[:space:]]+=[[:space:]]+[0-9]{6}' "$CODE_FILE" |
  sed -E 's/^[[:space:]]+([A-Za-z0-9]+)[[:space:]]+=[[:space:]]+([0-9]{6})/\1 \2/')

if [[ ${#entries[@]} -eq 0 ]]; then
  echo "未从 $CODE_FILE 解析到任何错误码" >&2
  exit 1
fi

fail=0
for entry in "${entries[@]}"; do
  name="${entry%% *}"
  code="${entry##* }"

  if ! grep -qE "^[[:space:]]+${name}:" "$STATUS_FILE"; then
    echo "statusMap 缺少映射：${name} (${code})" >&2
    fail=1
  fi

  if ! grep -q "${code}" "$DOC_FILE"; then
    echo "文档缺少错误码：${code} (${name}) -> ${DOC_FILE}" >&2
    fail=1
  fi
done

# 反向：statusMap 里不得出现未定义常量
mapped=()
while IFS= read -r name; do
  mapped+=("$name")
done < <(grep -Eo '^[[:space:]]+Code[A-Za-z0-9]+:' "$STATUS_FILE" | sed -E 's/^[[:space:]]+//; s/:$//')
for name in "${mapped[@]}"; do
  if ! grep -qE "^[[:space:]]+${name}[[:space:]]+=" "$CODE_FILE"; then
    echo "statusMap 引用了未定义常量：${name}" >&2
    fail=1
  fi
done

if [[ $fail -ne 0 ]]; then
  echo "错误码一致性校验失败" >&2
  exit 1
fi

echo "错误码一致性校验通过：${#entries[@]} 个错误码"
