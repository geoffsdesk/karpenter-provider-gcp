# ComputeClass-Aware Provisioning — Demo Script

## Talking Points & Commands for Each Slide

---

## Slide 1: Title

**Say:** "Today I'm going to show you how we extended Karpenter — the open-source Kubernetes node autoscaler — to work with GKE's Custom Compute Classes. This gives us the best of both worlds: Karpenter's lifecycle management combined with GKE's priority-based machine selection."

---

## Slide 2: The Problem

**Say:** "Right now, GKE teams have to choose. Karpenter gives you consolidation, drift detection, and TTL-based node rotation — but no priority fallback. CCC plus NAP gives you priority-based machine selection and active migration — but no fine-grained lifecycle control. You pick one and lose the other."

---

## Slide 3: The Solution

**Say:** "Our extension connects them with a single field. You add a computeClassRef to your GCENodeClass, and Karpenter starts reading the ComputeClass priority rules. No NAP required. Full backward compatibility — remove the field and it's vanilla Karpenter."

**Terminal command to show the live config:**

```bash
kubectl get gcenodeclass cc-test -o yaml | grep -A2 computeClassRef
```

---

## Slide 4: How Priority Fallback Works

**Say:** "The ComputeClass defines four priority tiers. Karpenter evaluates them top to bottom. Priority 0 says 'try spot E2 first' — the cheapest option. If that capacity isn't available, it falls to Priority 1, then 2, then 3. Your workloads always run, at the best available price."

---

## Slide 5: Expected Impact

**Say:** "With spot-first priority strategies, we expect 40 to 70 percent cost savings. The provisioning success rate stays above 99 percent because of the fallback chain. And the overhead of reading ComputeClass rules adds less than 500 milliseconds to the provisioning path."

---

## Slide 6: What We Built

**Say:** "Phase 1 is live on this cluster right now. We built the computeClassRef field, priority-aware filtering, graceful degradation, NAP conflict detection, a feature gate, and drift detection for ComputeClass changes. All behind an alpha feature flag, disabled by default."

---

## Slide 7: Risks & Mitigations

**Say:** "We evaluated six major risks before building this. The biggest ones are API coupling — we mitigate by treating ComputeClass as read-only with graceful fallback — and the two control loops problem — we detect NAP at startup and warn or fail. Every risk has a concrete mitigation."

---

## Slide 8: What's Next

**Say:** "Phase 1 is live. Phase 2 adds priority-aware consolidation, a dry-run CLI, and Prometheus metrics. Phase 3 is reservation awareness and GPU topology support. The code is on GitHub and we're looking for feedback."

---

## Live Demo Commands

### Show the setup

```bash
# Show Karpenter is running with ComputeClass enabled
kubectl get pods -n karpenter-system

# Show the GCENodeClass with ComputeClass reference
kubectl get gcenodeclass cc-test -o yaml | grep -A2 computeClassRef

# Show the ComputeClass priority rules
kubectl get computeclass cc-test-cost-optimized -o yaml
```

### Show the proof (existing NodeClaim)

```bash
# Show the provisioned node — e2-standard-2 spot, matching Priority 0
kubectl get nodeclaims

# Show the ComputeClass annotation — the money shot
kubectl get nodeclaim cc-test-pool-z629n -o jsonpath='{.metadata.annotations}' | python3 -m json.tool
```

### Live provisioning demo (optional, takes ~30 seconds)

```bash
# Delete existing nodeclaim to start fresh
kubectl delete nodeclaim --all

# Wait for Karpenter to re-provision
sleep 5

# Watch it create a new node
kubectl get nodeclaims -w
```

### Cleanup after demo

```bash
kubectl scale deployment cc-test-workload --replicas=0
# Karpenter auto-deletes the empty node after 60 seconds
```

---

## Key Phrases to Emphasize

- "One field change, full backward compatibility"
- "Spot first, with guaranteed fallback"
- "Karpenter's lifecycle management plus GKE's priority intelligence"
- "The annotation proves it — compute-class-name: cc-test-cost-optimized"
- "Alpha feature gate — safe to deploy, easy to roll back"
