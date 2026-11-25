#!/bin/sh
# Enforce the coverage gates on the whole module.
#
# Statement, function and decision coverage come from gomcdc (instrumented).
# Line coverage comes from limgo over a merged coverprofile; limgo's branch
# estimate is intentionally not used.
set -eu

cd "$(dirname "$0")/.."
mkdir -p .coverage

STATEMENT_MIN="${STATEMENT_MIN:-90}"
FUNCTION_MIN="${FUNCTION_MIN:-90}"
DECISION_MIN="${DECISION_MIN:-80}"
LINES_MIN="${LINES_MIN:-90}"

go test ./... -coverpkg=./... -covermode=count -coverprofile=.coverage/raw.out

go run github.com/wadey/gocovmerge@v0.0.0-20160331181800-b5bfa59ec0ad \
  .coverage/raw.out > .coverage/merged.out

cat > .coverage/limgo.json <<EOF
{"coverage":{"global":{"statements":0,"lines":80,"branches":0}},"statistic":{}}
EOF

go run github.com/GoTestTools/limgo/cmd/limgo@v1.0.0 \
  -coverfile=.coverage/merged.out -config=.coverage/limgo.json \
  -outfmt=json > .coverage/limgo-report.json

go run ./scripts/coveragecheck -file .coverage/limgo-report.json -min "${LINES_MIN}"

go run github.com/shrydev2020/gomcdc/v2@v2.1.1 test \
  --coverage=statement,function,decision \
  --save .coverage/gomcdc.json ./...

go run github.com/shrydev2020/gomcdc/v2@v2.1.1 require --complete \
  --statement "${STATEMENT_MIN}" \
  --function "${FUNCTION_MIN}" \
  --decision "${DECISION_MIN}" \
  .coverage/gomcdc.json
