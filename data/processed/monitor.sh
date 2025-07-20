#!/bin/bash

# monitor-progress.sh
# Script to monitor OCR processing progress

DB_FILE="document-pipeline.sqlite3"

while true; do
  echo "$(date '+%Y-%m-%d %H:%M:%S') - OCR Processing Progress:"
  
  # Get processed count - extract just the number
  PROCESSED=$(duckdb -c "SELECT count(*) AS NR_PROCESSED FROM ocr;" "$DB_FILE" | grep -oP '\d+' | tail -1)
  
  # Get total count - extract just the number
  TOTAL=$(duckdb -c "SELECT count(*) FROM pages;" "$DB_FILE" | grep -oP '\d+' | tail -1)
  
  # Calculate percentage
  if [[ "$TOTAL" =~ ^[0-9]+$ ]] && [ "$TOTAL" -gt 0 ]; then
    PERCENTAGE=$(echo "scale=2; ($PROCESSED * 100) / $TOTAL" | bc)
    echo "Processed: $PROCESSED / $TOTAL pages ($PERCENTAGE%)"
  else
    echo "Processed: $PROCESSED / $TOTAL pages"
  fi

  # break
  
  echo "----------------------------------------"

  # Wait 5 seconds before checking again
  sleep 5
done
