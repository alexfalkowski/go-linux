#!/usr/bin/env bash
# Shared Docker setup for repository verification and coverage launchers.

# Run a facility's verification in privileged Docker after dependency setup.
# Arguments after the facility are its architecture and Make assignments.
run_privileged_container() {
  local image=$1
  local facility=$2
  shift 2

  docker run --quiet --rm --privileged --user root \
    --mount "type=bind,src=$PWD,dst=/home/circleci/go-linux" \
    --workdir /home/circleci/go-linux \
    -- "$image" \
    bash -eo pipefail -c '
      git config --global --add safe.directory /home/circleci/go-linux
      make dep
      make "verify-$0" arch="$1" "${@:2}"
    ' "$facility" "$@"
}

# Run a command as the checkout owner with empty capability sets. After image
# and label, pass extra Docker options, --, then the command and its arguments.
# Keep a root init for cross-UID permission checks and signal forwarding; only
# the setup process receives the capabilities needed to remap/drop identity.
run_nonroot_container() {
  local image=$1
  local label=$2
  local docker_options=()
  shift 2

  while [[ $1 != -- ]]; do
    docker_options+=("$1")
    shift
  done
  shift

  docker run --quiet --rm --init --user root \
    --cap-drop ALL --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
    --cap-add SETUID --cap-add SETGID --cap-add SETPCAP --cap-add KILL \
    --security-opt no-new-privileges \
    --mount "type=bind,src=$PWD,dst=/work" --workdir /work \
    --env HOME=/home/circleci \
    "${docker_options[@]}" \
    -- "$image" bash -eo pipefail -c '
      # Remap only the image home, outside /work, preserving checkout ownership.
      usermod --uid "$1" circleci
      uid=$1
      gid=$2
      shift 2
      exec setpriv --reuid="$uid" --regid="$gid" --clear-groups \
        --inh-caps=-all --ambient-caps=-all --bounding-set=-all \
        "$@"
    ' "$label" "$(id -u)" "$(id -g)" "$@"
}
