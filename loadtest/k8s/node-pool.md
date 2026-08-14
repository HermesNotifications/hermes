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

The pool is created by Terraform: `aws_eks_node_group.loadtest_generators` in `infra/terraform/modules/eks/main.tf`, driven by the `loadtest_generator_node_pool` variable. It is set only in `infra/terraform/environments/loadtest.tfvars`; the variable defaults to `null`, so staging and production build no pool and generators cannot land there.

The name, taint and label above are the contract between that resource and every manifest in this directory. They are duplicated in two places by necessity — Terraform cannot read these YAML files and Kustomize cannot read Terraform state — so changing one means changing the other in the same commit. A mismatch does not error; generator pods simply stay `Pending`.

### Availability zone

The pool schedules into the same subnets as the main node group. In the loadtest environment that is a single subnet in a single AZ (`single_az_workloads = true`), which is deliberate: generator-to-service traffic *is* the load test, and running the generators in another AZ would bill every request twice in each direction while measuring a network path production does not have.

Autoscaling has one consequence worth knowing: with the pool confined to one AZ, a capacity shortfall for `c7g.2xlarge` in that AZ cannot be absorbed by another one. The failure is visible — nodes stay unschedulable rather than quietly landing elsewhere — but it does mean a run can be blocked by AZ capacity.
