# Load-test node pool

Load-test generator pods must run on a dedicated node pool so they do not compete with Hermes services for CPU, memory, or network.

## Pool spec

- **Name:** `loadtest-generators`
- **Instance type:** `c7g.2xlarge` (Graviton, 8 vCPU, 16 GiB) — matches the project's multi-cloud/ARM preference.
- **Autoscaling:** min 0 / max sized per planned parallelism (each TestRun pod requests 2 vCPU / 4 GiB; budget = `parallelism × 2` vCPU headroom + 20%).
- **Taint:** `loadtest=true:NoSchedule`
- **Label:** `pool=loadtest-generators`

## Taint tolerations

Every pod in the `loadtest` namespace must tolerate the taint and node-select onto the pool. Applied by every manifest in `loadtest/k8s/` via:

```yaml
tolerations:
  - key: loadtest
    operator: Equal
    value: "true"
    effect: NoSchedule
nodeSelector:
  pool: loadtest-generators
```

## IaC

The actual pool is created by Terraform / Crossplane under `infra/` in the staging cluster. This document is the contract; add the pool to the staging cluster's node group spec before running cluster-mode load tests.
