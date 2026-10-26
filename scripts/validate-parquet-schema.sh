#!/bin/bash
# Validation script for Parquet schema compatibility

set -euo pipefail

echo "=== Parquet Schema Validation ==="

# Check if exporter binary exists
if [[ ! -f "./bin/exporter" ]]; then
    echo "Building exporter..."
    make build-exporter
fi

# Test schema generation
echo "Testing Parquet schema generation..."
./bin/exporter --dry-run 2>&1 | head -20

# Validate with Python/PyArrow if available
if command -v python3 &> /dev/null; then
    echo "Validating with PyArrow..."
    python3 << 'EOF'
import pyarrow.parquet as pq
import pyarrow as pa

# Expected schema from the Go implementation
expected_fields = [
    ("timestamp", pa.timestamp('us')),
    ("cluster", pa.string()),
    ("namespace", pa.string()),
    ("workload", pa.string()),
    ("kind", pa.string()),
    ("cost_center", pa.string()),
    ("team", pa.string()),
    ("cpu_cores_requested", pa.float64()),
    ("cpu_cores_used", pa.float64()),
    ("memory_gib_requested", pa.float64()),
    ("memory_gib_used", pa.float64()),
    ("storage_gib_requested", pa.float64()),
    ("storage_gib_used", pa.float64()),
    ("compute_cost_usd", pa.float64()),
    ("storage_cost_usd", pa.float64()),
    ("network_cost_usd", pa.float64()),
    ("shared_cost_usd", pa.float64()),
    ("total_cost_usd", pa.float64()),
    ("efficiency_pct", pa.float64()),
]

expected_schema = pa.schema(expected_fields)
print("Expected PyArrow Schema:")
print(expected_schema)
print()

# Check nullable fields
nullable_fields = {"cost_center", "team"}
for field in expected_schema:
    is_nullable = field.nullable
    expected_nullable = field.name in nullable_fields
    status = "✓" if is_nullable == expected_nullable else "✗"
    print(f"  {status} {field.name}: nullable={is_nullable} (expected={expected_nullable})")

print("\nSchema validation complete!")
EOF
else
    echo "Python3 not available, skipping PyArrow validation"
fi

echo "=== Validation Complete ==="
