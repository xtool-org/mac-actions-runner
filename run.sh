#!/bin/bash

set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "${project_dir}"

# shellcheck source=scripts/common.sh
source "${project_dir}/scripts/common.sh"

TART_BIN="$("${project_dir}/scripts/ensure-tart.sh")"
export TART_BIN
configure_network_helper
resolve_github_runner_url
resolve_xcode_app_path

if [[ -n "${XCODE_APP_PATH}" ]]; then
  if [[ "${XCODE_APP_PATH}" != /* ]]; then
    echo "XCODE_APP_PATH must be absolute: ${XCODE_APP_PATH}" >&2
    exit 1
  fi
  if [[ "${XCODE_APP_PATH}" == *:* ]]; then
    echo "XCODE_APP_PATH cannot contain a colon: ${XCODE_APP_PATH}" >&2
    exit 1
  fi
  if [[ ! -d "${XCODE_APP_PATH}/Contents/Developer" ]]; then
    echo "Xcode app not found at: ${XCODE_APP_PATH}" >&2
    exit 1
  fi
fi

if [[ ! -f "${APP_PRIVATE_KEY_FILE}" ]]; then
  echo "GitHub App private key not found: ${APP_PRIVATE_KEY_FILE}" >&2
  exit 1
fi

if ! vm_exists "${TART_BASE_VM}"; then
  echo "Base VM ${TART_BASE_VM} does not exist; preparing it now."
  bash "${project_dir}/prepare-image.sh"
fi

mkdir -p "${project_dir}/.state"

current_vm=""
tart_pid=""

cleanup_current_vm() {
  if [[ -z "${current_vm}" ]]; then
    return
  fi

  echo "Destroying disposable VM ${current_vm}."
  "${TART_BIN}" stop "${current_vm}" --timeout 10 >/dev/null 2>&1 || true

  if [[ -n "${tart_pid}" ]]; then
    wait "${tart_pid}" 2>/dev/null || true
  fi

  if vm_exists "${current_vm}"; then
    "${TART_BIN}" delete "${current_vm}"
  fi

  current_vm=""
  tart_pid=""
}

trap cleanup_current_vm EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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

if [[ -n "${XCODE_APP_PATH}" ]]; then
  tart_run_command+=(--dir "${XCODE_APP_PATH}:ro,tag=xcode")
fi

while true; do
  runner_suffix="$(date +%s)-$$-${RANDOM}"
  current_vm="${RUNNER_NAME_PREFIX}-${runner_suffix}"
  runner_name="${current_vm}"

  echo "Creating disposable VM ${current_vm} from ${TART_BASE_VM}."
  "${TART_BIN}" clone "${TART_BASE_VM}" "${current_vm}"
  "${TART_BIN}" set "${current_vm}" --random-mac

  vm_log="${project_dir}/.state/vm.log"
  "${tart_run_command[@]}" "${current_vm}" >"${vm_log}" 2>&1 &
  tart_pid=$!

  echo "Waiting for the Tart guest agent."
  if ! wait_for_guest "${current_vm}" "${VM_READY_TIMEOUT_SECONDS}"; then
    echo "VM did not become ready; see ${vm_log}" >&2
    cleanup_current_vm
    sleep "${RUNNER_RESTART_DELAY_SECONDS}"
    continue
  fi

  if [[ -n "${XCODE_APP_PATH}" ]]; then
    echo "Mounting ${XCODE_APP_PATH} read-only at /usr/share/Xcode.app."
    if ! "${TART_BIN}" exec "${current_vm}" /bin/bash -c '
      set -e
      sudo -n install -d -o root -g root /usr/share/Xcode.app
      sudo -n mount -t virtiofs -o ro xcode /usr/share/Xcode.app
      test -d /usr/share/Xcode.app/Contents/Developer
    '; then
      echo "Could not mount the Xcode app inside the guest." >&2
      exit 1
    fi
  fi

  echo "Requesting a one-time GitHub runner registration token."
  runner_token="$(python3 "${project_dir}/scripts/github_auth.py" \
    registration-token \
    --app-id "${APP_ID}" \
    --org "${ORG_NAME}" \
    --private-key "${APP_PRIVATE_KEY_FILE}" \
    --api-url "${GITHUB_API_URL}")"

  echo "Starting ephemeral runner ${runner_name}."
  set +e
  {
    printf 'RUNNER_URL=%q\n' "${GITHUB_RUNNER_URL}"
    printf 'RUNNER_TOKEN=%q\n' "${runner_token}"
    printf 'RUNNER_NAME=%q\n' "${runner_name}"
    printf 'RUNNER_LABELS=%q\n' "${RUNNER_LABELS}"
    printf 'RUNNER_GROUP=%q\n' "${RUNNER_GROUP}"
    cat "${project_dir}/scripts/run-runner.sh"
  } | "${TART_BIN}" exec -i "${current_vm}" /bin/bash -s
  pipeline_status=("${PIPESTATUS[@]}")
  set -e

  runner_token=""
  runner_status="${pipeline_status[1]}"
  cleanup_current_vm

  if [[ "${runner_status}" -eq 0 ]]; then
    echo "Job finished; preparing a fresh VM."
  else
    echo "Runner exited with status ${runner_status}; retrying in ${RUNNER_RESTART_DELAY_SECONDS} seconds." >&2
    sleep "${RUNNER_RESTART_DELAY_SECONDS}"
  fi
done
