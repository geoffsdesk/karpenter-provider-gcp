# ComputeClass-Aware Provisioning — Demo

This directory contains everything needed to demo Karpenter's ComputeClass extension on a GKE cluster. The extension lets Karpenter read GKE Custom Compute Class (CCC) priority rules when selecting instance types, combining Karpenter's lifecycle management with GKE's priority-based machine selection.

## Prerequisites

- A GKE cluster running version **1.33.3+** with NAP **disabled** (Karpenter replaces NAP)
- `gcloud`, `kubectl`, `helm`, and `docker` installed locally
- The `feature/computeclass-aware-provisioning` branch of this repo checked out
- Cluster credentials configured: `gcloud container clusters get-credentials <cluster> --region <region> --project <project>`

## Quick Start

### 1. Set your environment

```bash
export PROJECT_ID="your-gcp-project-id"
export CLUSTER_NAME="your-cluster-name"
export CLUSTER_LOCATION="your-cluster-region"   # e.g. us-central1
```

### 2. Deploy Karpenter with ComputeClass support

```bash
chmod +x demo/scripts/deploy-karpenter-cc.sh
./demo/scripts/deploy-karpenter-cc.sh
```

This script builds the controller image from source, pushes it to Artifact Registry, and deploys Karpenter via Helm with the `ENABLE_COMPUTECLASS=true` feature flag.

### 3. Apply the test manifests

```bash
kubectl apply -f demo/manifests/test-computeclass-coralclaw.yaml
```

This creates:

- A **ComputeClass** (`cc-test-cost-optimized`) with four priority tiers: Spot E2 → Spot N2 → On-Demand E2 → On-Demand N2
- A **GCENodeClass** (`cc-test`) referencing the ComputeClass via `computeClassRef`
- A **NodePool** (`cc-test-pool`) using the GCENodeClass
- A test **Deployment** (`cc-test-workload`) starting at 0 replicas

### 4. Trigger provisioning

```bash
kubectl scale deployment cc-test-workload --replicas=3
```

Karpenter detects the pending pods, reads the ComputeClass priority rules, and provisions a node using the highest-priority available machine type (Spot E2 first).

### 5. Verify

```bash
# Check the provisioned node
kubectl get nodeclaims

# See the ComputeClass annotation on the NodeClaim
kubectl get nodeclaims -o json | jq '.items[].metadata.annotations["karpenter.k8s.gcp/compute-class-name"]'
```

### 6. Cleanup

```bash
kubectl scale deployment cc-test-workload --replicas=0
kubectl delete -f demo/manifests/test-computeclass-coralclaw.yaml
```

## Interactive Demo

For a guided walkthrough with narration and step-by-step commands:

```bash
chmod +x demo/scripts/cc-demo.sh
./demo/scripts/cc-demo.sh
```

See `demo/scripts/demo-script-notes.md` for slide-by-slide talking points.

## Dashboard

Open `demo/dashboard/cc-dashboard.html` in a browser. You can pass cluster info via URL parameters:

```
file:///path/to/demo/dashboard/cc-dashboard.html?cluster=my-cluster&region=us-central1
```

The dashboard supports two modes:

- **Simulation mode** — click "Simulate Provisioning" to see how priority-based provisioning works
- **Live data mode** — click "Refresh Data" and paste JSON from your cluster:

```bash
kubectl get nodeclaims -o json | jq '{
  nodes: [.items[] | {
    name: .metadata.name,
    type: .metadata.labels["node.kubernetes.io/instance-type"] // "unknown",
    zone: .metadata.labels["topology.kubernetes.io/zone"] // "unknown",
    capacity: .metadata.labels["karpenter.sh/capacity-type"] // "unknown",
    cc_name: .metadata.annotations["karpenter.k8s.gcp/compute-class-name"] // null,
    cc_priority: .metadata.annotations["karpenter.k8s.gcp/compute-class-priority"] // null,
    created: .metadata.creationTimestamp
  }]
}'
```

## Directory Layout

```
demo/
├── README.md                  # This file
├── dashboard/
│   └── cc-dashboard.html      # Browser-based ComputeClass dashboard
├── manifests/
│   └── test-computeclass-coralclaw.yaml  # ComputeClass + GCENodeClass + NodePool + Deployment
├── patches/
│   ├── 0001-feat-add-ComputeClass-aware-provisioning-for-GKE-Cus.patch
│   ├── 0001-fix-add-computeClassRef-to-GCENodeClass-CRD-schema.patch
│   └── 0002-feat-implement-full-Phase-1-alpha-for-ComputeClass-a.patch
└── scripts/
    ├── cc-demo.sh             # Interactive demo with narration
    ├── demo-script-notes.md   # Slide-by-slide talking points
    └── deploy-karpenter-cc.sh # Full deployment script
```

## Patches

The `patches/` directory contains git patches for the ComputeClass extension. These can be applied to a clean checkout of the upstream `cloudpilot-ai/karpenter-provider-gcp` repo:

```bash
git clone https://github.com/cloudpilot-ai/karpenter-provider-gcp.git
cd karpenter-provider-gcp
git am ../demo/patches/*.patch
```

## How It Works

The ComputeClass extension adds a single field (`computeClassRef`) to the GCENodeClass CRD. When Karpenter provisions a node:

1. It checks if the GCENodeClass has a `computeClassRef`
2. If present, it reads the ComputeClass priority rules from the GKE API
3. It filters and reorders instance type candidates based on priority tiers
4. It selects the cheapest instance matching the highest-available priority
5. It annotates the resulting NodeClaim with the ComputeClass name and matched priority

Without the field, Karpenter behaves exactly as before — full backward compatibility.

The feature is gated behind `ComputeClassAwareProvisioning` and requires `ENABLE_COMPUTECLASS=true`.
