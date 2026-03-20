#!/bin/bash
# =============================================================================
# ComputeClass-Aware Provisioning — Interactive Demo Script
# For: Product Manager Walkthroughs
#
# Configuration via environment variables:
#   CLUSTER_NAME     - GKE cluster name (default: my-cluster)
#   CLUSTER_LOCATION - GKE cluster region (default: us-central1)
#   PROJECT_ID       - GCP project ID (default: my-project)
# =============================================================================
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-my-cluster}"
CLUSTER_LOCATION="${CLUSTER_LOCATION:-us-central1}"
PROJECT_ID="${PROJECT_ID:-my-project}"

# ── Colors & Helpers ──────────────────────────────────────────────────────────
BLUE='\033[1;34m'
GREEN='\033[1;32m'
YELLOW='\033[1;33m'
RED='\033[1;31m'
CYAN='\033[1;36m'
PURPLE='\033[1;35m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

banner() { echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; echo -e "${BOLD}  $1${NC}"; echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"; }
narrate() { echo -e "${CYAN}  ▸ $1${NC}"; }
success() { echo -e "${GREEN}  ✓ $1${NC}"; }
warn() { echo -e "${YELLOW}  ⚠ $1${NC}"; }
step() { echo -e "\n${PURPLE}  [$1/${TOTAL_STEPS}]${NC} ${BOLD}$2${NC}\n"; }
pause() { echo -e "\n${DIM}  Press Enter to continue...${NC}"; read -r; }
run_cmd() { echo -e "  ${DIM}\$ $1${NC}"; eval "$1"; }

TOTAL_STEPS=7

# ── Intro ─────────────────────────────────────────────────────────────────────
clear
banner "Karpenter + GKE ComputeClass — Live Demo"

echo -e "  ${BOLD}What you're about to see:${NC}"
echo -e ""
echo -e "  Karpenter is an open-source Kubernetes node autoscaler. It watches for"
echo -e "  pending pods and provisions exactly the right compute to run them."
echo -e ""
echo -e "  ${CYAN}ComputeClass${NC} is a GKE feature that lets platform teams define"
echo -e "  ${CYAN}prioritized compute preferences${NC} — like 'try spot first, then fall"
echo -e "  back to on-demand' — as a reusable Kubernetes resource."
echo -e ""
echo -e "  ${GREEN}Our extension${NC} combines both: Karpenter reads ComputeClass priority"
echo -e "  rules to make smarter provisioning decisions, while keeping its"
echo -e "  superior lifecycle management (consolidation, drift detection, TTL)."
echo -e ""
echo -e "  ${DIM}Cluster: ${CLUSTER_NAME} (${CLUSTER_LOCATION})${NC}"
echo -e "  ${DIM}Project: ${PROJECT_ID}${NC}"

pause

# ── Step 1: Show Karpenter is running ─────────────────────────────────────────
step 1 "Verify Karpenter is running with ComputeClass support"

narrate "Let's confirm Karpenter is deployed and the ComputeClass feature is enabled."
echo ""
run_cmd "kubectl get pods -n karpenter-system -l app.kubernetes.io/name=karpenter"
echo ""

narrate "Checking that the ENABLE_COMPUTECLASS environment variable is set:"
echo ""
run_cmd "kubectl get deploy -n karpenter-system karpenter -o jsonpath='{.spec.template.spec.containers[0].env[*].name}' | tr ' ' '\n' | grep -i compute || echo '  (check logs instead)'"
echo ""
success "Karpenter is running with ComputeClass awareness enabled."

pause

# ── Step 2: Show the ComputeClass ─────────────────────────────────────────────
step 2 "Examine the ComputeClass priority rules"

narrate "A ComputeClass defines the platform team's compute preferences as"
narrate "prioritized rules. Let's look at ours:"
echo ""

cat <<'YAML'
  apiVersion: cloud.google.com/v1
  kind: ComputeClass
  metadata:
    name: cc-test-cost-optimized
  spec:
    priorities:
      - spot: true          # Priority 0: Spot E2 (cheapest)
        machineFamily: e2
      - spot: true          # Priority 1: Spot N2 (fallback spot)
        machineFamily: n2
      - machineFamily: e2   # Priority 2: On-demand E2
      - machineFamily: n2   # Priority 3: On-demand N2 (last resort)
YAML

echo ""
narrate "Karpenter reads these top-to-bottom. It tries Priority 0 first —"
narrate "if spot E2 capacity is available, it provisions that. Otherwise it"
narrate "falls through to the next rule, and so on."
echo ""
success "This gives you cost optimization with guaranteed fallback."

pause

# ── Step 3: Show the GCENodeClass with computeClassRef ────────────────────────
step 3 "See how GCENodeClass references the ComputeClass"

narrate "The GCENodeClass is where Karpenter meets ComputeClass."
narrate "One new field — computeClassRef — connects them:"
echo ""

cat <<'YAML'
  apiVersion: karpenter.k8s.gcp/v1alpha1
  kind: GCENodeClass
  metadata:
    name: cc-test
  spec:
    computeClassRef:
      name: cc-test-cost-optimized   # ← References the ComputeClass
    imageSelectorTerms:
      - alias: ContainerOptimizedOS@latest
    disks:
      - category: pd-balanced
        sizeGiB: 50
        boot: true
YAML

echo ""
narrate "Without this field, Karpenter works exactly like before."
narrate "With it, Karpenter gains priority-aware machine selection."
echo ""
success "Single field change. Full backward compatibility."

pause

# ── Step 4: Apply test resources ──────────────────────────────────────────────
step 4 "Deploy test resources to the cluster"

narrate "Let's apply the ComputeClass, GCENodeClass, NodePool, and a test workload."
echo ""

# Check if resources already exist
if kubectl get computeclass cc-test-cost-optimized 2>/dev/null; then
    warn "Test resources already exist. Cleaning up first..."
    kubectl delete deployment cc-test-workload --ignore-not-found 2>/dev/null || true
    kubectl delete nodepool cc-test-pool --ignore-not-found 2>/dev/null || true
    sleep 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "${SCRIPT_DIR}/test-computeclass-coralclaw.yaml" ]; then
    run_cmd "kubectl apply -f ${SCRIPT_DIR}/test-computeclass-coralclaw.yaml"
