# Upgrading Hermes

## General Upgrade Process

1. **Check release notes** for the target version at the [releases page](https://github.com/darylrobbins/hermes/releases).

2. **Back up your database** before upgrading. The migration job runs automatically during `helm upgrade`, and migrations are not reversible.

   It and the NATS stream provisioner are `post-upgrade` hooks, so they run *after* the new
   pods are rolled out. Expect brief restarts while they complete, the same as on a first
   install. Both Jobs are named per release revision (`<release>-migrate-<revision>`), because
   a Job's pod template is immutable and a stable name would fail on the second upgrade.

   ```bash
   pg_dump -h your-db-host -U hermes hermes > hermes-backup-$(date +%Y%m%d).sql
   ```

3. **Update the chart:**

   ```bash
   helm upgrade hermes oci://ghcr.io/hermesnotifications/charts/hermes \
     --namespace hermes \
     --reuse-values \
     --version <target-version>
   ```

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
