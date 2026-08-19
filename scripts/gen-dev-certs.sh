#!/usr/bin/env bash
# 生成开发用自签证书（仅供本地调试，不要用于生产）。
# 用法：./scripts/gen-dev-certs.sh [输出目录]  默认 ./.dev/certs
set -euo pipefail

OUT_DIR="${1:-./.dev/certs}"
DAYS="${DAYS:-825}"
CN="${CN:-localhost}"

command -v openssl >/dev/null 2>&1 || { echo "缺少 openssl" >&2; exit 1; }

mkdir -p "$OUT_DIR"
CRT="$OUT_DIR/panel.crt"
KEY="$OUT_DIR/panel.key"

if [[ -f "$CRT" && -f "$KEY" && "${FORCE:-}" != "1" ]]; then
  echo "证书已存在：$CRT（设置 FORCE=1 可覆盖）"
  exit 0
fi

# SAN 同时覆盖 localhost 与回环地址，浏览器与 curl 才能按主机名校验
openssl req -x509 -newkey rsa:2048 -sha256 -days "$DAYS" -nodes \
  -keyout "$KEY" -out "$CRT" \
  -subj "/C=CN/O=NovaPanel Dev/CN=$CN" \
  -addext "subjectAltName=DNS:localhost,DNS:$CN,IP:127.0.0.1,IP:::1" \
  -addext "keyUsage=digitalSignature,keyEncipherment" \
  -addext "extendedKeyUsage=serverAuth" >/dev/null 2>&1

chmod 600 "$KEY"
chmod 644 "$CRT"

echo "已生成自签证书："
echo "  证书：$CRT"
echo "  私钥：$KEY"
echo "在 conf/panel.yaml 中指向上述路径，或用环境变量："
echo "  NOVA_SERVER_TLS_CERT_FILE=$CRT NOVA_SERVER_TLS_KEY_FILE=$KEY"
