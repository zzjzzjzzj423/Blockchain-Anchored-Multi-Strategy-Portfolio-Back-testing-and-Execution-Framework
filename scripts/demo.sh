#!/bin/sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_root"

PATH="$project_root/.tools/go/bin:$project_root/.tools/foundry-v1.9.0/bin:$PATH"
GOCACHE="$project_root/.cache/go-build"
GOMODCACHE="$project_root/.cache/go-mod"
GOTELEMETRY=off
GOTOOLCHAIN=local
export PATH GOCACHE GOMODCACHE GOTELEMETRY GOTOOLCHAIN

make build
./bin/project doctor
./bin/project data validate --root .

echo "Final holdout ensemble metrics"
jq '.metrics.ensemble' reports/m5_final_holdout.json

bundle="artifacts/098376de7f855b9e"
./bin/project evidence verify --bundle "$bundle"
proof=$(./bin/project evidence prove --bundle "$bundle" --path results/trades.csv)
./bin/project evidence verify-proof --bundle "$bundle" --proof "$proof"

./bin/project chain rehearse --root .
./bin/project mutate-test --count 100 --seed 5017 "$bundle"

jq '{artifact_identical_runs,root_identical_runs,network_calls,agent_calls,status}' reports/m8_replay.json
jq '{root_submission_gas:.root_submission_actual_gas,proof_verification}' "$bundle/chain/gas_report.json"
jq '.' reports/m8_overhead.json

echo "Limitation: the registry proves commitment to bytes, not source-data truth or future profitability."
echo "M9 demonstration passed"
