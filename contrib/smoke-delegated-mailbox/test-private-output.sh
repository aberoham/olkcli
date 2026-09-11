#!/usr/bin/env bash
# Offline regression: preserve captured output/status without archiving payloads.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "${TEST_DIR}"' EXIT
mkdir -p "${TEST_DIR}/harness"
cp "${HERE}/lib.sh" "${TEST_DIR}/harness/lib.sh"
export SEND_ACCOUNT=sender@example.com SHARED_MAILBOX=shared@example.com RECIPIENT=recipient@example.com
export RUN_ID=offline EXPECTED_COMMIT=offline
# shellcheck source=lib.sh
source "${TEST_DIR}/harness/lib.sh"
private_fixture() {
  printf 'private draft body\n'
  printf 'private provider error\n' >&2
  return 7
}
run_private private_fixture >"${TEST_DIR}/stdout"
[[ "${RUN_STATUS}" == 7 ]]
[[ "${RUN_OUTPUT}" == $'private draft body\nprivate provider error' ]]
if grep -Eq 'private draft body|private provider error' "${LOG}" "${TEST_DIR}/stdout"; then
  printf 'Private output leaked\n' >&2
  exit 1
fi
grep -q 'output withheld from transcript' "${LOG}"
printf 'Private output/status retained; transcript and stdout exclude payloads.\n'
