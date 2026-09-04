# Makefile for Phase 0 Validation
# Provides convenient targets for running validation checks

.PHONY: help validate validate-network validate-alloy validate-prometheus validate-crds report clean

# Default target
help:
	@echo "Phase 0 Validation Targets:"
	@echo ""
	@echo "  make validate              - Run all Phase 0 validations"
	@echo "  make validate-network      - Run network policy validation only"
	@echo "  make validate-alloy        - Run Alloy configuration validation only"
	@echo "  make validate-prometheus   - Run Prometheus/Thanos validation only"
	@echo "  make validate-crds         - Run CRDs & Operators validation only"
	@echo "  make report                - Generate validation report only"
	@echo "  make fix                   - Run validation with auto-fix attempts (where safe)"
	@echo "  make clean                 - Clean up test resources"
	@echo ""
	@echo "Options:"
	@echo "  NAMESPACE=<ns>             - Target specific namespace"
	@echo "  VERBOSE=1                  - Enable verbose output"

# Run all validations
validate:
	@chmod +x scripts/validate-phase0.sh
	@./scripts/validate-phase0.sh

# Run with auto-fix (where safe)
fix:
	@chmod +x scripts/validate-phase0.sh
	@./scripts/validate-phase0.sh --fix

# Network policy validation only
validate-network:
	@chmod +x scripts/validate-network-policies.sh
	@./scripts/validate-network-policies.sh $(if $(VERBOSE),--verbose) $(if $(NAMESPACE),--namespace=$(NAMESPACE))

# Alloy configuration validation only
validate-alloy:
	@chmod +x scripts/check-alloy-config.sh
	@./scripts/check-alloy-config.sh

# Prometheus/Thanos validation only
validate-prometheus:
	@chmod +x scripts/check-prometheus-thanos.sh
	@./scripts/check-prometheus-thanos.sh

# CRDs & Operators validation only
validate-crds:
	@chmod +x scripts/check-crds-operators.sh
	@./scripts/check-crds-operators.sh

# Generate report only
report:
	@chmod +x scripts/validate-phase0.sh
	@./scripts/validate-phase0.sh --report-only

# Clean up any test pods
clean:
	@echo "Cleaning up test pods..."
	@kubectl delete pods -n monitoring -l test=connectivity --grace-period=0 --force 2>/dev/null || true
	@kubectl delete pods -n monitoring -l test=network-policy-validation --grace-period=0 --force 2>/dev/null || true
	@kubectl delete pods -n observability -l test=network-policy-validation --grace-period=0 --force 2>/dev/null || true
	@kubectl delete pods -n cost-management -l test=network-policy-validation --grace-period=0 --force 2>/dev/null || true
	@echo "Cleanup complete"

# Install required tools
install-tools:
	@echo "Checking required tools..."
	@which kubectl >/dev/null || (echo "kubectl not found" && exit 1)
	@which curl >/dev/null || (echo "curl not found" && exit 1)
	@which base64 >/dev/null || (echo "base64 not found" && exit 1)
	@echo "All required tools available"

# Quick health check
health:
	@echo "=== Quick Cluster Health ==="
	@kubectl get nodes -o wide
	@echo ""
	@echo "=== Key Namespaces ==="
	@kubectl get ns monitoring observability cost-management --show-labels 2>/dev/null || echo "Some namespaces missing"
	@echo ""
	@echo "=== Key Deployments ==="
	@kubectl get deploy -n monitoring -l app.kubernetes.io/part-of=observability 2>/dev/null || echo "No observability deployments found"
	@echo ""
	@echo "=== Prometheus/Thanos ==="
	@kubectl get prometheus,thanosquery,thanosstore,thanoscompact -n monitoring 2>/dev/null || echo "Prometheus/Thanos not found"
