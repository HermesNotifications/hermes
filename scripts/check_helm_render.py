#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if the rendered Helm chart contradicts the Go source it is supposed to deploy.

The chart's only existing CI control is `helm template ... > /dev/null`
(.github/workflows/ci.yml). That proves the templates parse. It asserts nothing about what
they produce, which is how the chart reached main in a state where `helm install`
crash-loops six services at boot, routes two paths no handler has served since the
group/type rename, and has no rule at all for four admin endpoints. Every one of those
renders perfectly.

So this gate does not check the chart against a list someone wrote down. Wherever it can,
it reads the answer out of the Go source at check time:

  * the route table comes from the `Path:` fields of the huma operations in
    internal/{admin,inbox,userservice,send};
  * the set of services that cannot start without JetStream streams comes from
    messaging.StreamsForService, and the provisioner's identity from
    messaging.ProvisionerService;
  * the accepted email providers come from the switch in email.NewProvider;
  * the set of images Hermes actually publishes comes from the release.yml build matrix --
    the *public* one, because ghcr.io is where the chart's default registry points and so
    the only registry a self-hoster can pull from. cd.yml's matrix feeds a private ECR; the
    two are asserted to agree, since a service in one and not the other is a defect either
    way round.

What remains hardcoded is the map from a Go package directory to the service identity it
runs as (SERVICE_SOURCE_DIRS below) and the registry prefix. Both are named in comments
where they sit, with what will rot if they change.

Usage:
    helm template rel charts/hermes/ ... | check_helm_render.py - --source-root .
"""

import base64
import os
import re
import sys
from collections import namedtuple

# --------------------------------------------------------------------------------------
# The two hardcodes, and what rots.
# --------------------------------------------------------------------------------------

# Go package directory -> the service identity that package runs as, which is also the
# `app.kubernetes.io/name` label the chart puts on that service's Service and pods.
#
# ROT: adding a new HTTP service, or renaming internal/userservice, silently drops its
# routes from the gate. The `source_problems` guard below catches a directory that yields
# no routes at all, so a rename is loud; a brand new service is not, because the gate
# cannot know about a package nobody told it about. That is the residual gap.
SERVICE_SOURCE_DIRS = {
    "internal/admin": "hermes-admin",
    "internal/inbox": "hermes-inbox",
    # The odd one out: package `userservice`, service identity `hermes-user`.
    "internal/userservice": "hermes-user",
    "internal/send": "hermes-send",
}

# ROT: this must track charts/hermes/values.yaml `global.image.registry`. If the chart's
# default registry moves, the "does Hermes publish this?" check goes quiet rather than
# wrong — every image stops looking like a Hermes image. Kept as a CLI flag so the
# Makefile can pass whatever the chart is actually set to.
DEFAULT_REGISTRY = "ghcr.io/hermesnotifications"

# config.EnvDevelopment. Only this literal relaxes Validate(); anything else, including a
# typo, takes the strict path. Mirrored here so the gate agrees with the binary.
ENV_DEVELOPMENT = "development"

Source = namedtuple(
    "Source",
    [
        "routes",
        "stream_services",
        "provisioner",
        "email_providers",
        # release.yml -- the public GHCR matrix, which is what the chart's default registry
        # resolves against and therefore the only one a self-hoster can pull from.
        "published_images",
        # cd.yml -- the private ECR matrix. Checked only for agreement with the above.
        "internal_images",
        "registry",
    ],
)


# --------------------------------------------------------------------------------------
# Reading the Go and workflow source.
# --------------------------------------------------------------------------------------

_ROUTE_RE = re.compile(r'\bPath:\s*"(/v1[^"]*)"')


def parse_go_routes(text):
    """Every /v1 path registered as a huma operation in one file's source."""
    return set(_ROUTE_RE.findall(text))


def parse_stream_services(text):
    """Service identities in messaging.StreamsForService.

    These are exactly the services that call EnsureStreams and os.Exit if a stream is
    missing, so they are exactly the services a chart install must provision streams for.
    """
    start = text.find("var StreamsForService = map[string][]string{")
    if start < 0:
        return set()
    end = text.find("\n}", start)
    body = text[start:end if end > 0 else len(text)]
    return set(re.findall(r'^\s*"([^"]+)":', body, re.MULTILINE))


