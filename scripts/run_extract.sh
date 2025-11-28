#!/bin/bash

set -e
set -x


# Determine project root as: parent of the scripts directory
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

go build ./cmd/extract-documents

if [[ "$(uname)" == "Darwin" ]]; then
    HASH=`uuidgen`
else
    HASH=`uuid`
fi

DATESTAMP=`date -u +"%Y-%m-%d_%H%M%SZ"`

DATASET_DIR="$PROJECT_ROOT/data/processed/dataset-$DATESTAMP-$HASH"

PARQUET_FILE="$PROJECT_ROOT/data/processed/dataset-$DATESTAMP-$HASH/summary.parquet"

mkdir -p $DATASET_DIR

./extract-documents \
    --http-cache    		$PROJECT_ROOT/data/processed/http-cache.sqlite3 \
    --pipeline-db-file		$PROJECT_ROOT/data/processed/document-pipeline.sqlite3 \
    --output-dir            $DATASET_DIR \
	--output-parquet-file   $PARQUET_FILE \
    --utas-raw-pdfs         $PROJECT_ROOT/data/external/utas \
    --wps-csv               $PROJECT_ROOT/data/raw/wps_missing.csv \
