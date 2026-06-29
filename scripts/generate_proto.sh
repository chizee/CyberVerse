#!/bin/bash
# Generate Python gRPC code from proto files
# Works on both macOS and Linux
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVER_DIR="$REPO_ROOT/server"
PROTO_DIR="$REPO_ROOT/proto"
OUT_DIR="$REPO_ROOT/inference/generated"
GO_OUT_DIR="$REPO_ROOT/server/internal/pb"

# protoc is not a Go module; pin for reproducible server/internal/pb headers.
# protobuf 5.29.x prints "libprotoc 29.3" from `protoc --version`.
REQUIRED_LIBPROTOC_VERSION="29.3"
PINNED_PROTOC="${HOME}/.local/cyberverse-tools/protobuf-${REQUIRED_LIBPROTOC_VERSION}/bin/protoc"

resolve_go_protoc() {
    local candidate="${PROTOC:-}"
    if [[ -n "$candidate" ]]; then
        if [[ "$candidate" == */* ]]; then
            if [[ ! -x "$candidate" ]]; then
                echo "ERROR: PROTOC is not executable: $candidate" >&2
                exit 1
            fi
            printf '%s\n' "$candidate"
            return
        fi
        if command -v "$candidate" &>/dev/null; then
            command -v "$candidate"
            return
        fi
        echo "ERROR: PROTOC command not found: $candidate" >&2
        exit 1
    fi

    if [[ -x "$PINNED_PROTOC" ]]; then
        printf '%s\n' "$PINNED_PROTOC"
        return
    fi

    if command -v protoc &>/dev/null; then
        command -v protoc
        return
    fi

    echo "ERROR: protoc not found (required for Go proto generation)." >&2
    echo "Set PROTOC=/path/to/protoc or install protobuf 5.29.3 at $PINNED_PROTOC." >&2
    exit 1
}

verify_protoc_for_go() {
    local protoc_bin="$1"
    if [[ ! -x "$protoc_bin" ]]; then
        echo "ERROR: protoc is not executable: $protoc_bin" >&2
        exit 1
    fi
    local pv
    pv=$("$protoc_bin" --version 2>&1 | tr -d '\r')
    if [[ "$pv" != "libprotoc ${REQUIRED_LIBPROTOC_VERSION}" ]]; then
        echo "ERROR: protoc version mismatch for reproducible Go codegen." >&2
        echo "  Expected: libprotoc ${REQUIRED_LIBPROTOC_VERSION} (protobuf 5.29.3)" >&2
        echo "  Got:      ${pv} (${protoc_bin})" >&2
        echo "Set PROTOC=/path/to/protoc or install protobuf 5.29.3 at $PINNED_PROTOC." >&2
        exit 1
    fi
}

PYTHON_BIN="${PYTHON:-}"
if [[ -z "$PYTHON_BIN" ]]; then
    for candidate in python python3; do
        if command -v "$candidate" &> /dev/null && "$candidate" -c 'import grpc_tools.protoc' &> /dev/null; then
            PYTHON_BIN="$candidate"
            break
        fi
    done
fi
if [[ -z "$PYTHON_BIN" ]]; then
    echo "grpc_tools is required: install grpcio-tools or set PYTHON=/path/to/python"
    exit 1
fi

mkdir -p "$OUT_DIR"

"$PYTHON_BIN" -m grpc_tools.protoc \
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

# Generate Go gRPC code (plugin versions: server/go.mod tool block)
mkdir -p "$GO_OUT_DIR"

if command -v go &> /dev/null && [[ -f "$SERVER_DIR/go.mod" ]]; then
    GO_PROTOC_BIN=$(resolve_go_protoc)
    verify_protoc_for_go "$GO_PROTOC_BIN"
    PLUGIN_GEN_GO=$(go -C "$SERVER_DIR" tool -n protoc-gen-go)
    PLUGIN_GEN_GO_GRPC=$(go -C "$SERVER_DIR" tool -n protoc-gen-go-grpc)
    "$GO_PROTOC_BIN" -I "$PROTO_DIR" \
        --plugin=protoc-gen-go="$PLUGIN_GEN_GO" \
        --plugin=protoc-gen-go-grpc="$PLUGIN_GEN_GO_GRPC" \
        --go_out="$GO_OUT_DIR" --go_opt=paths=source_relative \
        --go-grpc_out="$GO_OUT_DIR" --go-grpc_opt=paths=source_relative \
        "$PROTO_DIR"/*.proto
    echo "Go proto generation complete: $GO_OUT_DIR"
else
    echo "Skipping Go proto generation: go not found or missing $SERVER_DIR/go.mod"
    echo "Go plugins are pinned in server/go.mod (tool); with Go installed, versions follow that file."
    echo "protoc for Go codegen: protobuf 5.29.3 (protoc --version => libprotoc ${REQUIRED_LIBPROTOC_VERSION})"
fi
