#!/usr/bin/env bash

set -euo pipefail

# Environment variables (with defaults)
MONGODB_URI=${MONGODB_URI:-"mongodb://localhost:27017"}
DB_NAME=${DB_NAME:-""}
COLLECTION_NAMES=${COLLECTION_NAMES:-""}  # Comma-separated list
BACKUP_DIR="/backups"
S3_BUCKET=${S3_BUCKET:-""}
S3_PREFIX=${S3_PREFIX:-"mongodb-backups"}
BACKUP_NAME=${BACKUP_NAME:-"backup-$(date +%Y%m%d-%H%M%S)"}
SPLIT_SIZE=${SPLIT_SIZE:-"1G"}  # 1GB chunks

# Create backup directory
mkdir -p "$BACKUP_DIR"

echo "Starting MongoDB backup..."
echo "Database: $DB_NAME"
echo "Collections: $COLLECTION_NAMES"
echo "Backup name: $BACKUP_NAME"

# Build mongodump command
MONGODUMP_CMD="mongodump --uri=\"$MONGODB_URI\" --archive=\"$BACKUP_DIR/${BACKUP_NAME}.archive\" --gzip"

# Add database filter if specified
if [[ -n "$DB_NAME" ]]; then
    MONGODUMP_CMD="$MONGODUMP_CMD --db=\"$DB_NAME\""
fi

# Add collection filters if specified
if [[ -n "$COLLECTION_NAMES" ]]; then
    IFS=',' read -ra COLLECTIONS <<< "$COLLECTION_NAMES"
    for collection in "${COLLECTIONS[@]}"; do
        collection=$(echo "$collection" | xargs)  # trim whitespace
        if [[ -n "$collection" ]]; then
            MONGODUMP_CMD="$MONGODUMP_CMD --collection=\"$collection\""
        fi
    done
fi

echo "Running: $MONGODUMP_CMD"
eval "$MONGODUMP_CMD"

echo "Backup completed. Archive size:"
ls -lh "$BACKUP_DIR/${BACKUP_NAME}.archive"

# Split the backup into chunks
echo "Splitting backup into ${SPLIT_SIZE} chunks..."
cd "$BACKUP_DIR"
split -b "$SPLIT_SIZE" -d "${BACKUP_NAME}.archive" "${BACKUP_NAME}_part_"

# Remove original archive after splitting
rm "${BACKUP_NAME}.archive"

# Generate hashes and create manifest
echo "Generating hashes and manifest..."
manifest_file="$BACKUP_DIR/manifest.json"
cat > "$manifest_file" << 'EOF'
{
  "backup_name": "",
  "created_at": "",
  "database": "",
  "collections": [],
  "total_size": 0,
  "parts": []
}
EOF

# Update manifest with metadata
collections_array=$(echo "$COLLECTION_NAMES" | sed 's/,/","/g' | sed 's/^/"/' | sed 's/$/"/' | sed 's/""//g')
if [[ "$collections_array" == '""' ]]; then
    collections_array='[]'
else
    collections_array="[$collections_array]"
fi

total_size=0
parts_json="["

for part_file in ${BACKUP_NAME}_part_*; do
    if [[ -f "$part_file" ]]; then
        echo "Processing $part_file..."
        
        # Calculate hash
        hash=$(sha256sum "$part_file" | cut -d' ' -f1)
        size=$(stat -f%z "$part_file" 2>/dev/null || stat -c%s "$part_file")
        total_size=$((total_size + size))
        
        # Add to parts JSON
        if [[ "$parts_json" != "[" ]]; then
            parts_json="$parts_json,"
        fi
        parts_json="$parts_json{\"filename\":\"$part_file\",\"size\":$size,\"sha256\":\"$hash\"}"
        
        echo "  $part_file: $hash ($(numfmt --to=iec $size))"
    fi
done

parts_json="$parts_json]"

# Update manifest file using jq if available, otherwise sed
jq --arg backup_name "$BACKUP_NAME" \
   --arg created_at "$(date -Iseconds)" \
   --arg database "$DB_NAME" \
   --argjson collections "$collections_array" \
   --arg total_size "$total_size" \
   --argjson parts "$parts_json" \
   '.backup_name = $backup_name | .created_at = $created_at | .database = $database | .collections = $collections | .total_size = ($total_size | tonumber) | .parts = $parts' \
   "$manifest_file" > "${manifest_file}.tmp" && mv "${manifest_file}.tmp" "$manifest_file"

echo "Manifest created:"
cat "$manifest_file"

# Upload to S3 if bucket is specified
if [[ -n "$S3_BUCKET" ]]; then
    echo "Uploading to S3 bucket: $S3_BUCKET"
    s3_path="s3://$S3_BUCKET/$S3_PREFIX/$BACKUP_NAME"
    
    # Configure AWS CLI with custom endpoint if specified
    if [[ -n "${AWS_S3_ENDPOINT:-}" ]]; then
        export AWS_CLI_S3_ENDPOINT="--endpoint-url=$AWS_S3_ENDPOINT"
    else
        export AWS_CLI_S3_ENDPOINT=""
    fi
    
    # Upload manifest first
    echo "Uploading manifest..."
    eval "aws s3 cp $AWS_CLI_S3_ENDPOINT \"$manifest_file\" \"$s3_path/manifest.json\""
    
    # Upload all parts
    for part_file in ${BACKUP_NAME}_part_*; do
        if [[ -f "$part_file" ]]; then
            echo "Uploading $part_file..."
            eval "aws s3 cp $AWS_CLI_S3_ENDPOINT \"$part_file\" \"$s3_path/$part_file\""
        fi
    done
    
    echo "Backup uploaded successfully to: $s3_path"
    
    # Clean up local files after successful upload
    echo "Cleaning up local files..."
    rm -f ${BACKUP_NAME}_part_* "$manifest_file"
    
else
    echo "No S3 bucket specified. Backup files remain in $BACKUP_DIR"
    echo "Total backup size: $(numfmt --to=iec $total_size)"
fi

echo "Backup process completed successfully!"
