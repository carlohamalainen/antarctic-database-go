#!/bin/bash

set -e
set -x

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

go build ./cmd/information-exchange

if [[ "$(uname)" == "Darwin" ]]; then
    HASH=`uuidgen`
else
    HASH=`uuid`
fi

DATESTAMP=`date -u +"%Y-%m-%d_%H%M%SZ"`

OUTPUT_FILE="$PROJECT_ROOT/data/processed/information-exchange-$DATESTAMP-$HASH.log"

OUTPUT_DIR="$PROJECT_ROOT/data/processed/information-exchange-$DATESTAMP-$HASH"

mkdir $OUTPUT_DIR

./information-exchange \
    --http-cache    $PROJECT_ROOT/data/processed/information-exchange-http-cache.sqlite3 \
    --output-dir    $OUTPUT_DIR &> $OUTPUT_FILE
