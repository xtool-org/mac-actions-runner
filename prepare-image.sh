#!/bin/bash

set -euo pipefail

image_version=1

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "${project_dir}"

# shellcheck source=scripts/common.sh
source "${project_dir}/scripts/common.sh"

TART_BIN="$("${project_dir}/scripts/ensure-tart.sh")"
export TART_BIN
configure_network_helper

mkdir -p "${project_dir}/.state"
image_version_file="${project_dir}/.state/${TART_BASE_VM}.image-version"
installed_image_version=""
if [[ -f "${image_version_file}" ]]; then
  IFS= read -r installed_image_version <"${image_version_file}" || true
fi

base_vm_exists=false
if vm_exists "${TART_BASE_VM}"; then
  base_vm_exists=true
  if [[ "${installed_image_version}" == "${image_version}" ]]; then
    echo "Base VM ${TART_BASE_VM} is current at image version ${image_version}."
    exit 0
  fi

  if [[ -n "${installed_image_version}" ]]; then
    echo "Base VM ${TART_BASE_VM} image version ${installed_image_version} is outdated; rebuilding version ${image_version}."
  else
    echo "Base VM ${TART_BASE_VM} has no image version; rebuilding version ${image_version}."
  fi
else
  echo "Base VM ${TART_BASE_VM} does not exist; building image version ${image_version}."
fi

staging_vm="${TART_BASE_VM}-preparing-$$"
tart_pid=""
published=false

cleanup() {
  if [[ "${published}" == true ]]; then
    return
  fi

  if [[ -n "${tart_pid}" ]]; then
    "${TART_BIN}" stop "${staging_vm}" --timeout 10 >/dev/null 2>&1 || true
    wait "${tart_pid}" 2>/dev/null || true
  fi
  if vm_exists "${staging_vm}"; then
    "${TART_BIN}" delete "${staging_vm}"
  fi
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Cloning ${TART_SOURCE_IMAGE} as ${staging_vm}."
"${TART_BIN}" clone "${TART_SOURCE_IMAGE}" "${staging_vm}"
"${TART_BIN}" set "${staging_vm}" \
  --cpu "${TART_CPU}" \
  --memory "${TART_MEMORY_MB}" \
  --disk-size "${TART_DISK_GB}"

echo "Booting ${staging_vm} for provisioning."
tart_run_command=("${TART_BIN}" run --no-graphics)
case "${TART_NETWORK_MODE}" in
  shared)
    ;;
  softnet)
    tart_run_command+=(--net-softnet)
    if [[ -n "${TART_SOFTNET_ALLOW}" ]]; then
      tart_run_command+=("--net-softnet-allow=${TART_SOFTNET_ALLOW}")
    fi
    ;;
  host)
    tart_run_command+=(--net-host)
    ;;
  *)
    echo "Unsupported TART_NETWORK_MODE: ${TART_NETWORK_MODE}" >&2
    exit 1
    ;;
esac

"${tart_run_command[@]}" "${staging_vm}" &
tart_pid=$!

if ! wait_for_guest "${staging_vm}" "${VM_READY_TIMEOUT_SECONDS}"; then
  echo "VM did not become ready within ${VM_READY_TIMEOUT_SECONDS} seconds." >&2
  exit 1
fi

echo "Installing the GitHub runner, Docker, and Docker Compose inside the base VM."
"${TART_BIN}" exec -i "${staging_vm}" /bin/bash -s \
  <"${project_dir}/scripts/provision-guest.sh"
"${TART_BIN}" exec "${staging_vm}" /bin/sync

"${TART_BIN}" stop "${staging_vm}" --timeout 30
wait "${tart_pid}" 2>/dev/null || true
tart_pid=""

if [[ "${base_vm_exists}" == true ]]; then
  "${TART_BIN}" delete "${TART_BASE_VM}"
fi
"${TART_BIN}" rename "${staging_vm}" "${TART_BASE_VM}"
published=true
printf '%s\n' "${image_version}" >"${image_version_file}"

echo "Base VM ${TART_BASE_VM} image version ${image_version} is ready."