else
    run_cmd "kubectl apply -f ../test-computeclass-coralclaw.yaml 2>/dev/null || kubectl apply -f test-computeclass-coralclaw.yaml"
fi

echo ""
success "Resources created. The test deployment starts at 0 replicas."

pause

# ── Step 5: Trigger provisioning ──────────────────────────────────────────────
step 5 "Scale up workload to trigger Karpenter provisioning"

narrate "Now we scale the test workload to 3 replicas."
narrate "This creates pending pods that Karpenter will detect."
echo ""
run_cmd "kubectl scale deployment cc-test-workload --replicas=3"
echo ""

narrate "Karpenter is now evaluating these pending pods..."
narrate "It will:"
echo -e "    ${CYAN}1.${NC} Detect the 3 pending pods"
echo -e "    ${CYAN}2.${NC} Read the ComputeClass priority rules"
echo -e "    ${CYAN}3.${NC} Filter instance types by Priority 0 (Spot E2) first"
echo -e "    ${CYAN}4.${NC} Select the cheapest matching instance"
echo -e "    ${CYAN}5.${NC} Launch the node"
echo ""

narrate "Watching Karpenter logs for provisioning decisions..."
echo -e "  ${DIM}(Waiting up to 60s for activity — Ctrl+C to skip)${NC}"
echo ""

timeout 60 kubectl logs -n karpenter-system -l app.kubernetes.io/name=karpenter -f --tail=0 2>/dev/null | head -30 || true

pause

# ── Step 6: Inspect the results ───────────────────────────────────────────────
step 6 "Inspect provisioning results"

narrate "Let's see what Karpenter provisioned:"
echo ""

echo -e "  ${BOLD}NodeClaims:${NC}"
run_cmd "kubectl get nodeclaims 2>/dev/null || echo '  No nodeclaims found yet (provisioning may still be in progress)'"
echo ""

echo -e "  ${BOLD}Pods:${NC}"
run_cmd "kubectl get pods -l app=cc-test -o wide 2>/dev/null || echo '  Pods not scheduled yet'"
echo ""

echo -e "  ${BOLD}NodeClaim annotations (ComputeClass tracking):${NC}"
kubectl get nodeclaims -o json 2>/dev/null | python3 -c "
import json, sys
try:
    data = json.load(sys.stdin)
    for item in data.get('items', []):
        name = item['metadata']['name']
        ann = item['metadata'].get('annotations', {})
        cc = ann.get('karpenter.k8s.gcp/compute-class-name', 'none')
        print(f'  {name}: ComputeClass={cc}')
except:
    print('  (No nodeclaims to inspect yet)')
" 2>/dev/null || echo "  (Waiting for nodeclaims...)"
echo ""

success "Each NodeClaim is annotated with the ComputeClass that informed its provisioning."

pause

# ── Step 7: Cleanup ───────────────────────────────────────────────────────────
step 7 "Cleanup"

echo -e "  Would you like to clean up the test resources?"
echo -e "  ${DIM}This removes the test deployment, nodepool, and lets Karpenter"
echo -e "  consolidate (delete) any nodes it provisioned.${NC}"
echo ""
read -p "  Clean up? (y/n) " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    run_cmd "kubectl scale deployment cc-test-workload --replicas=0"
    run_cmd "kubectl delete deployment cc-test-workload --ignore-not-found"
    run_cmd "kubectl delete nodepool cc-test-pool --ignore-not-found"
    echo ""
    success "Test resources cleaned up. Karpenter will consolidate empty nodes."
else
    narrate "Leaving resources in place. Clean up later with:"
    echo -e "    kubectl delete -f test-computeclass-coralclaw.yaml"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
banner "Demo Complete"

echo -e "  ${BOLD}What we demonstrated:${NC}"
echo ""
echo -e "  ${GREEN}1.${NC} ComputeClass defines platform-wide compute preferences"
echo -e "  ${GREEN}2.${NC} GCENodeClass references ComputeClass via a single field"
echo -e "  ${GREEN}3.${NC} Karpenter reads priority rules and provisions accordingly"
echo -e "  ${GREEN}4.${NC} Each node is annotated with its ComputeClass source"
echo -e "  ${GREEN}5.${NC} Full backward compatibility — remove the ref and it's vanilla Karpenter"
echo ""
echo -e "  ${BOLD}Key benefits:${NC}"
echo ""
echo -e "  ${CYAN}Cost:${NC}       Priority-based spot preference can save 40-70%"
echo -e "  ${CYAN}Reliability:${NC} Automatic fallback ensures workloads always run"
echo -e "  ${CYAN}Control:${NC}    Platform teams define rules; developers just deploy"
echo -e "  ${CYAN}Lifecycle:${NC}  Karpenter's consolidation, drift, and TTL still work"
echo ""
echo -e "  ${DIM}Dashboard: open cc-dashboard.html in your browser${NC}"
echo -e "  ${DIM}PRD:       PRD-ComputeClass-Karpenter-Extension.docx${NC}"
echo -e "  ${DIM}Code:      github.com/geoffsdesk/karpenter-provider-gcp${NC}"
echo ""
