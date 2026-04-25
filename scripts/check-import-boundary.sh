#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

SERVICES=(
  "gateway"
  "order-service"
  "product-service"
  "seckill-service"
  "user-service"
)

if ! command -v rg >/dev/null 2>&1; then
  echo "[ERROR] ripgrep (rg) is required but not found."
  exit 2
fi

has_violation=0

for svc in "${SERVICES[@]}"; do
  svc_dir="${ROOT_DIR}/${svc}"
  if [[ ! -d "${svc_dir}" ]]; then
    continue
  fi

  forbidden=()
  for other in "${SERVICES[@]}"; do
    if [[ "${other}" != "${svc}" ]]; then
      forbidden+=("seckill-mall/${other}")
    fi
  done

  # Build regex: "seckill-mall/gateway|seckill-mall/order-service|..."
  forbidden_pattern="$(IFS='|'; echo "${forbidden[*]}")"

  # Only check service source tree itself; allow common imports globally.
  # Any import to another service module is considered an architecture violation.
  matches="$(
    rg -n --no-heading --glob '*.go' "\"(${forbidden_pattern})(/[^\"\\s]*)?\"" "${svc_dir}" || true
  )"

  if [[ -n "${matches}" ]]; then
    has_violation=1
    echo "[VIOLATION] ${svc} imports another service module directly:"
    echo "${matches}"
    echo
  fi
done

if [[ "${has_violation}" -ne 0 ]]; then
  echo "[FAIL] import boundary check failed."
  echo "Policy: each service may import only itself (seckill-mall/<self>/...) and common (seckill-mall/common/...)."
  exit 1
fi

echo "[PASS] import boundary check passed."
