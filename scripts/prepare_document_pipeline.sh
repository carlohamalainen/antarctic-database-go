#!/bin/bash

set -e
set -x

cd $PROJECT_ROOT

go build ./cmd/prepare-document-pipeline

# for debugging
# rm -fv $PROJECT_ROOT/data/processed/http-cache.sqlite3
# rm -fv $PROJECT_ROOT/data/processed/document-pipeline.sqlite3
# rm -fv $PROJECT_ROOT/data/processed/document-summary.parquet

# Use this flag to only process a handful of documents:
# --quick

if [[ "$(uname)" == "Darwin" ]]; then
    HASH=`uuidgen`
else
    HASH=`uuid`
fi

DATESTAMP=`date -u +"%Y-%m-%d_%H%M%SZ"`

OUTPUT_FILE="$PROJECT_ROOT/data/processed/prepare-document-pipeline-$DATESTAMP-$HASH.log"

./prepare-document-pipeline \
    --http-cache           $PROJECT_ROOT/data/processed/http-cache.sqlite3 \
    --new-pipeline-db-file $PROJECT_ROOT/data/processed/document-pipeline.sqlite3 \
    --document-summary     $PROJECT_ROOT/data/processed/document-summary.parquet \
    --utas-raw-pdfs        $PROJECT_ROOT/data/external/utas \
    --wps-csv              $PROJECT_ROOT/data/raw/wps_missing.csv &> $OUTPUT_FILE
