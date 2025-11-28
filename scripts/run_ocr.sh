#!/bin/bash

set -e
set -x


PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

go build ./cmd/run-ocr

if [[ "$(uname)" == "Darwin" ]]; then
    HASH=`uuidgen`
else
    HASH=`uuid`
fi

DATESTAMP=`date -u +"%Y-%m-%d_%H%M%SZ"`

OUTPUT_FILE="$PROJECT_ROOT/data/processed/ocr-pipeline-$DATESTAMP-$HASH.log"

./run-ocr \
	--pipeline-db-file $PROJECT_ROOT/data/processed/document-pipeline.sqlite3 \
	--use-asset-upload=false \
	--service nvidia \
	--batch-size 10 &> $OUTPUT_FILE
