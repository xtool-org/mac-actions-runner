#!/bin/bash

set -euo pipefail

: "${RUNNER_JIT_CONFIG:?RUNNER_JIT_CONFIG is required}"

jit_config="${RUNNER_JIT_CONFIG}"
unset RUNNER_JIT_CONFIG
export ACTIONS_RUNNER_INPUT_JITCONFIG="${jit_config}"
jit_config=""

cd /opt/actions-runner

if [[ "$(id -u)" -eq 0 ]]; then
  exec runuser -u admin -- ./run.sh
else
  exec ./run.sh
fi
