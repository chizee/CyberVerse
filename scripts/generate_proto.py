#!/usr/bin/env python
"""
Cross-platform proto generation.

This repo historically used scripts/generate_proto.sh, which relies on bash,
python3 and sed. On Windows, those are not always available. This script keeps
the same behavior using pure Python so it works on Windows/macOS/Linux.
"""

from __future__ import annotations

import argparse
import re
import shutil
import subprocess
import sys
from pathlib import Path


def _fix_python_imports(py_out_dir: Path) -> None:
    # grpc_tools generates imports like:
    #   import common_pb2 as common__pb2
    # We want:
    #   from inference.generated import common_pb2 as common__pb2
    import_re = re.compile(r"^import\s+([a-z_]+_pb2)\b", flags=re.MULTILINE)

    for f in sorted(py_out_dir.glob("*_pb2*.py")):
        text = f.read_text(encoding="utf-8")
        updated = import_re.sub(r"from inference.generated import \1", text)
        if updated != text:
            f.write_text(updated, encoding="utf-8")


def generate_python(proto_dir: Path, py_out_dir: Path) -> None:
    try:
        from grpc_tools import protoc  # type: ignore
    except Exception as e:  # pragma: no cover - error path
        raise RuntimeError(
            "grpcio-tools is required to generate Python protos. "
            "Install with: pip install '.[inference]'"
        ) from e

    proto_files = sorted(proto_dir.glob("*.proto"))
    if not proto_files:
        raise RuntimeError(f"No .proto files found under: {proto_dir}")

    py_out_dir.mkdir(parents=True, exist_ok=True)
    (py_out_dir / "__init__.py").touch(exist_ok=True)

    args = [
        "protoc",
        f"-I{proto_dir}",
        f"--python_out={py_out_dir}",
        f"--grpc_python_out={py_out_dir}",
        *[str(p) for p in proto_files],
    ]

    rc = protoc.main(args)
    if rc != 0:
        raise RuntimeError(f"grpc_tools.protoc failed with exit code {rc}")

    _fix_python_imports(py_out_dir)


def generate_go(proto_dir: Path, go_out_dir: Path) -> bool:
    protoc = shutil.which("protoc")
    gen_go = shutil.which("protoc-gen-go")
    gen_go_grpc = shutil.which("protoc-gen-go-grpc")
    if not (protoc and gen_go and gen_go_grpc):
        return False

    proto_files = sorted(proto_dir.glob("*.proto"))
    if not proto_files:
        raise RuntimeError(f"No .proto files found under: {proto_dir}")

    go_out_dir.mkdir(parents=True, exist_ok=True)

    subprocess.run(
        [
            protoc,
            "-I",
            str(proto_dir),
            f"--go_out={go_out_dir}",
            "--go_opt=paths=source_relative",
            f"--go-grpc_out={go_out_dir}",
            "--go-grpc_opt=paths=source_relative",
            *[str(p) for p in proto_files],
        ],
        check=True,
    )
    return True


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description="Generate gRPC stubs from proto/*.proto")
    parser.add_argument("--proto-dir", default="proto", help="Directory containing .proto files")
    parser.add_argument(
        "--python-out",
        default="inference/generated",
        help="Output directory for generated Python code",
    )
    parser.add_argument(
        "--go-out",
        default="server/internal/pb",
        help="Output directory for generated Go code",
    )
    parser.add_argument(
        "--skip-go",
        action="store_true",
        help="Skip Go proto generation even if protoc plugins are installed",
    )
    args = parser.parse_args(argv)

    repo_root = Path(__file__).resolve().parent.parent
    proto_dir = (repo_root / args.proto_dir).resolve()
    py_out_dir = (repo_root / args.python_out).resolve()
    go_out_dir = (repo_root / args.go_out).resolve()

    generate_python(proto_dir=proto_dir, py_out_dir=py_out_dir)
    print(f"Python proto generation complete: {py_out_dir}")

    if args.skip_go:
        return 0

    try:
        did_go = generate_go(proto_dir=proto_dir, go_out_dir=go_out_dir)
    except subprocess.CalledProcessError as e:  # pragma: no cover - error path
        print(f"Go proto generation failed: {e}", file=sys.stderr)
        return 1

    if did_go:
        print(f"Go proto generation complete: {go_out_dir}")
    else:
        print("Skipping Go proto generation: protoc/protoc-gen-go/protoc-gen-go-grpc not found")
        print("Install with:")
        print("  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest")
        print("  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest")

    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))

