# Upgrading Hermes

## General Upgrade Process

1. **Check release notes** for the target version at the [releases page](https://github.com/darylrobbins/hermes/releases).

2. **Back up your database** before upgrading. The migration job runs automatically during `helm upgrade`, and migrations are not reversible.

   It and the NATS stream provisioner are ordinary resources rather than Helm hooks
   ([ADR 0008](../adr/0008-helm-chart-provisioning-jobs-are-not-hooks.md)), applied in the
   same pass as the Deployments. Expect brief restarts while they complete, the same as on a
   first install. Both Jobs are named per release revision (`<release>-migrate-<revision>`),
   because a Job's pod template is immutable and a stable name would fail on the second
   upgrade; Helm prunes the previous revision's Job because it is absent from the new
   manifest.

   ```bash
   pg_dump -h your-db-host -U hermes hermes > hermes-backup-$(date +%Y%m%d).sql
   ```

3. **Update the chart:**

   ```bash
   helm upgrade hermes oci://ghcr.io/hermesnotifications/charts/hermes \
     --namespace hermes \
     --reuse-values \
     --version <target-version> \
     --wait --wait-for-jobs
   ```

   **`--wait-for-jobs` matters here in a way it does not on a fresh install.** `--wait`
   blocks on the Deployments becoming Ready — but on an upgrade they are already Ready before
   the command starts, so `--wait` alone can return while the new migration Job is still
   `ContainerCreating`. On a fresh install the services cannot become Ready until the
   migration has run, so `--wait` covers it there. Add `--atomic` if you want a failed
   upgrade rolled back automatically.

4. **Verify the upgrade:**

   ```bash
   # Check all pods are running
   kubectl get pods -n hermes

   # Run the built-in health tests
   helm test hermes -n hermes
   ```

## Pre-Upgrade Checklist

- [ ] Read the release notes for breaking changes
- [ ] Back up the PostgreSQL database
- [ ] Confirm current deployment is healthy (`helm test`)
- [ ] Review any custom values overrides against new defaults
- [ ] Plan a maintenance window if the release includes database migrations
- [ ] Test the upgrade in a staging environment first

## Rollback

If something goes wrong, roll back to the previous release:

```bash
helm rollback hermes -n hermes
```

Note: Database migrations cannot be rolled back automatically. If the new version included migrations, you may need to restore from your database backup.

## Version-Specific Notes

### 0.1.0

- Initial Helm chart release. No upgrade path needed -- fresh install only.

<!-- Future version-specific notes go here. Template:

### X.Y.Z

- **Breaking:** Description of breaking change and migration steps.
- **New:** Notable new features or configuration options.
- **Deprecated:** Values or features that will be removed in a future release.
-->
