#!/bin/bash

set -euo pipefail

tart_version="2.32.1"

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tart_url="https://github.com/openai/tart/releases/download/${tart_version}/tart.tar.gz"
install_dir="${project_dir}/.tools/tart/${tart_version}"
tart_bin="${install_dir}/tart.app/Contents/MacOS/tart"

if [[ -x "${tart_bin}" ]]; then
  printf '%s\n' "${tart_bin}"
  exit 0
fi

for command_name in curl shasum tar; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "Required command not found: ${command_name}" >&2
    exit 1
  fi
done

mkdir -p "${project_dir}/.tools/tart"
temporary_dir="$(mktemp -d "${project_dir}/.tools/tart/.install-${tart_version}.XXXXXX")"

cleanup() {
  rm -rf "${temporary_dir}"
}
trap cleanup EXIT INT TERM

archive="${temporary_dir}/tart.tar.gz"
echo "Downloading Tart ${tart_version}." >&2
curl -fL --retry 3 --output "${archive}" "${tart_url}"

tar -xzf "${archive}" -C "${temporary_dir}"
if [[ ! -x "${temporary_dir}/tart.app/Contents/MacOS/tart" ]]; then
  echo "Tart archive did not contain the expected executable." >&2
  exit 1
fi

mkdir -p "${install_dir}"
mv "${temporary_dir}/tart.app" "${install_dir}/tart.app"

printf '%s\n' "${tart_bin}"
