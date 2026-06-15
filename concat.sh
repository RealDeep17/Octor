#!/bin/bash
# Surgical Codebase Concatenator
# Target: Octor Repository

# 1. Setup paths
REPO_ROOT="/home/ubuntu/octor"
OUTPUT_DIR="$REPO_ROOT/OCTOR_CODEBASE_V2"
MAX_SIZE=1500000 # 1.5MB
PART=1

mkdir -p "$OUTPUT_DIR"
rm -f "$OUTPUT_DIR"/*.txt

CURRENT_OUT="$OUTPUT_DIR/octor_codebase_part_${PART}.txt"
touch "$CURRENT_OUT"

cd "$REPO_ROOT" || exit 1

# 2. Get clean file list (exclude junk/binary/metadata)
FILES=$(git ls-files | grep -E '\.(go|py|js|ts|jsx|tsx|html|sh|css|sql|yml|yaml|md|json|proto|mod|sum|txt|xml|toml)$' | grep -vE '^(bin|node_modules|vendor|venv|dist|\.git|OCTOR_CODEBASE_V2|infra-data)')

# 3. Concatenate
git ls-files | grep -E '\.(go|py|js|ts|jsx|tsx|html|sh|css|sql|yml|yaml|md|json|proto|mod|sum|txt|xml|toml)$' | grep -vE '^(bin|node_modules|vendor|venv|dist|\.git|OCTOR_CODEBASE_V2|infra-data)' | while read -r f; do
    # Skip logs/large metadata
    [[ "$f" == "remediation_changes.txt" ]] && continue
    [[ "$f" == "REMEDIATION.md" ]] && continue
    [[ "$f" == "STABILIZATION_PROOF.md" ]] && continue

    # Rotation check
    SIZE=$(stat -c%s "$CURRENT_OUT")
    if [ "$SIZE" -gt "$MAX_SIZE" ]; then
        PART=$((PART + 1))
        CURRENT_OUT="$OUTPUT_DIR/octor_codebase_part_${PART}.txt"
        touch "$CURRENT_OUT"
    fi

    # Append with clear boundaries
    {
        echo ">>> FILE_START: $f"
        cat "$f"
        echo -e "\n<<< FILE_END: $f"
    } >> "$CURRENT_OUT"
done

echo "Generated $PART parts in $OUTPUT_DIR"
