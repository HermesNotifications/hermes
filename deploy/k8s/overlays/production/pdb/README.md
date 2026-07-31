# Production PodDisruptionBudgets

Finding 36. Read this before adding one.

## Which workloads get a PDB, and which deliberately do not

Every Deployment in the production overlay runs at 2 or more replicas after finding 8's
capacity half was fixed (`../patches/replicas.yaml`), so every one of them can be covered
without the classic footgun: **a `minAvailable` PDB on a single-replica Deployment permanently
reports `disruptionsAllowed: 0` and blocks node drains forever.** That case does not arise
here, but it would the moment someone adds a workload at `replicas: 1` — so a new PDB is only
correct alongside a replica count of at least 2.

Covered, and why:

| Workload | Why it needs one |
|---|---|
| `hermes-admin`, `hermes-inbox`, `hermes-user` | User- and operator-facing request paths; an eviction storm is an outage. |
| `hermes-send` | Every write enters here. Losing it drops ingestion outright — there is no queue in front of it. |
| `hermes-dispatch` | The only consumer of `NOTIFICATIONS`. Work is durable in JetStream, but the whole pipeline stalls behind it. |
| `hermes-worker-events` | The only writer of notification status. Downtime freezes every notification's status at its last event, which is directly user-visible in the inbox. |
| `hermes-worker-email`, `hermes-worker-sms`, `hermes-worker-inbox` | Delivery is the product. Messages survive in the WorkQueue, so this buys latency rather than durability — which is still the thing customers measure. |
| `centrifugo` | In the request path for every WebSocket client; holds no durable state itself. |
| `nats` | Quorum. See below — it is the one that is different. |

Not covered, on purpose:

- **`hermes-migrate`, `hermes-natsprovision`, the cleanup CronJob.** Jobs, not long-running
  workloads. A PDB over a Job's pods can wedge a drain waiting for a pod that will never be
  replaced.

## `maxUnavailable: 1`, not `minAvailable: 1`

The application PDBs originally all said `minAvailable: 1`. That is a budget that stops
protecting the moment the workload is worth protecting: `minAvailable: 1` on a Deployment the
HPA has taken to 10 replicas allows **nine** concurrent evictions, so a multi-node drain during
a cluster upgrade can take admin from 10 pods to 1 in one step and the PDB reports that as
within budget. It only ever guarantees that the service does not go to *zero*.

`maxUnavailable: 1` states the intended invariant directly and holds at every replica count the
HPA can reach: at most one pod of a given workload is voluntarily disrupted at a time. It is
also drain-safe by construction — with 2 or more healthy replicas it always allows exactly one
disruption, so a drain proceeds, just serially.

## NATS is the exception

`nats-pdb.yaml` keeps `minAvailable: 2` because NATS' requirement is an absolute quorum of a
fixed-size cluster, not a fraction of an elastic one: JetStream's meta layer needs 2 of 3, and
there is no HPA that can change the 3. `maxUnavailable: 1` would be numerically equivalent
today and would silently stop meaning "quorum" if the replica count ever changed.

That PDB only became meaningful when the hard anti-affinity in
`../patches/anti-affinity.yaml` was added. Before it, all three pods could sit on one node,
where the budget protected nothing and simultaneously blocked the drain that would have fixed
the situation.
