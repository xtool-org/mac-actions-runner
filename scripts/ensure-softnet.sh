#!/bin/bash

set -euo pipefail

softnet_version="0.19.0"
softnet_sha256="1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
softnet_url="https://github.com/openai/softnet/releases/download/${softnet_version}/softnet.tar.gz"
install_dir="${project_dir}/.tools/softnet/${softnet_version}"
softnet_bin="${install_dir}/softnet"
require_configured=false

if [[ "${1:-}" == "--require-configured" ]]; then
  require_configured=true
elif [[ $# -ne 0 ]]; then
  echo "Usage: $0 [--require-configured]" >&2
  exit 2
fi

for command_name in curl shasum tar; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command not found: ${command_name}" >&2
    exit 1
  fi
done

if [[ ! -x "${softnet_bin}" ]]; then
  mkdir -p "${project_dir}/.tools/softnet"
  temporary_dir="$(mktemp -d "${project_dir}/.tools/softnet/.install-${softnet_version}.XXXXXX")"

  cleanup() {
    rm -rf "${temporary_dir}"
  }
  trap cleanup EXIT INT TERM

  archive="${temporary_dir}/softnet.tar.gz"
  echo "Downloading Softnet ${softnet_version}." >&2
  curl -fL --retry 3 --output "${archive}" "${softnet_url}"

  actual_sha256="$(shasum -a 256 "${archive}" | awk '{print $1}')"
  if [[ "${actual_sha256}" != "${softnet_sha256}" ]]; then
    echo "Softnet archive checksum mismatch." >&2
    echo "Expected: ${softnet_sha256}" >&2
    echo "Actual:   ${actual_sha256}" >&2
    exit 1
  fi

  tar -xzf "${archive}" -C "${temporary_dir}" softnet
  if [[ ! -x "${temporary_dir}/softnet" ]]; then
    echo "Softnet archive did not contain the expected executable." >&2
    exit 1
  fi

  mkdir -p "${install_dir}"
  mv "${temporary_dir}/softnet" "${softnet_bin}"
fi

if [[ "${require_configured}" == true ]]; then
  softnet_owner="$(stat -f '%u' "${softnet_bin}")"
  if [[ "${softnet_owner}" != 0 || ! -u "${softnet_bin}" ]]; then
    echo "Softnet needs its one-time privilege setup." >&2
    echo "Run: ${project_dir}/scripts/setup-softnet.sh" >&2
    exit 1
  fi
fi

printf '%s\n' "${softnet_bin}"