def parse_provisioner_identity(text):
    """messaging.ProvisionerService — the identity the provisioner Job's pods must carry."""
    match = re.search(r'\bconst\s+ProvisionerService\s*=\s*"([^"]+)"', text)
    return match.group(1) if match else None


def parse_email_providers(text):
    """The provider strings email.NewProvider accepts, read off its switch arms."""
    start = text.find("func NewProvider(")
    if start < 0:
        return set()
    end = text.find("default:", start)
    body = text[start:end if end > 0 else len(text)]
    return set(re.findall(r'^\s*case\s+"([^"]+)":', body, re.MULTILINE))


def parse_published_images(text):
    """Image names a workflow's build matrix actually builds and pushes, as `hermes-<service>`.

    An image reference under the Hermes registry that is not in this set cannot be pulled,
    however well-formed it looks. That is not a hypothetical: the NATS sub-chart reads the
    same `global.image.registry` key Hermes sets, so the bundled bus rendered as
    ghcr.io/hermesnotifications/nats:2.10.26-alpine.
    """
    start = text.find("      matrix:")
    if start < 0:
        return set()
    # The matrix block ends at the next key at `    ` depth -- `env:` in cd.yml, `steps:` in
    # release.yml. Take whichever comes first rather than naming one of them.
    ends = [text.find(f"\n    {key}:", start) for key in ("env", "steps")]
    ends = [e for e in ends if e > 0]
    body = text[start:min(ends) if ends else len(text)]
    return {"hermes-" + s for s in re.findall(r"^\s*-\s+([a-z0-9][a-z0-9-]*)\s*$", body, re.MULTILINE)}


def _read(root, relpath):
    try:
        with open(os.path.join(root, relpath), encoding="utf-8") as handle:
            return handle.read()
    except OSError:
        return ""


def load_source(root):
    """Assemble everything the gate needs to know from the repository itself."""
    routes = {}
    for directory, identity in SERVICE_SOURCE_DIRS.items():
        found = set()
        try:
            names = sorted(os.listdir(os.path.join(root, directory)))
        except OSError:
            names = []
        for name in names:
            if name.endswith(".go") and not name.endswith("_test.go"):
                found |= parse_go_routes(_read(root, os.path.join(directory, name)))
        routes[identity] = found

    provision = _read(root, "internal/messaging/provision.go")
    return Source(
        routes=routes,
        stream_services=parse_stream_services(provision),
        provisioner=parse_provisioner_identity(provision),
        email_providers=parse_email_providers(_read(root, "internal/email/email.go")),
        published_images=parse_published_images(_read(root, ".github/workflows/release.yml")),
        internal_images=parse_published_images(_read(root, ".github/workflows/cd.yml")),
        registry=DEFAULT_REGISTRY,
    )


def source_problems(source):
    """Reasons the gate would be checking against nothing.

    Every one of these means a silent pass, which is the exact failure mode this script
    exists to close — so they are errors, not warnings.
    """
    problems = []
    for identity, paths in sorted(source.routes.items()):
        if not paths:
            problems.append(f"no /v1 route found for {identity}; has its package moved?")
    if not source.routes:
        problems.append("no route sources configured at all")
    if not source.stream_services:
        problems.append("messaging.StreamsForService not found in internal/messaging/provision.go")
    if not source.provisioner:
        problems.append("messaging.ProvisionerService not found in internal/messaging/provision.go")
    if not source.email_providers:
        problems.append("email.NewProvider switch not found in internal/email/email.go")
    if not source.published_images:
        problems.append("build matrix not found in .github/workflows/release.yml")
    if not source.internal_images:
        problems.append("build matrix not found in .github/workflows/cd.yml")

    # The two pipelines build the same Dockerfile from the same source and differ only in where
    # they push. A service in one and not the other is a defect either way round: missing from
    # release.yml it is unpullable by every self-hoster, and missing from cd.yml it breaks
    # Kargo's promotion. Neither shows up until someone deploys.
    if source.published_images and source.internal_images:
        public_only = source.published_images - source.internal_images
        internal_only = source.internal_images - source.published_images
        if public_only:
            problems.append(
                "in release.yml's matrix but not cd.yml's: "
                + ", ".join(sorted(public_only))
                + " (published to GHCR but never delivered to ECR, so Kargo cannot promote it)"
            )
        if internal_only:
            problems.append(
                "in cd.yml's matrix but not release.yml's: "
                + ", ".join(sorted(internal_only))
                + " (delivered to ECR but never published, so no self-hoster can pull it)"
            )
    return problems


