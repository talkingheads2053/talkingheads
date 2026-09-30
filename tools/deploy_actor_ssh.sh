#!/bin/bash

# Try public-key auth first (set up via tools/setup_ssh_key.sh), then fall
# back to interactive password auth if the key isn't installed yet.
ALLOW_SSH_AUTH_WITH_PASSWORD="-o PreferredAuthentications=publickey,password -o PasswordAuthentication=yes"

# Ensure a source argument was provided
if [ "$#" -lt 4 ] || [ "$#" -gt 5 ]; then
    echo "Usage: $0 <path_to_source> <name_of_program> <script_dir> <ip_address_or_hostname> [username]"
    exit 1
fi

# 1. Use go to build the program
SOURCE_PATH="$1"
PROGRAM_NAME="$2"
SCRIPT_DIR="$3"
TARGET_HOST="$4"
TARGET_USER="${5:-arduino}"
TARGET="$TARGET_USER@$TARGET_HOST"
OUTPUT_PATH="${PROGRAM_NAME%.*}"
echo "Building program from source '$SOURCE_PATH'..."
cd $SOURCE_PATH
if ! GOOS=linux GOARCH=arm64 go build -o "$OUTPUT_PATH" .; then
    echo "Error: Failed to build program from $SOURCE_PATH"
    exit 1
fi

LOCAL_FILE="$OUTPUT_PATH"
FILE_NAME=$(basename "$LOCAL_FILE")
TARGET_FILE_PATH="~/talkingheads/$FILE_NAME"

# Ensure target directories exist
echo "Ensuring remote directories exist..."
ssh $ALLOW_SSH_AUTH_WITH_PASSWORD "$TARGET" "mkdir -p ~/talkingheads/scripts"

# 2. Upload the Actor file
echo "Uploading program '$FILE_NAME'..."
if ! scp $ALLOW_SSH_AUTH_WITH_PASSWORD "$LOCAL_FILE" "$TARGET:$TARGET_FILE_PATH"; then
    echo "Error: Failed to upload $LOCAL_FILE"
    exit 1
fi

# 3. Upload the scripts for the Actor.
echo "Uploading scripts for program '$FILE_NAME'..."
SCRIPT_DIR="$3"
if [ -d "$SCRIPT_DIR" ]; then
    for script in "$SCRIPT_DIR"/*.*; do
        if [ -f "$script" ]; then
            SCRIPT_NAME=$(basename "$script")
            TARGET_SCRIPT_PATH="~/talkingheads/scripts/$SCRIPT_NAME"
            echo "Uploading script '$SCRIPT_NAME'..."
            if ! scp $ALLOW_SSH_AUTH_WITH_PASSWORD "$script" "$TARGET:$TARGET_SCRIPT_PATH"; then
                echo "Error: Failed to upload $script"
                exit 1
            fi
        fi
    done
else
    echo "No scripts directory found at $SCRIPT_DIR, skipping script upload."
fi

echo "Done!"
