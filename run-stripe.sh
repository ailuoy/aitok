#!/usr/bin/env bash
set -Eeuo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
for env_file in "$ROOT_DIR/.env" "$ROOT_DIR/backend/.env"; do
  if [ -f "$env_file" ]; then
    set -a
    # shellcheck disable=SC1090
    source "$env_file"
    set +a
  fi
done
command -v stripe >/dev/null || { echo "请先安装 Stripe CLI" >&2; exit 1; }
if [[ "${STRIPE_SECRET_KEY:-}" != sk_test_* ]]; then
  echo "本地转发脚本需要 STRIPE_SECRET_KEY 沙盒密钥" >&2
  exit 1
fi
export STRIPE_API_KEY="$STRIPE_SECRET_KEY"
exec stripe listen --skip-update \
  --events checkout.session.completed,checkout.session.async_payment_succeeded,checkout.session.async_payment_failed,checkout.session.expired \
  --forward-to "http://localhost:${BACKEND_PORT:-15681}/api/stripe/webhook"
