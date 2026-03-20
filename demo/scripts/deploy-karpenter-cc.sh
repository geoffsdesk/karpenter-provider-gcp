#!/bin/bash
# =============================================================================
# Deploy Karpenter GCP Provider with ComputeClass Extension
#
# Configuration via environment variables:
#   PROJECT_ID       - GCP project ID (required)
#   CLUSTER_NAME     - GKE cluster name (required)
#   CLUSTER_LOCATION - GKE cluster region (required)
#   KARPENTER_NS     - Karpenter namespace (default: karpenter-system)
#   IMAGE_TAG        - Container image tag (default: auto-generated)
# =============================================================================
set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────
PROJECT_ID="${PROJECT_ID:?ERROR: Set PROJECT_ID env var (e.g. export PROJECT_ID=my-gcp-project)}"
CLUSTER_NAME="${CLUSTER_NAME:?ERROR: Set CLUSTER_NAME env var (e.g. export CLUSTER_NAME=my-cluster)}"
CLUSTER_LOCATION="${CLUSTER_LOCATION:?ERROR: Set CLUSTER_LOCATION env var (e.g. export CLUSTER_LOCATION=us-central1)}"
KARPENTER_NAMESPACE="${KARPENTER_NS:-karpenter-system}"
IMAGE_REPO="${CLUSTER_LOCATION}-docker.pkg.dev/${PROJECT_ID}/karpenter/controller"
IMAGE_TAG="${IMAGE_TAG:-computeclass-alpha-$(date +%Y%m%d-%H%M%S)}"

echo "============================================="
echo "Deploying Karpenter GCP Provider"
echo "  Cluster:   ${CLUSTER_NAME} (${CLUSTER_LOCATION})"
echo "  Project:   ${PROJECT_ID}"
echo "  Image:     ${IMAGE_REPO}:${IMAGE_TAG}"
echo "  Namespace: ${KARPENTER_NAMESPACE}"
echo "============================================="

# ── Step 0: Prerequisites Check ──────────────────────────────────────────────
echo ""
echo ">>> Step 0: Checking prerequisites..."