# --------------------------------------------------------------------------------------
# Path prefix semantics.
# --------------------------------------------------------------------------------------


def segments(path):
    return tuple(s for s in path.split("/") if s)


def prefixes(rule, route):
    """Whether an ingress rule with pathType: Prefix would match `route`.

    Element-wise, matching Kubernetes: /v1/user does not match /v1/users. A string-prefix
    implementation would be permissive in exactly the direction that lets a dead rule look
    live.
    """
    rule_parts, route_parts = segments(rule), segments(route)
    return len(rule_parts) <= len(route_parts) and route_parts[: len(rule_parts)] == rule_parts


def longest_match(rules, route):
    """The rule Kubernetes would route `route` to — longest prefix wins.

    This is the whole reason /v1/users can go to admin while /v1/users/me goes to the user
    service, which is what deploy/k8s/base/ingress.yaml does and the chart did not.
    """
    candidates = [r for r in rules if prefixes(r, route)]
    return max(candidates, key=lambda r: len(segments(r))) if candidates else None


# --------------------------------------------------------------------------------------
# Rendered-manifest helpers.
# --------------------------------------------------------------------------------------

_WORKLOAD_KINDS = ("Deployment", "StatefulSet", "DaemonSet", "Job", "ReplicaSet")


def pod_templates(docs):
    """(owner name, kind, pod labels, containers) for every document that creates a pod.

    Separate from check_networkpolicy_selectors.workloads because that one needs only
    labels; this needs the container list too, and needs bare Pods (helm test hooks render
    as Pods, and they pull images like anything else).
    """
    out = []
    for doc in docs:
        kind = doc.get("kind")
        meta = doc.get("metadata") or {}
        name = meta.get("name", "?")
        spec = doc.get("spec") or {}

        if kind == "Pod":
            template_meta, pod_spec = meta, spec
        elif kind == "CronJob":
            template = ((spec.get("jobTemplate") or {}).get("spec") or {}).get("template") or {}
            template_meta, pod_spec = template.get("metadata") or {}, template.get("spec") or {}
        elif kind in _WORKLOAD_KINDS:
            template = spec.get("template") or {}
            template_meta, pod_spec = template.get("metadata") or {}, template.get("spec") or {}
        else:
            continue

        containers = list(pod_spec.get("initContainers") or []) + list(pod_spec.get("containers") or [])
        out.append((name, kind, template_meta.get("labels") or {}, containers))
    return out


def _app_name(labels):
    return labels.get("app.kubernetes.io/name")


def service_backends(docs):
    """Rendered Service name -> the service identity it fronts.

    Read off `app.kubernetes.io/name`, which the chart's helpers set to
    `<chart name>-<service>` — the same string messaging.StreamsForService keys on and the
    same string SERVICE_SOURCE_DIRS maps to. Deriving it this way rather than by string
    surgery on the release name is what keeps the ingress check honest under a rename.

    The kustomize overlays label Services with `part-of` only, and there the Service is
    already *named* for its identity (`hermes-admin`), so its own name is the answer. The
    fallback is deliberately only reached when the label is absent: under the chart the
    Service name carries a release prefix and would be the wrong string, so preferring the
    label keeps that case honest.
    """
    out = {}
    for doc in docs:
        if doc.get("kind") != "Service":
            continue
        meta = doc.get("metadata") or {}
        name = meta.get("name")
        out[name] = _app_name(meta.get("labels") or {}) or name
    return out


def ingress_rules(docs):
    """(ingress name, path, backend service name) for every rule in every Ingress."""
    out = []
    for doc in docs:
        if doc.get("kind") != "Ingress":
            continue
        name = (doc.get("metadata") or {}).get("name", "?")
        for rule in (doc.get("spec") or {}).get("rules") or []:
            for path in ((rule.get("http") or {}).get("paths") or []):
                backend = ((path.get("backend") or {}).get("service") or {}).get("name")
                out.append((name, path.get("path", ""), backend))
    return out


