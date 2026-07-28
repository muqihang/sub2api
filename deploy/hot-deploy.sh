#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/hot-deploy-common.sh
source "${SCRIPT_DIR}/lib/hot-deploy-common.sh"

preload_config "$@"
apply_defaults
parse_args "$@"
validate_config
acquire_lock
trap release_lock EXIT

if [[ "${DRY_RUN}" == "true" ]]; then
  print_dry_run
  exit 0
fi

ROLLBACK_ARMED=false
CANDIDATE_CREATED=false
CANDIDATE_COMMITTED=false
trap 'handle_transaction_error $?' ERR
trap 'handle_transaction_signal 130' INT
trap 'handle_transaction_signal 143' TERM
run_transaction
