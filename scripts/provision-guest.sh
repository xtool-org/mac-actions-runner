#!/bin/bash

set -euo pipefail

if [[ "$(id -u)" -eq 0 ]]; then
  sudo_command=()
else
  sudo_command=(sudo -n)
fi

export DEBIAN_FRONTEND=noninteractive

"${sudo_command[@]}" apt-get update
"${sudo_command[@]}" apt-get install -y \
  ca-certificates \
  curl \
  docker.io \
  git \
  jq

"${sudo_command[@]}" systemctl enable --now docker
"${sudo_command[@]}" usermod -aG docker admin
"${sudo_command[@]}" install -d -o admin -g admin /opt/actions-runner

release_json="$(curl -fsSL \
  -H 'Accept: application/vnd.github+json' \
  https://api.github.com/repos/actions/runner/releases/latest)"
runner_version="$(jq -er '.tag_name | ltrimstr("v")' <<<"${release_json}")"

runner_archive_name="actions-runner-linux-arm64-${runner_version}.tar.gz"
runner_archive="/tmp/${runner_archive_name}"
runner_url="https://github.com/actions/runner/releases/download/v${runner_version}/${runner_archive_name}"
runner_digest="$(jq -er \
  --arg archive_name "${runner_archive_name}" \
  '.assets[] | select(.name == $archive_name) | .digest | select(startswith("sha256:"))' \
  <<<"${release_json}")"

curl -fL --retry 3 --output "${runner_archive}" "${runner_url}"
printf '%s  %s\n' "${runner_digest#sha256:}" "${runner_archive}" \
  | sha256sum --check --status
"${sudo_command[@]}" tar -xzf "${runner_archive}" -C /opt/actions-runner
"${sudo_command[@]}" chown -R admin:admin /opt/actions-runner

for required_file in \
  /opt/actions-runner/run.sh \
  /opt/actions-runner/bin/Runner.Listener \
  /opt/actions-runner/bin/libcoreclr.so; do
  if [[ ! -s "${required_file}" ]]; then
    echo "Runner extraction produced an empty file: ${required_file}" >&2
    exit 1
  fi
done

"${sudo_command[@]}" /opt/actions-runner/bin/installdependencies.sh
rm -f "${runner_archive}"
"${sudo_command[@]}" apt-get clean
sync

echo "Provisioned GitHub Actions runner ${runner_version} with an isolated Docker daemon."