def hermes_configmaps(docs):
    """ConfigMaps carrying Hermes service configuration.

    Identified by content rather than by name so a fullnameOverride cannot hide one, and
    so the sub-charts' own ConfigMaps are not mistaken for it.
    """
    out = []
    for doc in docs:
        if doc.get("kind") != "ConfigMap":
            continue
        data = doc.get("data") or {}
        if any(k.startswith("HERMES_") for k in data):
            out.append(((doc.get("metadata") or {}).get("name", "?"), data))
    return out


def secret_values(docs):
    """Merged plaintext of every rendered Secret, stringData and base64 data alike."""
    out = {}
    for doc in docs:
        if doc.get("kind") != "Secret":
            continue
        out.update(doc.get("stringData") or {})
        for key, value in (doc.get("data") or {}).items():
            try:
                out[key] = base64.b64decode(value).decode("utf-8")
            except (ValueError, UnicodeDecodeError):
                continue
    return out


# --------------------------------------------------------------------------------------
# The checks.
# --------------------------------------------------------------------------------------


def check_ingress_routes(docs, routes_by_identity):
    """Both directions: no unreachable handler, and no rule pointing at nothing.

    Only /v1 rules are considered. /realtime goes to Centrifugo, which serves no Go route
    in this repository, and flagging it would make the gate cry wolf.
    """
    backends = service_backends(docs)
    rules = [(path, backend) for _, path, backend in ingress_rules(docs) if path.startswith("/v1")]
    failures = []

    for path, backend in rules:
        if backend not in backends:
            failures.append(
                f"ingress rule {path} points at Service {backend!r}, which this chart does not render"
            )

    rule_paths = [path for path, _ in rules]
    target = {path: backends.get(backend) for path, backend in rules}

    for identity, paths in sorted(routes_by_identity.items()):
        for route in sorted(paths):
            match = longest_match(rule_paths, route)
            if match is None:
                failures.append(
                    f"{route} is served by {identity} but no ingress rule reaches it"
                )
            elif target.get(match) != identity:
                failures.append(
                    f"{route} is served by {identity} but the longest matching ingress rule "
                    f"{match} sends it to {target.get(match)!r}"
                )

    every_route = [r for paths in routes_by_identity.values() for r in paths]
    for path in sorted(set(rule_paths)):
        if not any(prefixes(path, route) for route in every_route):
            failures.append(f"ingress rule {path} matches no handler; no Go route lives under it")

    return failures


def check_provisioner(docs, stream_services, provisioner_identity):
    """A stream-consuming service without a provisioner Job is a guaranteed crash-loop.

    messaging.EnsureStreams cannot create a stream and exits non-zero when one is missing
    (ADR 0005 phase 4), so the chart needs a Job that declares them.

    It must be a plain tracked resource, not a Helm hook — ADR 0008. Neither phase works:

      * pre-install runs before the release's regular resources exist, so with the bundled
        sub-charts there is no bus to provision against, and no ConfigMap/Secret to read
        credentials from. Both observed in-cluster.
      * post-install runs only after Helm has waited for every regular resource to be
        Ready, and under `--wait`/`--atomic` the stream consumers can never become Ready
        until this Job has run. Measured: `helm install --wait --timeout 4m` failed with
        `context deadline exceeded`, no Job was ever created, no streams existed, and all
        nine services were in CrashLoopBackOff.

    A render-time check cannot see the deadlock itself. It can see the annotation that
    causes it, which is the whole reason this is phrased as "must not be a hook" rather
    than "must be the right hook".
    """
    enabled = {
        _app_name(labels)
        for _, _, labels, _ in pod_templates(docs)
        if _app_name(labels) in stream_services
    }
    if not enabled:
        return []

    for name, _, labels, _ in pod_templates(docs):
        if _app_name(labels) != provisioner_identity:
            continue
        annotations = _annotations_for(docs, name)
        hook = annotations.get("helm.sh/hook", "")
        if hook:
            return [
                f"{provisioner_identity} Job {name} carries helm.sh/hook={hook!r}; it must "
                "be a plain tracked resource (ADR 0008). A pre- phase runs before the bus "
                "and the ConfigMap exist; a post- phase never runs at all under `--wait` "
                "or `--atomic`, because Helm is blocked waiting for the very services this "
                "Job unblocks."
            ]
        return []

    return [
        f"no {provisioner_identity} Job, but {', '.join(sorted(enabled))} "
        f"{'is' if len(enabled) == 1 else 'are'} enabled and refuse to start without "
        "JetStream streams (messaging.EnsureStreams cannot create them)"
    ]


