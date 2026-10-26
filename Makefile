# Phase 0 Native Makefile
# Usage: make -f native/Makefile <target>

GO ?= go

.PHONY: help apply validate clean deploy-ksm deploy-metrics-server deploy-node-exporter deploy-rbac deploy-crds deploy-examples build-exporter

# Default target
help:
	@echo "Kubernetes-Native FinOps Platform - Phase 0"
	@echo ""
	@echo "Targets:"
	@echo "  apply              - Apply all Phase 0 resources"
	@echo "  validate           - Run Phase 0 validation script"
	@echo "  clean              - Delete all Phase 0 resources"
	@echo "  deploy-ksm         - Deploy KSM only"
	@echo "  deploy-metrics-server - Deploy Metrics Server only"
	@echo "  deploy-node-exporter - Deploy Node Exporter only"
	@echo "  deploy-rbac        - Deploy RBAC only"
	@echo "  deploy-crds        - Deploy CRDs only"
	@echo "  deploy-examples    - Deploy example CRs only"
	@echo "  report             - View validation report"

# Apply all Phase 0 resources
apply:
	kubectl apply -k native/

# Validate Phase 0
validate:
	chmod +x native/scripts/validate-native-phase0.sh
	./native/scripts/validate-native-phase0.sh

# Clean all Phase 0 resources
clean:
	kubectl delete -k native/ --ignore-not-found=true

# Individual component deployment
deploy-ksm:
	kubectl apply -f native/ksm/

deploy-metrics-server:
	kubectl apply -f native/metrics-server/

deploy-node-exporter:
	kubectl apply -f native/node-exporter/

deploy-rbac:
	kubectl apply -f native/rbac/

deploy-crds:
	kubectl apply -f native/crds/

deploy-examples:
	kubectl apply -f native/examples/

# View report
report:
	@if [ -f docs/phase0-native-report.md ]; then cat docs/phase0-native-report.md; else echo "Report not found. Run 'make validate' first."; fi

# Build targets
build-exporter:
	@echo "Building Cost Exporter..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -a -o bin/exporter ./cmd/exporter

# Check individual components
check-ksm:
	kubectl get deployment kube-state-metrics -n monitoring -o wide
	kubectl get configmap kube-state-metrics-config -n monitoring -o yaml
	kubectl get networkpolicy kube-state-metrics-allow-cost-controller -n monitoring -o yaml

check-metrics-server:
	kubectl get deployment metrics-server -n kube-system -o wide
	kubectl top nodes
	kubectl top pods -A

check-node-exporter:
	kubectl get daemonset node-exporter -n monitoring -o wide
	kubectl get service node-exporter -n monitoring -o yaml
	kubectl get networkpolicy node-exporter-allow-cost-controller -n monitoring -o yaml

check-rbac:
	kubectl get sa cost-controller-sa -n cost-management -o yaml
	kubectl get clusterrole cost-controller-reader -o yaml
	kubectl get clusterrolebinding cost-controller-reader-binding -o yaml

check-crds:
	kubectl get crd | grep finops.io
	kubectl get crd costallocations.finops.io -o jsonpath='{.spec.versions[0].subresources.status}'

check-examples:
	kubectl get namespacebudget finops-system-budget -n finops-system -o yaml
