#!/bin/bash

set -euo pipefail

: "${RUNNER_URL:?RUNNER_URL is required}"
: "${RUNNER_TOKEN:?RUNNER_TOKEN is required}"
: "${RUNNER_NAME:?RUNNER_NAME is required}"
: "${RUNNER_LABELS:?RUNNER_LABELS is required}"
: "${RUNNER_GROUP:?RUNNER_GROUP is required}"

runner_token="${RUNNER_TOKEN}"
unset RUNNER_TOKEN

cd /opt/actions-runner

run_as_admin() {
  if [[ "$(id -u)" -eq 0 ]]; then
    runuser -u admin -- "$@"
  else
    "$@"
  fi
}

run_as_admin ./config.sh \
  --url "${RUNNER_URL}" \
  --token "${runner_token}" \
  --name "${RUNNER_NAME}" \
  --labels "${RUNNER_LABELS}" \
  --runnergroup "${RUNNER_GROUP}" \
  --work _work \
  --ephemeral \
  --unattended \
  --replace

runner_token=""
if [[ "$(id -u)" -eq 0 ]]; then
  exec runuser -u admin -- ./run.sh
else
  exec ./run.sh
fi