def _annotations_for(docs, name):
    for doc in docs:
        if (doc.get("metadata") or {}).get("name") == name:
            return (doc.get("metadata") or {}).get("annotations") or {}
    return {}


def _is_pinned(image):
    """Whether an image reference names a specific version.

    The last path element must carry a tag or a digest. Splitting on the last '/' first is
    what stops `localhost:5000/img` reading as tagged — the colon is a registry port.
    """
    last = image.rsplit("/", 1)[-1]
    return ":" in last or "@" in last


def check_images(docs, published_images, registry):
    """Every image must be pinned, and every Hermes-registry image must actually exist.

    The second half is not pedantry. Helm merges the parent chart's `global` into every
    sub-chart, and the NATS chart reads `global.image.registry` — the same key Hermes uses
    for its own images. The bundled bus therefore rendered as
    ghcr.io/hermesnotifications/nats:2.10.26-alpine, which nobody publishes. `helm template`
    is content; the StatefulSet is ImagePullBackOff.
    """
    failures = []
    prefix = registry.rstrip("/") + "/"
    for owner, kind, _, containers in pod_templates(docs):
        for container in containers:
            image = container.get("image") or ""
            where = f"{kind}/{owner} container {container.get('name', '?')!r}"
            if not image:
                failures.append(f"{where} has an empty image reference")
                continue
            if not _is_pinned(image):
                failures.append(f"{where} image {image!r} is not tagged or digest-pinned")
                continue
            if image.startswith(prefix):
                repo = image[len(prefix):].rsplit("@", 1)[0].rsplit(":", 1)[0]
                if repo not in published_images:
                    failures.append(
                        f"{where} image {image!r} sits under the Hermes registry, but Hermes "
                        f"does not publish {repo!r} (.github/workflows/cd.yml build matrix). "
                        "A sub-chart inheriting global.image.registry does this."
                    )
    return failures


def check_config(docs, email_providers):
    """HERMES_ENV must be set, and the render must be one config.Validate() would accept.

    HERMES_ENV defaults to "development" in config.go, so a chart that never sets it puts
    a self-hoster in development mode without saying so — placeholder secrets tolerated,
    plaintext datastores tolerated. Setting it explicitly is the point.

    The datastore assertions mirror config.Validate() rather than restating a policy: they
    are what the binary will do, so a render that fails them is a crash-loop the chart
    should have refused to produce.
    """
    failures = []
    for name, data in hermes_configmaps(docs):
        env = data.get("HERMES_ENV")
        if env is None:
            failures.append(
                f"ConfigMap {name} does not set HERMES_ENV; config.go then defaults it to "
                f"{ENV_DEVELOPMENT!r} and a self-hoster runs relaxed validation unknowingly"
            )

        provider = data.get("HERMES_EMAIL_PROVIDER")
        if provider is not None and provider not in email_providers:
            failures.append(
                f"ConfigMap {name} sets HERMES_EMAIL_PROVIDER={provider!r}, which "
                f"email.NewProvider rejects (it accepts {sorted(email_providers)}); the "
                "email worker would exit with 'unknown email provider'"
            )

        if env is not None and env != ENV_DEVELOPMENT:
            nats = data.get("HERMES_NATS_URL", "")
            redis = data.get("HERMES_REDIS_URL", "")
            if nats and not nats.startswith("tls://"):
                failures.append(
                    f"ConfigMap {name} has HERMES_ENV={env!r} but HERMES_NATS_URL={nats!r} "
                    "is plaintext; config.Validate() rejects it and every service exits"
                )
            if redis and not redis.startswith("rediss://"):
                failures.append(
                    f"ConfigMap {name} has HERMES_ENV={env!r} but HERMES_REDIS_URL={redis!r} "
                    "is plaintext; config.Validate() rejects it and every service exits"
                )
    return failures


def _hook_phases(doc):
    """The helm.sh/hook value, tolerating an annotations block that is explicitly null.

    `or {}` rather than a default argument: real renders contain `annotations:` with
    nothing under it (the centrifugo sub-chart emits exactly that), and `.get(k, {})`
    returns None in that case, not {}.
    """
    annotations = (doc.get("metadata") or {}).get("annotations") or {}
    return annotations.get("helm.sh/hook", "")


