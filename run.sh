#!/bin/bash

set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "${project_dir}"

if [[ "${TART_BIN:-auto}" == auto ]]; then
  TART_BIN="$("${project_dir}/scripts/ensure-tart.sh")"
fi
export TART_BIN

if [[ "${TART_NETWORK_MODE:-softnet}" == softnet ]]; then
  SOFTNET_BIN="$("${project_dir}/scripts/ensure-softnet.sh" --require-configured)"
  PATH="$(dirname -- "${SOFTNET_BIN}"):${PATH}"
  export SOFTNET_BIN PATH
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Required command not found: go" >&2
  exit 1
fi

controller_bin="${project_dir}/.tools/bin/tartscaleset"
mkdir -p "$(dirname -- "${controller_bin}")"
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "${controller_bin}" ./cmd/tartscaleset
exec "${controller_bin}"
