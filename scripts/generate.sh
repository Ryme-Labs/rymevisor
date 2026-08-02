#!/bin/bash
set -e

echo "Generating protobuf code..."

# Check if buf is installed
if ! command -v buf &> /dev/null; then
    echo "buf not found. Installing..."
    go install github.com/bufbuild/buf/cmd/buf@latest
fi

# Generate code
buf generate

echo "Code generation complete!"
echo "Generated files are in gen/go/"