def _config_references(container):
    """(kind, name) for every ConfigMap/Secret a container reads."""
    for entry in container.get("envFrom") or []:
        for kind in ("configMapRef", "secretRef"):
            if kind in entry:
                yield kind, (entry[kind] or {}).get("name")
    for entry in container.get("env") or []:
        source = entry.get("valueFrom") or {}
        for kind in ("configMapKeyRef", "secretKeyRef"):
            if kind in source:
                yield kind, (source[kind] or {}).get("name")


def check_hook_config_refs(docs):
    """A pre-install hook must not read a resource Helm creates after the hooks.

    Helm applies a release's regular manifests only once its pre-install hooks have
    finished, so a hook Job referencing the release ConfigMap gets a pod that never leaves
    CreateContainerConfigError and an install that dies on a timeout saying nothing useful.
    Confirmed in-cluster on k3s v1.34.

    This existed in migration-job.yaml from the day it was written, which means a fresh
    `helm install` of this chart had never once run its database migrations. Nothing could
    have caught it short of installing: it renders, it lints, and it applies.

    A reference to a name the chart does not render at all is fine — that is a
    user-supplied existingSecret, which is in the cluster before install begins.
    """
    rendered = {}
    for doc in docs:
        if doc.get("kind") in ("ConfigMap", "Secret"):
            rendered[(doc.get("kind"), (doc.get("metadata") or {}).get("name"))] = _hook_phases(doc)

    kind_of = {"configMapRef": "ConfigMap", "configMapKeyRef": "ConfigMap",
               "secretRef": "Secret", "secretKeyRef": "Secret"}

    failures = []
    for doc in docs:
        if "pre-install" not in _hook_phases(doc) and "pre-upgrade" not in _hook_phases(doc):
            continue
        owner = (doc.get("metadata") or {}).get("name", "?")
        for _, _, _, containers in pod_templates([doc]):
            for container in containers:
                for ref_kind, name in _config_references(container):
                    phases = rendered.get((kind_of[ref_kind], name))
                    if phases is None:
                        continue  # not rendered by this chart; it pre-exists
                    if "pre-install" not in phases and "pre-upgrade" not in phases:
                        failures.append(
                            f"pre-install hook {owner} reads {kind_of[ref_kind]} {name!r}, "
                            "which Helm does not create until after the hooks have run; the "
                            "pod sits in CreateContainerConfigError and the install times out"
                        )
    return failures


def check_rewrite_targets(docs):
    """An nginx rewrite-target using a capture group needs use-regex to have one.

    The failure is entirely silent. `rewrite-target: /$2` against `path: /realtime(/|$)(.*)`
    looks correct and renders without complaint, but without `use-regex: "true"` nginx treats
    the path as a literal prefix, never compiles the groups, and substitutes empty for `$2` --
    so every websocket request is rewritten to `/` and the realtime route does not work at all.
    The kustomize overlays set it; the chart shipped without it.
    """
    failures = []
    for doc in docs:
        if doc.get("kind") != "Ingress":
            continue
        meta = doc.get("metadata") or {}
        annotations = meta.get("annotations") or {}
        target = annotations.get("nginx.ingress.kubernetes.io/rewrite-target", "")
        if "$" not in target:
            continue
        if annotations.get("nginx.ingress.kubernetes.io/use-regex") != "true":
            failures.append(
                f"Ingress {meta.get('name', '?')!r} sets rewrite-target {target!r}, which "
                "refers to a regex capture group, but does not set "
                'nginx.ingress.kubernetes.io/use-regex: "true". nginx will treat the path as a '
                "literal prefix, capture nothing, and rewrite every request to the substituted "
                "empty value — silently breaking the route while the manifest looks right."
            )
    return failures


def _rendered_secret_keys(docs):
    """Keys in each Secret this render creates, from either stringData or data."""
    secrets = {}
    for doc in docs:
        if doc.get("kind") != "Secret":
            continue
        name = (doc.get("metadata") or {}).get("name")
        if not name:
            continue
        keys = set((doc.get("stringData") or {}).keys())
        keys |= set((doc.get("data") or {}).keys())
        secrets[name] = keys
    return secrets


def _iter_env_refs(docs):
    """Every secretKeyRef in the render, as (workload, container, env name, ref)."""
    for owner, kind, _, containers in pod_templates(docs):
        for container in containers:
            for entry in container.get("env") or []:
                ref = (entry.get("valueFrom") or {}).get("secretKeyRef")
                if ref:
                    yield f"{kind}/{owner}", container.get("name", "?"), entry.get("name", "?"), ref


