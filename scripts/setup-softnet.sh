#!/bin/bash

set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
softnet_bin="$("${project_dir}/scripts/ensure-softnet.sh")"

echo "Configuring pinned Softnet with the root ownership and setuid bit required by Tart."
sudo chown root:wheel "${softnet_bin}"
sudo chmod u+s "${softnet_bin}"

"${project_dir}/scripts/ensure-softnet.sh" --require-configured >/dev/null
echo "Softnet is configured: ${softnet_bin}"
