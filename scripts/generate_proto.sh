#!/bin/bash
# Generate Python gRPC code from proto files
# Works on both macOS and Linux
set -e

PROTO_DIR="proto"
OUT_DIR="inference/generated"

mkdir -p "$OUT_DIR"

python3 -m grpc_tools.protoc \
    -I "$PROTO_DIR" \
    --python_out="$OUT_DIR" \
    --grpc_python_out="$OUT_DIR" \
    "$PROTO_DIR"/*.proto

# Fix imports to use absolute paths (cross-platform sed)
fix_imports() {
    local file="$1"
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' 's/^import \([a-z_]*\)_pb2/from inference.generated import \1_pb2/' "$file"
    else
        sed -i 's/^import \([a-z_]*\)_pb2/from inference.generated import \1_pb2/' "$file"
    fi
}

for f in "$OUT_DIR"/*_pb2_grpc.py "$OUT_DIR"/*_pb2.py; do
    fix_imports "$f"
done

echo "Python proto generation complete: $OUT_DIR"

# Generate Go gRPC code
GO_OUT_DIR="server/internal/pb"
mkdir -p "$GO_OUT_DIR"

if command -v protoc-gen-go &> /dev/null && command -v protoc-gen-go-grpc &> /dev/null; then
    protoc -I "$PROTO_DIR" \
        --go_out="$GO_OUT_DIR" --go_opt=paths=source_relative \
        --go-grpc_out="$GO_OUT_DIR" --go-grpc_opt=paths=source_relative \
        "$PROTO_DIR"/*.proto
    echo "Go proto generation complete: $GO_OUT_DIR"
else
    echo "Skipping Go proto generation: protoc-gen-go or protoc-gen-go-grpc not found"
    echo "Install with: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    echo "             go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
fi