def check_secret_refs(docs):
    """A secretKeyRef naming a key the chart does not put in that Secret.

    Only Secrets this render CREATES are checked. A reference to an externally-supplied Secret
    (externalPostgresql.existingSecret and friends) names something the chart cannot see, and
    asserting about its contents would be pretending to knowledge we do not have.

    Where the chart owns both sides, a missing key is unambiguous — and invisible to every
    other gate. `helm template` renders it, `helm lint` accepts it, and the failure arrives
    when the kubelet builds the container: CreateContainerConfigError, on a pod whose manifest
    reads correctly. That is how the bundled Centrifugo shipped a reference to
    HERMES_CENTRIFUGO_API_KEY, a key templates/secret.yaml only wrote when it happened to be
    set.
    """
    failures = []
    secrets = _rendered_secret_keys(docs)

    for owner, container, env_name, ref in _iter_env_refs(docs):
        target, key = ref.get("name"), ref.get("key")
        if target not in secrets:
            continue  # externally supplied; not ours to judge
        if ref.get("optional"):
            continue
        if key not in secrets[target]:
            failures.append(
                f"{owner} container {container!r} env {env_name!r} reads {target}/{key}, "
                f"but this chart creates {target} without that key "
                f"(it has: {', '.join(sorted(secrets[target])) or 'nothing'}). The pod would "
                "sit in CreateContainerConfigError; a rendered manifest cannot show this."
            )
    return failures


def check_realtime_prefix_strip(docs):
    """The realtime route must actually strip /realtime, whichever controller renders it.

    Centrifugo serves /connection/... at its root, so the ingress has to remove the /realtime
    prefix. nginx does it with a regex path plus rewrite-target; Traefik v3 removed regex from
    Ingress paths entirely, so the nginx form matches nothing there and /realtime returns 404
    while every other route works.

    Both failures are silent in the same way: the Ingress is accepted, the widget connects to
    nothing, falls down the whole transport ladder (ADR 0017) and reports itself disconnected,
    with no error anywhere that names the ingress. So this asserts the mechanism is present
    and matches the dialect the rest of the manifest is written in.
    """
    failures = []
    middlewares = {
        (doc.get("metadata") or {}).get("name")
        for doc in docs
        if doc.get("kind") == "Middleware" and "stripPrefix" in (doc.get("spec") or {})
    }

    for doc in docs:
        if doc.get("kind") != "Ingress":
            continue
        meta = doc.get("metadata") or {}
        name = meta.get("name", "?")
        annotations = meta.get("annotations") or {}

        paths = [
            path
            for rule in (doc.get("spec") or {}).get("rules") or []
            for path in ((rule.get("http") or {}).get("paths") or [])
            if str(path.get("path", "")).startswith("/realtime")
        ]
        if not paths:
            continue

        traefik_ref = annotations.get("traefik.ingress.kubernetes.io/router.middlewares", "")
        nginx_rewrite = annotations.get("nginx.ingress.kubernetes.io/rewrite-target", "")

        if traefik_ref:
            # <namespace>-<name>@kubernetescrd. A bare name resolves to nothing and Traefik
            # skips the middleware without complaint.
            if not traefik_ref.endswith("@kubernetescrd"):
                failures.append(
                    f"Ingress {name!r} references middleware {traefik_ref!r}, which is not in "
                    "Traefik's <namespace>-<name>@kubernetescrd form. Traefik resolves it to "
                    "nothing and skips it, so /realtime reaches Centrifugo unstripped."
                )
                continue
            referenced = traefik_ref.rsplit("@", 1)[0]
            if not any(referenced.endswith(m) for m in middlewares):
                failures.append(
                    f"Ingress {name!r} references stripPrefix middleware {referenced!r}, but no "
                    f"Middleware with a stripPrefix spec renders. Found: {sorted(middlewares)}."
                )
            for path in paths:
                if "(" in str(path.get("path", "")):
                    failures.append(
                        f"Ingress {name!r} is annotated for Traefik but its path "
                        f"{path['path']!r} is an nginx regex. Traefik v3 removed regex from "
                        "Ingress paths and matches this literally, so the route never fires."
                    )
        elif not nginx_rewrite:
            failures.append(
                f"Ingress {name!r} routes {paths[0]['path']!r} to Centrifugo but neither strips "
                "the prefix (nginx rewrite-target) nor references a Traefik stripPrefix "
                "middleware. Centrifugo would receive /realtime/... and 404 every connection."
            )
    return failures


