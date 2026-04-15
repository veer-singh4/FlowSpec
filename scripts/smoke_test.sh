#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: go is not installed"
  exit 1
fi

if ! command -v terraform >/dev/null 2>&1; then
  echo "ERROR: terraform is not installed"
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "ERROR: python3 is not installed"
  exit 1
fi

echo "Building flow CLI..."
go build -o "$TMP_DIR/flow" "$ROOT_DIR/cmd/flow"

cat > "$TMP_DIR/main.ufl" << 'SPEC'
app smoke-service {
  cloud aws ap-south-1

  use network.vpc as smoke-vpc {
    name smoke-vpc
    cidr 10.50.0.0/16
    azs ["ap-south-1a","ap-south-1b"]
    private_subnets ["10.50.1.0/24","10.50.2.0/24"]
    public_subnets ["10.50.101.0/24","10.50.102.0/24"]
  }
}
SPEC

pushd "$ROOT_DIR" >/dev/null

echo "Parser check..."
python3 parser-py/parser.py "$TMP_DIR/main.ufl" >/dev/null

echo "flow init..."
"$TMP_DIR/flow" init

echo "flow plan..."
"$TMP_DIR/flow" plan "$TMP_DIR/main.ufl"

echo "Smoke test passed (plan phase)."

popd >/dev/null