command -v gcloud >/dev/null 2>&1 || { echo "ERROR: gcloud not found"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl not found"; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "ERROR: helm not found"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "ERROR: docker not found. Install Docker or use ko instead."; exit 1; }

echo "All prerequisites found."

# ── Step 1: Connect to cluster ───────────────────────────────────────────────
echo ""
echo ">>> Step 1: Connecting to GKE cluster..."
gcloud container clusters get-credentials "${CLUSTER_NAME}" \
    --region "${CLUSTER_LOCATION}" \
    --project "${PROJECT_ID}"

echo "Connected. Current context:"
kubectl config current-context

# ── Step 2: Create Artifact Registry (if needed) ────────────────────────────
echo ""
echo ">>> Step 2: Ensuring Artifact Registry exists..."
gcloud artifacts repositories describe karpenter \
    --location="${CLUSTER_LOCATION}" \
    --project="${PROJECT_ID}" 2>/dev/null || \
gcloud artifacts repositories create karpenter \
    --repository-format=docker \
    --location="${CLUSTER_LOCATION}" \
    --project="${PROJECT_ID}" \
    --description="Karpenter GCP Provider images"

# Configure Docker auth for Artifact Registry
gcloud auth configure-docker "${CLUSTER_LOCATION}-docker.pkg.dev" --quiet

# ── Step 3: Build and push the image ────────────────────────────────────────
echo ""
echo ">>> Step 3: Building container image..."
cd "$(dirname "$0")/karpenter-provider-gcp" 2>/dev/null || \
cd "$(git rev-parse --show-toplevel)" 2>/dev/null || \
{ echo "ERROR: Run this from the repo root or the code directory"; exit 1; }

# Verify we're on the feature branch
BRANCH=$(git branch --show-current)
echo "Current branch: ${BRANCH}"
if [[ "${BRANCH}" != "feature/computeclass-aware-provisioning" ]]; then
    echo "WARNING: Not on feature branch. Switch with: git checkout feature/computeclass-aware-provisioning"
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo
    [[ $REPLY =~ ^[Yy]$ ]] || exit 1
fi

# Build with Docker (multi-stage)
cat > /tmp/Dockerfile.karpenter <<'DOCKERFILE'
FROM golang:1.24-bookworm AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY cmd/ cmd/
COPY pkg/ pkg/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=vendor \
    -ldflags="-s -w" \
    -o /karpenter-controller \
    ./cmd/controller/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /karpenter-controller /karpenter-controller
ENTRYPOINT ["/karpenter-controller"]
DOCKERFILE

docker build -f /tmp/Dockerfile.karpenter -t "${IMAGE_REPO}:${IMAGE_TAG}" .
echo "Image built: ${IMAGE_REPO}:${IMAGE_TAG}"

echo ""
echo ">>> Step 3b: Pushing image to Artifact Registry..."
docker push "${IMAGE_REPO}:${IMAGE_TAG}"
echo "Image pushed."

# ── Step 4: Create namespace and RBAC ────────────────────────────────────────
echo ""
echo ">>> Step 4: Setting up namespace and RBAC..."
kubectl create namespace "${KARPENTER_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# ── Step 5: Create GCP credentials secret (if using SA key) ─────────────────
echo ""
echo ">>> Step 5: Credentials setup..."
# Check if a credentials secret already exists
if kubectl get secret karpenter-gcp-credentials -n "${KARPENTER_NAMESPACE}" 2>/dev/null; then
    echo "Credentials secret already exists."
else
    echo "No credentials secret found."
    echo "Option A: Create a service account key and store it:"
    echo "  gcloud iam service-accounts keys create /tmp/karpenter-sa-key.json \\"
    echo "    --iam-account=karpenter@${PROJECT_ID}.iam.gserviceaccount.com"
    echo "  kubectl create secret generic karpenter-gcp-credentials \\"
    echo "    --from-file=key.json=/tmp/karpenter-sa-key.json \\"
    echo "    -n ${KARPENTER_NAMESPACE}"
    echo ""
    echo "Option B: Use Workload Identity (recommended) — set credentials.enabled=false in Helm values."
    echo ""
    read -p "Do you have a service account key to use? Enter path (or 'skip' for Workload Identity): " SA_KEY_PATH
    if [[ "${SA_KEY_PATH}" != "skip" && -f "${SA_KEY_PATH}" ]]; then
        kubectl create secret generic karpenter-gcp-credentials \
            --from-file=key.json="${SA_KEY_PATH}" \
            -n "${KARPENTER_NAMESPACE}"
        echo "Secret created."
    else
        echo "Skipping credentials secret. Make sure Workload Identity is configured."
    fi
fi

# ── Step 6: Install CRDs ────────────────────────────────────────────────────
echo ""
echo ">>> Step 6: Installing Karpenter CRDs..."
kubectl apply -f charts/karpenter/crds/

# ── Step 7: Deploy with Helm ────────────────────────────────────────────────
echo ""
echo ">>> Step 7: Deploying Karpenter via Helm..."
helm upgrade --install karpenter charts/karpenter \
    --namespace "${KARPENTER_NAMESPACE}" \
    --set controller.image.repository="${IMAGE_REPO}" \
    --set controller.image.tag="${IMAGE_TAG}" \
    --set controller.settings.projectID="${PROJECT_ID}" \
    --set controller.settings.clusterName="${CLUSTER_NAME}" \
    --set controller.settings.clusterLocation="${CLUSTER_LOCATION}" \
    --set controller.replicaCount=1 \
    --set logLevel=debug \
    --set "controller.env[0].name=ENABLE_COMPUTECLASS" \
    --set "controller.env[0].value=true" \
    --set "controller.env[1].name=STRICT_NAP_CHECK" \
    --set "controller.env[1].value=false" \
    --wait --timeout 5m

echo ""
echo ">>> Deployment complete! Checking pod status..."
kubectl get pods -n "${KARPENTER_NAMESPACE}" -l app.kubernetes.io/name=karpenter

echo ""
echo "============================================="
echo "Karpenter deployed with ComputeClass support!"
echo ""
echo "Next steps:"
echo "  1. Apply test resources:  kubectl apply -f examples/computeclass/cost-optimized.yaml"
echo "  2. Check logs:            kubectl logs -n ${KARPENTER_NAMESPACE} -l app.kubernetes.io/name=karpenter -f"
echo "  3. Create a test pod:     kubectl run test-cc --image=nginx --restart=Never"
echo "============================================="