# CHECKS maps a --only name to the check it selects. The names are part of the CLI, so
# renaming one is a breaking change to whatever invokes this.
CHECK_NAMES = (
    "routes", "rewrites", "realtime", "provisioner", "images", "config", "hook-refs",
    "secret-refs",
)


def evaluate(docs, source, only=None):
    """Run the selected checks — every one by default. Returns (failures, stats)."""
    enabled = set(only) if only else set(CHECK_NAMES)

    failures = []
    if "routes" in enabled:
        failures += check_ingress_routes(docs, source.routes)
    if "rewrites" in enabled:
        failures += check_rewrite_targets(docs)
    if "realtime" in enabled:
        failures += check_realtime_prefix_strip(docs)
    if "secret-refs" in enabled:
        failures += check_secret_refs(docs)
    if "provisioner" in enabled:
        failures += check_provisioner(docs, source.stream_services, source.provisioner)
    if "images" in enabled:
        failures += check_images(docs, source.published_images, source.registry)
    if "config" in enabled:
        failures += check_config(docs, source.email_providers)
    if "hook-refs" in enabled:
        failures += check_hook_config_refs(docs)

    templates = pod_templates(docs)
    stats = {
        "workloads": len(templates),
        "ingress_rules": len(ingress_rules(docs)),
        "routes": sum(len(p) for p in source.routes.values()),
    }
    return failures, stats


def main(argv):
    paths = [a for a in argv if not a.startswith("--")]
    root = "."
    registry = DEFAULT_REGISTRY
    only = None
    for arg in argv:
        if arg.startswith("--source-root="):
            root = arg.split("=", 1)[1]
        elif arg.startswith("--registry="):
            registry = arg.split("=", 1)[1]
        elif arg.startswith("--only="):
            only = [c.strip() for c in arg.split("=", 1)[1].split(",") if c.strip()]
            unknown = [c for c in only if c not in CHECK_NAMES]
            if unknown:
                print(
                    f"ERROR: unknown --only check(s): {', '.join(unknown)}\n"
                    f"  known checks: {', '.join(CHECK_NAMES)}",
                    file=sys.stderr,
                )
                return 2

    try:
        import yaml
    except ImportError:
        # Unlike the NetworkPolicy gate, this one does not skip. That gate predates the
        # discovery that a control which quietly does not run is worse than no control;
        # this one is that discovery's consequence.
        print("ERROR: PyYAML not installed; cannot check the rendered chart", file=sys.stderr)
        return 1

    docs = []
    for path in paths:
        stream = sys.stdin if path == "-" else open(path, encoding="utf-8")
        docs.extend(doc for doc in yaml.safe_load_all(stream) if doc)

    source = load_source(root)._replace(registry=registry)

    problems = source_problems(source)
    if problems:
        print("ERROR: could not read the Go source this chart is checked against:", file=sys.stderr)
        for problem in problems:
            print(f"  {problem}", file=sys.stderr)
        print(f"\n(--source-root={root!r}; pass the repository root)", file=sys.stderr)
        return 1

    failures, stats = evaluate(docs, source, only)

    if stats["workloads"] == 0:
        print("ERROR: no pod-producing workloads found; is this a rendered chart?", file=sys.stderr)
        return 1
    if stats["ingress_rules"] == 0:
        print("ERROR: no Ingress rules found; render with ingress.enabled=true", file=sys.stderr)
        return 1

    if failures:
        print(f"FAIL: {len(failures)} problems in the rendered chart:\n", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        print(
            "\nEach of these renders cleanly and fails at install or at request time.\n"
            "`helm template > /dev/null` cannot see any of them.",
            file=sys.stderr,
        )
        return 1

    print(
        f"ok: {stats['routes']} Go routes reachable across {stats['ingress_rules']} ingress rules; "
        f"{stats['workloads']} workloads checked"
    )
    return 0


if __name__ == "__main__":
    args = sys.argv[1:]
    if not [a for a in args if not a.startswith("--")]:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    sys.exit(main(args))
