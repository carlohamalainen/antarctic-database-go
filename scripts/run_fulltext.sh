#!/bin/bash

set -e
set -x

cd $PROJECT_ROOT

go build ./cmd/run-fulltext

if [[ "$(uname)" == "Darwin" ]]; then
    HASH=`uuidgen`
else
    HASH=`uuid`
fi

OUTPUT_FILE="$PROJECT_ROOT/data/processed/fulltext-pipeline-$DATESTAMP-$HASH.log"

./run-fulltext \
	--pipeline-db-file $PROJECT_ROOT/data/processed/document-pipeline.sqlite3  &> $OUTPUT_FILE
