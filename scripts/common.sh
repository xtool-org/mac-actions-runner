#!/bin/bash

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

set -a
# shellcheck source=../runner.default.env
source "${project_dir}/runner.default.env"
set +a

if [[ -f "${project_dir}/runner.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${project_dir}/runner.env"
  set +a
fi

if [[ "${APP_PRIVATE_KEY_FILE}" != /* ]]; then
  APP_PRIVATE_KEY_FILE="${project_dir}/${APP_PRIVATE_KEY_FILE#./}"
fi

require_commands() {
  local command_name

  for command_name in "$@"; do
    if ! command -v "${command_name}" >/dev/null 2>&1; then
      echo "Required command not found: ${command_name}" >&2
      return 1
    fi
  done
}

configure_network_helper() {
  if [[ "${TART_NETWORK_MODE}" != softnet ]]; then
    return
  fi

  SOFTNET_BIN="$("${project_dir}/scripts/ensure-softnet.sh" --require-configured)"
  PATH="$(dirname -- "${SOFTNET_BIN}"):${PATH}"
  export SOFTNET_BIN PATH
}

resolve_github_runner_url() {
  if [[ "${GITHUB_RUNNER_URL}" == auto ]]; then
    GITHUB_RUNNER_URL="https://github.com/${ORG_NAME}"
    export GITHUB_RUNNER_URL
  fi
}

resolve_xcode_app_path() {
  local selected_developer_dir

  if [[ "${XCODE_APP_PATH}" != auto ]]; then
    return
  fi

  if ! selected_developer_dir="$(/usr/bin/xcode-select -p 2>/dev/null)"; then
    echo "Could not derive XCODE_APP_PATH from xcode-select -p." >&2
    echo "Set XCODE_APP_PATH to an absolute path, or leave it empty to disable the share." >&2
    return 1
  fi

  case "${selected_developer_dir}" in
    */Contents/Developer)
      XCODE_APP_PATH="${selected_developer_dir%/Contents/Developer}"
      ;;
    *)
      echo "xcode-select does not point inside an Xcode.app: ${selected_developer_dir}" >&2
      echo "Set XCODE_APP_PATH to an absolute path, or leave it empty to disable the share." >&2
      return 1
      ;;
  esac

  export XCODE_APP_PATH
}

vm_exists() {
  local vm_name="$1"

  "${TART_BIN:?TART_BIN is required}" list --source local --quiet \
    | grep -Fqx -- "${vm_name}"
}

wait_for_guest() {
  local vm_name="$1"
  local timeout_seconds="$2"
  local deadline=$((SECONDS + timeout_seconds))

  while ((SECONDS < deadline)); do
    if "${TART_BIN:?TART_BIN is required}" exec "${vm_name}" \
      /usr/bin/true >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done

  return 1
}
