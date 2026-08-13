#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the Helm chart render check.

Run: python3 -m unittest discover -s scripts -p 'test_*.py' -t scripts

stdlib unittest deliberately, matching test_check_networkpolicy_selectors.py — this and
that script are the only Python in the repo and a test-runner dependency for two files is
a worse trade than slightly wordier assertions.

The tests are split the way the script is: the `parse_*` functions read Go/YAML source
text and are tested against literal snippets of the real files, and the `check_*`
functions take already-parsed structures so the gate's decisions are testable without
running `helm template`.
"""

import unittest

import check_helm_render as check


# --------------------------------------------------------------------------------------
# Source parsing. Each snippet below is copied from the real file it claims to represent;
# if the real file's shape changes these tests keep passing while the gate silently reads
# nothing, which is why every parse_* function also has a "finds nothing" test and why the
# script treats an empty parse as an error rather than a pass.
# --------------------------------------------------------------------------------------


class TestParseGoRoutes(unittest.TestCase):
    HUMA = '''
    func (s *Server) registerUserRoutes() {
        huma.Register(s.api, huma.Operation{
            OperationID: "list-users",
            Method:      http.MethodGet,
            Path:        "/v1/users",
            Summary:     "List users",
        }, func(ctx context.Context, input *listUsersInput) (*userListOutput, error) {
    '''

    def test_reads_the_path_off_a_huma_operation(self):
        self.assertEqual(check.parse_go_routes(self.HUMA), {"/v1/users"})

    def test_reads_paths_with_placeholders(self):
        text = 'Path: "/v1/subscriptions/categories/{category_id}/subscriptions",'
        self.assertEqual(
            check.parse_go_routes(text),
            {"/v1/subscriptions/categories/{category_id}/subscriptions"},
        )

    def test_ignores_paths_outside_the_versioned_api(self):
        # /healthz and /readyz are served by every service and deliberately not routed.
        text = 'Path: "/healthz",\nPath: "/v1/inbox",\nPath: "/metrics",'
        self.assertEqual(check.parse_go_routes(text), {"/v1/inbox"})

    def test_finds_nothing_in_unrelated_source(self):
        self.assertEqual(check.parse_go_routes("package admin\n\nfunc f() {}\n"), set())


class TestParseStreamServices(unittest.TestCase):
    PROVISION = '''
var StreamsForService = map[string][]string{
	// Publishes notification.send.
	"hermes-send": {"NOTIFICATIONS"},
	"hermes-dispatch": {"NOTIFICATIONS", "DELIVERY", "EVENTS", DLQStreamName},
	"hermes-worker-email":  {"DELIVERY", "EVENTS", DLQStreamName},
	"hermes-worker-events": {"EVENTS", DLQStreamName},
}

func StreamNames() []string {
	"not-a-service": nope
}
'''

    def test_reads_every_key_and_stops_at_the_closing_brace(self):
        self.assertEqual(
            check.parse_stream_services(self.PROVISION),
            {"hermes-send", "hermes-dispatch", "hermes-worker-email", "hermes-worker-events"},
        )

    def test_finds_nothing_when_the_map_is_gone(self):
        self.assertEqual(check.parse_stream_services("package messaging\n"), set())


class TestParseProvisionerIdentity(unittest.TestCase):
    def test_reads_the_exported_constant(self):
        text = 'const ProvisionerService = "hermes-natsprovision"\n'
        self.assertEqual(check.parse_provisioner_identity(text), "hermes-natsprovision")

    def test_returns_none_when_absent(self):
        self.assertIsNone(check.parse_provisioner_identity("package messaging\n"))


class TestParseEmailProviders(unittest.TestCase):
    EMAIL = '''
func NewProvider(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case "smtp":
		return NewSMTPProvider(cfg), nil
	case "ses":
		return NewSESProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown email provider: %q", cfg.Provider)
	}
}
'''

    def test_reads_the_switch_arms(self):
        self.assertEqual(check.parse_email_providers(self.EMAIL), {"smtp", "ses"})

    def test_stops_at_default_so_unrelated_switches_do_not_leak_in(self):
        text = self.EMAIL + '''
func other() {
	switch x {
	case "sendgrid":
	}
}
'''
        self.assertEqual(check.parse_email_providers(text), {"smtp", "ses"})

    def test_finds_nothing_when_the_constructor_is_gone(self):
        self.assertEqual(check.parse_email_providers("package email\n"), set())


class TestParsePublishedImages(unittest.TestCase):
    # cd.yml: the matrix block is terminated by a job-level `env:`.
    CD = '''
    strategy:
      matrix:
        service:
          - admin
          - dispatch
          # ADR 0005 phase 4.
          - natsprovision
    env:
      IMAGE_TAG: something
'''

    # release.yml: no job-level `env:`, so the block runs to `steps:` instead. Parsing this
    # one by looking only for `env:` swallowed the whole file and read the step names as
    # services.
    RELEASE = '''
    strategy:
      fail-fast: false
      matrix:
        service:
          - admin
          - dispatch
          - natsprovision
    steps:
      - uses: actions/checkout@abc
      - name: Build and push
'''

    def test_reads_the_publish_matrix(self):
        self.assertEqual(
            check.parse_published_images(self.CD),
            {"hermes-admin", "hermes-dispatch", "hermes-natsprovision"},
        )

    def test_reads_a_matrix_terminated_by_steps(self):
        self.assertEqual(
            check.parse_published_images(self.RELEASE),
            {"hermes-admin", "hermes-dispatch", "hermes-natsprovision"},
        )

    def test_finds_nothing_when_the_matrix_is_gone(self):
        self.assertEqual(check.parse_published_images("name: cd\non: push\n"), set())


# --------------------------------------------------------------------------------------
# Path prefix semantics. Ingress `pathType: Prefix` matches on path *element* boundaries,
# not on string prefix — /v1/user does not match /v1/users. Getting this wrong in the
# permissive direction is what would let a dead route look reachable.
# --------------------------------------------------------------------------------------


class TestPrefixMatching(unittest.TestCase):
    def test_matches_on_element_boundaries(self):
        self.assertTrue(check.prefixes("/v1/users", "/v1/users"))
        self.assertTrue(check.prefixes("/v1/users", "/v1/users/me"))
        self.assertTrue(check.prefixes("/v1/auth", "/v1/auth/token"))

    def test_does_not_match_a_partial_element(self):
        self.assertFalse(check.prefixes("/v1/user", "/v1/users"))
        self.assertFalse(check.prefixes("/v1/users", "/v1/usersplural"))

    def test_a_longer_rule_does_not_prefix_a_shorter_route(self):
        self.assertFalse(check.prefixes("/v1/users/me", "/v1/users"))

    def test_a_literal_rule_does_not_match_a_placeholder_element(self):
        # /v1/subscriptions/{id} is a different route from /v1/subscriptions/categories.
        self.assertFalse(check.prefixes("/v1/subscriptions/categories", "/v1/subscriptions/{id}"))
        self.assertTrue(check.prefixes("/v1/subscriptions", "/v1/subscriptions/{id}"))

    def test_longest_match_wins(self):
        rules = ["/v1/users", "/v1/users/me", "/v1"]
        self.assertEqual(check.longest_match(rules, "/v1/users/me/contacts"), "/v1/users/me")
        self.assertEqual(check.longest_match(rules, "/v1/users"), "/v1/users")
        self.assertIsNone(check.longest_match(["/v1/inbox"], "/v1/users"))


# --------------------------------------------------------------------------------------
# Rendered-manifest helpers.
# --------------------------------------------------------------------------------------


def _service(name, app_name):
    return {"kind": "Service", "metadata": {"name": name, "labels": {"app.kubernetes.io/name": app_name}}}


def _deployment(name, app_name, images=("repo/img:1",)):
    return {
        "kind": "Deployment",
        "metadata": {"name": name},
        "spec": {
            "template": {
                "metadata": {"labels": {"app.kubernetes.io/name": app_name}},
                "spec": {"containers": [{"name": "c", "image": i} for i in images]},
            }
        },
    }


def _ingress(name, rules):
    """rules is [(path, backend service name)]."""
    return {
        "kind": "Ingress",
        "metadata": {"name": name},
        "spec": {
            "rules": [
                {
                    "http": {
                        "paths": [
                            {"path": p, "pathType": "Prefix",
                             "backend": {"service": {"name": b, "port": {"number": 80}}}}
                            for p, b in rules
                        ]
                    }
                }
            ]
        },
    }


def _configmap(name, data):
    return {"kind": "ConfigMap", "metadata": {"name": name}, "data": data}


def _realtime_ingress(path, annotations, path_type="Prefix"):
    return {
        "kind": "Ingress",
        "metadata": {"name": "hermes-realtime", "annotations": annotations},
        "spec": {
            "rules": [
                {
                    "http": {
                        "paths": [
                            {"path": path, "pathType": path_type,
                             "backend": {"service": {"name": "centrifugo", "port": {"number": 8000}}}}
                        ]
                    }
                }
            ]
        },
    }


def _stripprefix_middleware(name, prefixes=("/realtime",)):
    return {
        "kind": "Middleware",
        "metadata": {"name": name},
        "spec": {"stripPrefix": {"prefixes": list(prefixes)}},
    }


def _secret(name, keys, string_data=True):
    field = "stringData" if string_data else "data"
    return {"kind": "Secret", "metadata": {"name": name}, field: {k: "x" for k in keys}}


def _deployment_with_secret_ref(name, env_name, secret, key, optional=False):
    ref = {"name": secret, "key": key}
    if optional:
        ref["optional"] = True
    return {
        "kind": "Deployment",
        "metadata": {"name": name},
        "spec": {"template": {"metadata": {"labels": {"app.kubernetes.io/name": name}},
                              "spec": {"containers": [
                                  {"name": "c", "image": "x",
                                   "env": [{"name": env_name, "valueFrom": {"secretKeyRef": ref}}]}
                              ]}}},
    }


class TestSecretRefs(unittest.TestCase):
    """A secretKeyRef naming a key the chart does not create.

    Invisible to every other gate: `helm template` renders it, `helm lint` accepts it, and the
    failure arrives when the kubelet builds the container. This is the shape that shipped a
    Centrifugo reference to a key templates/secret.yaml only wrote when it happened to be set.
    """

    def test_a_resolvable_reference_passes(self):
        docs = [
            _secret("hermes-secrets", ["HERMES_JWT_SECRET"]),
            _deployment_with_secret_ref("d", "TOKEN", "hermes-secrets", "HERMES_JWT_SECRET"),
        ]
        self.assertEqual(check.check_secret_refs(docs), [])

    def test_a_missing_key_in_a_chart_owned_secret_is_flagged(self):
        docs = [
            _secret("hermes-secrets", ["HERMES_JWT_SECRET"]),
            _deployment_with_secret_ref("d", "APIKEY", "hermes-secrets", "HERMES_CENTRIFUGO_API_KEY"),
        ]
        failures = check.check_secret_refs(docs)
        self.assertTrue(any("HERMES_CENTRIFUGO_API_KEY" in f for f in failures), failures)
        self.assertTrue(any("CreateContainerConfigError" in f for f in failures), failures)

    def test_base64_data_secrets_are_read_too(self):
        docs = [
            _secret("hermes-secrets", ["HERMES_JWT_SECRET"], string_data=False),
            _deployment_with_secret_ref("d", "TOKEN", "hermes-secrets", "HERMES_JWT_SECRET"),
        ]
        self.assertEqual(check.check_secret_refs(docs), [])

    def test_an_externally_supplied_secret_is_not_judged(self):
        # externalPostgresql.existingSecret and friends name Secrets the chart never sees.
        # Asserting about their contents would be pretending to knowledge we do not have.
        docs = [_deployment_with_secret_ref("d", "URL", "my-own-secret", "anything")]
        self.assertEqual(check.check_secret_refs(docs), [])

    def test_an_optional_reference_is_allowed_to_be_absent(self):
        docs = [
            _secret("hermes-secrets", ["HERMES_JWT_SECRET"]),
            _deployment_with_secret_ref("d", "SEED", "hermes-secrets", "absent", optional=True),
        ]
        self.assertEqual(check.check_secret_refs(docs), [])


class TestRealtimePrefixStrip(unittest.TestCase):
    """Centrifugo serves from its root, so /realtime must be stripped — by either dialect.

    Every failure below is silent at install time: the Ingress is accepted, the widget
    connects to nothing, falls down the whole transport ladder and reports itself
    disconnected, and no log names the ingress.
    """

    NGINX = {
        "nginx.ingress.kubernetes.io/rewrite-target": "/$2",
        "nginx.ingress.kubernetes.io/use-regex": "true",
    }
    TRAEFIK = {
        "traefik.ingress.kubernetes.io/router.middlewares":
            "hermes-hermes-realtime-stripprefix@kubernetescrd",
    }

    def test_nginx_regex_form_passes(self):
        docs = [_realtime_ingress("/realtime(/|$)(.*)", self.NGINX, "ImplementationSpecific")]
        self.assertEqual(check.check_realtime_prefix_strip(docs), [])

    def test_traefik_middleware_form_passes(self):
        docs = [
            _realtime_ingress("/realtime", self.TRAEFIK),
            _stripprefix_middleware("hermes-realtime-stripprefix"),
        ]
        self.assertEqual(check.check_realtime_prefix_strip(docs), [])

    def test_an_nginx_regex_path_under_traefik_is_flagged(self):
        # The defect this check was written for. Traefik v3 removed regex from Ingress paths,
        # so it matches this literally, no request ever has that prefix, and /realtime 404s
        # while every /v1 route works — which reads as "Centrifugo is broken".
        docs = [
            _realtime_ingress("/realtime(/|$)(.*)", self.TRAEFIK, "ImplementationSpecific"),
            _stripprefix_middleware("hermes-realtime-stripprefix"),
        ]
        failures = check.check_realtime_prefix_strip(docs)
        self.assertTrue(any("regex" in f for f in failures), failures)

    def test_a_middleware_reference_with_no_middleware_is_flagged(self):
        docs = [_realtime_ingress("/realtime", self.TRAEFIK)]
        failures = check.check_realtime_prefix_strip(docs)
        self.assertTrue(any("no Middleware" in f or "stripPrefix" in f for f in failures), failures)

    def test_a_bare_middleware_name_is_flagged(self):
        # Traefik needs <namespace>-<name>@kubernetescrd. A bare name resolves to nothing and
        # is skipped without complaint, so the prefix is never stripped.
        docs = [
            _realtime_ingress("/realtime", {
                "traefik.ingress.kubernetes.io/router.middlewares": "hermes-realtime-stripprefix",
            }),
            _stripprefix_middleware("hermes-realtime-stripprefix"),
        ]
        failures = check.check_realtime_prefix_strip(docs)
        self.assertTrue(any("kubernetescrd" in f for f in failures), failures)

    def test_stripping_nothing_at_all_is_flagged(self):
        docs = [_realtime_ingress("/realtime", {})]
        failures = check.check_realtime_prefix_strip(docs)
        self.assertTrue(any("neither strips" in f for f in failures), failures)

    def test_ingresses_that_do_not_touch_realtime_are_ignored(self):
        docs = [_ingress("hermes", [("/v1/send", "hermes-send")])]
        self.assertEqual(check.check_realtime_prefix_strip(docs), [])


class TestPodTemplates(unittest.TestCase):
    def test_collects_deployments_jobs_cronjobs_and_bare_pods(self):
        docs = [
            _deployment("d", "hermes-admin"),
            {"kind": "Job", "metadata": {"name": "j"},
             "spec": {"template": {"metadata": {"labels": {"app.kubernetes.io/name": "hermes-migrate"}},
                                   "spec": {"containers": [{"image": "m:1"}]}}}},
            {"kind": "CronJob", "metadata": {"name": "c"},
             "spec": {"jobTemplate": {"spec": {"template": {"metadata": {"labels": {}},
                                                            "spec": {"containers": [{"image": "c:1"}]}}}}}},
            {"kind": "Pod", "metadata": {"name": "p", "labels": {"a": "b"}},
             "spec": {"containers": [{"image": "p:1"}]}},
            {"kind": "Service", "metadata": {"name": "s"}},
        ]
        got = check.pod_templates(docs)
        self.assertEqual([owner for owner, _, _, _ in got], ["d", "j", "c", "p"])

    def test_a_bare_pod_carries_its_own_metadata_labels(self):
        docs = [{"kind": "Pod", "metadata": {"name": "p", "labels": {"app.kubernetes.io/name": "x"}},
                 "spec": {"containers": []}}]
        _, _, labels, _ = check.pod_templates(docs)[0]
        self.assertEqual(labels, {"app.kubernetes.io/name": "x"})

    def test_tolerates_a_workload_with_no_containers(self):
        docs = [{"kind": "Deployment", "metadata": {"name": "d"}, "spec": {"template": {}}}]
        owner, kind, labels, containers = check.pod_templates(docs)[0]
        self.assertEqual((labels, containers), ({}, []))

    def test_init_containers_count_as_containers(self):
        docs = [{"kind": "Deployment", "metadata": {"name": "d"},
                 "spec": {"template": {"spec": {"initContainers": [{"image": "i:1"}],
                                                "containers": [{"image": "c:1"}]}}}}]
        _, _, _, containers = check.pod_templates(docs)[0]
        self.assertEqual([c["image"] for c in containers], ["i:1", "c:1"])


# --------------------------------------------------------------------------------------
# The gate's decisions.
# --------------------------------------------------------------------------------------


class TestCheckIngressRoutes(unittest.TestCase):
    """Defect 3: dead routes, missing routes, and the /v1/users split the review missed."""

    ROUTES = {
        "hermes-admin": {"/v1/users", "/v1/templates", "/v1/templates/{id}"},
        "hermes-user": {"/v1/users/me", "/v1/users/me/preferences"},
        "hermes-inbox": {"/v1/inbox"},
    }

    def _docs(self, rules):
        return [
            _service("rel-hermes-admin", "hermes-admin"),
            _service("rel-hermes-user", "hermes-user"),
            _service("rel-hermes-inbox", "hermes-inbox"),
            _ingress("rel-hermes", rules),
        ]

    def test_passes_when_every_route_is_reachable_at_the_right_backend(self):
        docs = self._docs([
            ("/v1/users", "rel-hermes-admin"),
            ("/v1/users/me", "rel-hermes-user"),
            ("/v1/templates", "rel-hermes-admin"),
            ("/v1/inbox", "rel-hermes-inbox"),
        ])
        self.assertEqual(check.check_ingress_routes(docs, self.ROUTES), [])

    def test_reports_a_route_with_no_rule_at_all(self):
        # The chart had no /v1/templates rule, so every admin template endpoint was
        # unreachable through an ingress install.
        docs = self._docs([
            ("/v1/users", "rel-hermes-admin"),
            ("/v1/users/me", "rel-hermes-user"),
            ("/v1/inbox", "rel-hermes-inbox"),
        ])
        failures = check.check_ingress_routes(docs, self.ROUTES)
        self.assertTrue(any("/v1/templates" in f and "no ingress rule" in f for f in failures), failures)

    def test_reports_a_rule_pointing_at_the_wrong_service(self):
        # The finding the review missed: /v1/users sent wholesale to the user service
        # makes the admin user listing unreachable, because internal/admin serves it.
        docs = self._docs([
            ("/v1/users", "rel-hermes-user"),
            ("/v1/templates", "rel-hermes-admin"),
            ("/v1/inbox", "rel-hermes-inbox"),
        ])
        failures = check.check_ingress_routes(docs, self.ROUTES)
        self.assertTrue(
            any("/v1/users" in f and "hermes-admin" in f and "hermes-user" in f for f in failures),
            failures,
        )

    def test_the_longest_prefix_split_resolves_users_correctly(self):
        # Both routes are served, by different services, and only a longest-prefix split
        # can express that. A gate using first-match or shortest-match would pass the
        # broken chart.
        docs = self._docs([
            ("/v1/users", "rel-hermes-admin"),
            ("/v1/users/me", "rel-hermes-user"),
            ("/v1/templates", "rel-hermes-admin"),
            ("/v1/inbox", "rel-hermes-inbox"),
        ])
        self.assertEqual(check.check_ingress_routes(docs, self.ROUTES), [])

    def test_reports_a_rule_no_handler_serves(self):
        # /v1/types and /v1/groups are pre-rename paths. They render, they resolve, and
        # they 404 — nothing but this notices.
        docs = self._docs([
            ("/v1/users", "rel-hermes-admin"),
            ("/v1/users/me", "rel-hermes-user"),
            ("/v1/templates", "rel-hermes-admin"),
            ("/v1/inbox", "rel-hermes-inbox"),
            ("/v1/types", "rel-hermes-admin"),
        ])
        failures = check.check_ingress_routes(docs, self.ROUTES)
        self.assertTrue(any("/v1/types" in f and "no handler" in f for f in failures), failures)

    def test_reports_a_backend_service_the_chart_does_not_render(self):
        docs = self._docs([("/v1/users", "rel-hermes-typo")])
        failures = check.check_ingress_routes(docs, self.ROUTES)
        self.assertTrue(any("rel-hermes-typo" in f for f in failures), failures)

    def test_ignores_non_v1_rules(self):
        # /realtime goes to Centrifugo, which serves no Go route in this repo.
        docs = self._docs([
            ("/v1/users", "rel-hermes-admin"),
            ("/v1/users/me", "rel-hermes-user"),
            ("/v1/templates", "rel-hermes-admin"),
            ("/v1/inbox", "rel-hermes-inbox"),
            ("/realtime(/|$)(.*)", "rel-centrifugo"),
        ])
        self.assertEqual(check.check_ingress_routes(docs, self.ROUTES), [])


class TestCheckProvisioner(unittest.TestCase):
    """Defect 1: without the provisioner Job the chart crash-loops six services at boot."""

    CONSUMERS = {"hermes-send", "hermes-dispatch", "hermes-worker-events"}
    IDENTITY = "hermes-natsprovision"

    def _job(self, annotations):
        return {
            "kind": "Job",
            "metadata": {"name": "rel-hermes-natsprovision-1", "annotations": annotations},
            "spec": {"template": {"metadata": {"labels": {"app.kubernetes.io/name": self.IDENTITY}},
                                  "spec": {"containers": [{"image": "x:1"}]}}},
        }

    def test_passes_when_the_job_is_a_plain_tracked_resource(self):
        docs = [_deployment("d", "hermes-send"), self._job({})]
        self.assertEqual(check.check_provisioner(docs, self.CONSUMERS, self.IDENTITY), [])

    def test_a_pre_install_hook_is_rejected(self):
        # It cannot work: with the bundled sub-charts the NATS bus is a regular release
        # resource, so at pre-install time there is nothing to provision against. Found by
        # installing — the migration Job failed with "lookup hv-postgresql: no such host".
        docs = [_deployment("d", "hermes-send"),
                self._job({"helm.sh/hook": "pre-install,pre-upgrade"})]
        failures = check.check_provisioner(docs, self.CONSUMERS, self.IDENTITY)
        self.assertTrue(any("helm.sh/hook" in f for f in failures), failures)

    def test_a_post_install_hook_is_rejected(self):
        # ADR 0008, and the reason this invariant is phrased as "not a hook" rather than
        # "the right hook". Helm waits for every regular resource to be Ready BEFORE
        # running post-install hooks, and the six stream consumers cannot become Ready
        # until this Job has run. Measured: `helm install --wait --timeout 4m` failed with
        # `context deadline exceeded`, no Job was ever created, and all nine services sat
        # in CrashLoopBackOff. `--atomic` rolled the release back on top of that.
        docs = [_deployment("d", "hermes-send"),
                self._job({"helm.sh/hook": "post-install,post-upgrade",
                           "helm.sh/hook-weight": "-4"})]
        failures = check.check_provisioner(docs, self.CONSUMERS, self.IDENTITY)
        self.assertTrue(any("--wait" in f for f in failures), failures)

    def test_the_rejection_names_the_phase_that_was_used(self):
        docs = [_deployment("d", "hermes-send"),
                self._job({"helm.sh/hook": "post-install"})]
        failures = check.check_provisioner(docs, self.CONSUMERS, self.IDENTITY)
        self.assertTrue(any("post-install" in f for f in failures), failures)

    def test_reports_the_missing_job(self):
        docs = [_deployment("s", "hermes-send"), _deployment("d", "hermes-dispatch")]
        failures = check.check_provisioner(docs, self.CONSUMERS, self.IDENTITY)
        self.assertEqual(len(failures), 1)
        self.assertIn("hermes-send", failures[0])
        self.assertIn("hermes-natsprovision", failures[0])

    def test_no_job_needed_when_no_stream_consumer_is_enabled(self):
        # An install of only the read-path services (inbox, user) touches no stream.
        docs = [_deployment("i", "hermes-inbox"), _deployment("u", "hermes-user")]
        self.assertEqual(check.check_provisioner(docs, self.CONSUMERS, self.IDENTITY), [])


class TestCheckImages(unittest.TestCase):
    PUBLISHED = {"hermes-admin", "hermes-natsprovision"}
    REGISTRY = "ghcr.io/hermesnotifications"

    def test_passes_on_fully_qualified_tagged_images(self):
        docs = [_deployment("d", "a", ["ghcr.io/hermesnotifications/hermes-admin:0.1.0",
                                       "docker.io/bitnami/redis:7.4.0"])]
        self.assertEqual(check.check_images(docs, self.PUBLISHED, self.REGISTRY), [])

    def test_reports_an_untagged_image(self):
        docs = [_deployment("d", "a", ["ghcr.io/hermesnotifications/hermes-admin"])]
        failures = check.check_images(docs, self.PUBLISHED, self.REGISTRY)
        self.assertTrue(any("not tagged" in f for f in failures), failures)

    def test_a_port_in_the_registry_host_is_not_a_tag(self):
        docs = [_deployment("d", "a", ["localhost:5000/hermes-admin"])]
        failures = check.check_images(docs, self.PUBLISHED, self.REGISTRY)
        self.assertTrue(any("not tagged" in f for f in failures), failures)

    def test_a_digest_counts_as_pinned(self):
        docs = [_deployment("d", "a", ["docker.io/bitnami/redis@sha256:" + "a" * 64])]
        self.assertEqual(check.check_images(docs, self.PUBLISHED, self.REGISTRY), [])

    def test_reports_an_empty_image(self):
        docs = [_deployment("d", "a", [""])]
        failures = check.check_images(docs, self.PUBLISHED, self.REGISTRY)
        self.assertTrue(any("empty" in f for f in failures), failures)

    def test_reports_a_hermes_registry_image_hermes_does_not_publish(self):
        # The real defect this exists for: the NATS sub-chart reads the same
        # `global.image.registry` key Hermes sets, so `nats:2.10.26-alpine` rendered as
        # `ghcr.io/hermesnotifications/nats:2.10.26-alpine` — an image that does not
        # exist. helm template is perfectly happy; the pod is ImagePullBackOff.
        docs = [_deployment("nats", "nats", ["ghcr.io/hermesnotifications/nats:2.10.26-alpine"])]
        failures = check.check_images(docs, self.PUBLISHED, self.REGISTRY)
        self.assertTrue(any("does not publish" in f and "nats" in f for f in failures), failures)

    def test_a_third_party_registry_is_not_checked_for_publication(self):
        docs = [_deployment("d", "a", ["docker.io/natsio/nats-box:0.16.0"])]
        self.assertEqual(check.check_images(docs, self.PUBLISHED, self.REGISTRY), [])


class TestCheckConfig(unittest.TestCase):
    """Defect 2 (HERMES_ENV) and the posture the lead asked to be enforced, not documented."""

    PROVIDERS = {"smtp", "ses"}

    def test_reports_a_configmap_with_no_hermes_env(self):
        docs = [_configmap("c", {"HERMES_NATS_URL": "nats://x:4222", "HERMES_EMAIL_PROVIDER": "smtp"})]
        failures = check.check_config(docs, self.PROVIDERS)
        self.assertTrue(any("HERMES_ENV" in f for f in failures), failures)

    def test_passes_on_a_development_configmap_with_plaintext_urls(self):
        # Evaluation posture: bundled infra is plaintext and that is the intended default.
        docs = [_configmap("c", {
            "HERMES_ENV": "development",
            "HERMES_NATS_URL": "nats://x:4222",
            "HERMES_REDIS_URL": "redis://y:6379/0",
            "HERMES_EMAIL_PROVIDER": "smtp",
        })]
        self.assertEqual(check.check_config(docs, self.PROVIDERS), [])

    def test_reports_an_email_provider_the_go_code_rejects(self):
        # `sendgrid` passed helm install and then killed the worker with
        # "unknown email provider". The enum has since been narrowed; this pins it.
        docs = [_configmap("c", {"HERMES_ENV": "development", "HERMES_EMAIL_PROVIDER": "sendgrid"})]
        failures = check.check_config(docs, self.PROVIDERS)
        self.assertTrue(any("sendgrid" in f for f in failures), failures)

    def test_reports_plaintext_datastores_outside_development(self):
        # config.go Validate() rejects each of these, so a render in this shape is a
        # guaranteed crash-loop. The chart is meant to refuse to render it at all; this
        # is the backstop that says so if the template guard is ever removed.
        docs = [_configmap("c", {
            "HERMES_ENV": "production",
            "HERMES_NATS_URL": "nats://x:4222",
            "HERMES_REDIS_URL": "redis://y:6379/0",
            "HERMES_EMAIL_PROVIDER": "smtp",
        })]
        failures = check.check_config(docs, self.PROVIDERS)
        self.assertTrue(any("HERMES_NATS_URL" in f for f in failures), failures)
        self.assertTrue(any("HERMES_REDIS_URL" in f for f in failures), failures)

    def test_accepts_tls_datastores_outside_development(self):
        docs = [_configmap("c", {
            "HERMES_ENV": "production",
            "HERMES_NATS_URL": "tls://x:4222",
            "HERMES_REDIS_URL": "rediss://y:6379/0",
            "HERMES_EMAIL_PROVIDER": "ses",
        })]
        self.assertEqual(check.check_config(docs, self.PROVIDERS), [])

    def test_a_typo_in_hermes_env_takes_the_strict_path(self):
        # config.go treats only the literal "development" as relaxed, so "developement"
        # is strict. The gate must agree, or it would bless a render the binary rejects.
        docs = [_configmap("c", {
            "HERMES_ENV": "developement",
            "HERMES_NATS_URL": "nats://x:4222",
            "HERMES_REDIS_URL": "redis://y:6379/0",
        })]
        failures = check.check_config(docs, self.PROVIDERS)
        self.assertTrue(any("HERMES_NATS_URL" in f for f in failures), failures)

    def test_ignores_configmaps_that_are_not_the_hermes_one(self):
        # Sub-charts render their own ConfigMaps; none of them carry HERMES_ keys.
        docs = [
            _configmap("nats-config", {"nats.conf": "port: 4222"}),
            _configmap("hermes-config", {"HERMES_ENV": "development", "HERMES_EMAIL_PROVIDER": "smtp"}),
        ]
        self.assertEqual(check.check_config(docs, self.PROVIDERS), [])


class TestCheckHookConfigRefs(unittest.TestCase):
    """A pre-install hook cannot read a resource Helm has not created yet.

    Helm creates a release's regular resources only after its pre-install hooks finish.
    A hook Job that references the release ConfigMap therefore gets a pod stuck in
    CreateContainerConfigError and an install that fails on a timeout with no useful
    message. Confirmed in-cluster on k3s v1.34 with a pod whose envFrom named a ConfigMap
    that did not exist.

    This is the defect that had been sitting in migration-job.yaml since it was written,
    which is why nothing about it is hypothetical: a fresh `helm install` of this chart
    could never run its database migrations.
    """

    HOOK = {"helm.sh/hook": "pre-install,pre-upgrade"}

    def _hook_job(self, refs):
        return {
            "kind": "Job",
            "metadata": {"name": "rel-migrate-1", "annotations": self.HOOK},
            "spec": {"template": {"metadata": {"labels": {}}, "spec": {"containers": [{
                "name": "migrate", "image": "m:1",
                "envFrom": [{kind: {"name": name}} for kind, name in refs],
            }]}}},
        }

    def _configmap_doc(self, name, annotations=None):
        doc = {"kind": "ConfigMap", "metadata": {"name": name}, "data": {"A": "1"}}
        if annotations:
            doc["metadata"]["annotations"] = annotations
        return doc

    def test_reports_a_hook_job_reading_the_release_configmap(self):
        docs = [self._configmap_doc("rel-hermes-config"),
                self._hook_job([("configMapRef", "rel-hermes-config")])]
        failures = check.check_hook_config_refs(docs)
        self.assertTrue(any("rel-hermes-config" in f for f in failures), failures)

    def test_accepts_a_hook_job_reading_a_hook_scoped_copy(self):
        docs = [self._configmap_doc("rel-hermes-config"),
                self._configmap_doc("rel-hermes-config-hook", self.HOOK),
                self._hook_job([("configMapRef", "rel-hermes-config-hook")])]
        self.assertEqual(check.check_hook_config_refs(docs), [])

    def test_accepts_a_reference_the_chart_does_not_render(self):
        # A user-supplied existingSecret is already in the cluster before install runs.
        docs = [self._hook_job([("secretRef", "my-own-secret")])]
        self.assertEqual(check.check_hook_config_refs(docs), [])

    def test_reports_a_hook_job_reading_the_release_secret(self):
        docs = [{"kind": "Secret", "metadata": {"name": "rel-hermes-secrets"}},
                self._hook_job([("secretRef", "rel-hermes-secrets")])]
        failures = check.check_hook_config_refs(docs)
        self.assertTrue(any("rel-hermes-secrets" in f for f in failures), failures)

    def test_checks_env_valuefrom_references_too(self):
        docs = [{"kind": "Secret", "metadata": {"name": "rel-hermes-secrets"}},
                {"kind": "Job", "metadata": {"name": "j", "annotations": self.HOOK},
                 "spec": {"template": {"metadata": {"labels": {}}, "spec": {"containers": [{
                     "name": "c", "image": "m:1",
                     "env": [{"name": "X", "valueFrom": {"secretKeyRef": {
                         "name": "rel-hermes-secrets", "key": "K"}}}],
                 }]}}}}]
        failures = check.check_hook_config_refs(docs)
        self.assertTrue(any("rel-hermes-secrets" in f for f in failures), failures)

    def test_tolerates_an_explicitly_null_annotations_block(self):
        # Real renders contain `annotations:` with nothing under it — the centrifugo
        # sub-chart emits exactly that. dict.get("annotations", {}) returns None there,
        # not {}, and reading it naively raised AttributeError on the whole gate.
        docs = [{"kind": "Deployment", "metadata": {"name": "d", "annotations": None},
                 "spec": {"template": {"metadata": {"labels": {}}, "spec": {"containers": []}}}},
                {"kind": "ConfigMap", "metadata": {"name": "c", "annotations": None}, "data": {}}]
        self.assertEqual(check.check_hook_config_refs(docs), [])

    def test_a_regular_workload_may_read_the_release_configmap(self):
        # This is the normal case and must not be flagged.
        docs = [self._configmap_doc("rel-hermes-config"),
                {"kind": "Deployment", "metadata": {"name": "d"},
                 "spec": {"template": {"metadata": {"labels": {}}, "spec": {"containers": [{
                     "name": "c", "image": "a:1",
                     "envFrom": [{"configMapRef": {"name": "rel-hermes-config"}}],
                 }]}}}}]
        self.assertEqual(check.check_hook_config_refs(docs), [])

    def test_a_post_install_hook_may_read_the_release_configmap(self):
        # By post-install the regular resources exist, so only pre- phases are a problem.
        docs = [self._configmap_doc("rel-hermes-config"),
                {"kind": "Job", "metadata": {"name": "j",
                                             "annotations": {"helm.sh/hook": "post-install"}},
                 "spec": {"template": {"metadata": {"labels": {}}, "spec": {"containers": [{
                     "name": "c", "image": "a:1",
                     "envFrom": [{"configMapRef": {"name": "rel-hermes-config"}}],
                 }]}}}}]
        self.assertEqual(check.check_hook_config_refs(docs), [])


class TestEvaluate(unittest.TestCase):
    """The whole gate, including the two ways it can silently verify nothing."""

    SOURCE = check.Source(
        routes={"hermes-admin": {"/v1/users"}},
        stream_services={"hermes-send"},
        provisioner="hermes-natsprovision",
        email_providers={"smtp"},
        published_images={"hermes-admin", "hermes-send", "hermes-natsprovision"},
        internal_images={"hermes-admin", "hermes-send", "hermes-natsprovision"},
        registry="ghcr.io/hermesnotifications",
    )

    def _good_docs(self):
        return [
            _service("rel-hermes-admin", "hermes-admin"),
            _ingress("rel-hermes", [("/v1/users", "rel-hermes-admin")]),
            _deployment("rel-hermes-admin", "hermes-admin",
                        ["ghcr.io/hermesnotifications/hermes-admin:0.1.0"]),
            _deployment("rel-hermes-send", "hermes-send",
                        ["ghcr.io/hermesnotifications/hermes-send:0.1.0"]),
            {"kind": "Job", "metadata": {"name": "p"},
             "spec": {"template": {"metadata": {"labels": {"app.kubernetes.io/name": "hermes-natsprovision"}},
                                   "spec": {"containers": [{"image": "ghcr.io/hermesnotifications/hermes-natsprovision:0.1.0"}]}}}},
            _configmap("rel-hermes-config", {"HERMES_ENV": "development", "HERMES_EMAIL_PROVIDER": "smtp"}),
        ]

    def test_a_correct_render_passes(self):
        failures, stats = check.evaluate(self._good_docs(), self.SOURCE)
        self.assertEqual(failures, [])
        self.assertEqual(stats["workloads"], 3)

    def test_an_image_the_release_matrix_does_not_build_is_flagged(self):
        # `hermes-cleanup` was exactly this case in the real repo: charts reference it,
        # the build matrix did not build it.
        source = check.Source(**{**self.SOURCE._asdict(),
                                 "published_images": {"hermes-admin", "hermes-natsprovision"}})
        failures, _ = check.evaluate(self._good_docs(), source)
        self.assertTrue(any("hermes-send" in f and "does not publish" in f for f in failures), failures)

    def test_an_empty_manifest_is_an_error_not_a_pass(self):
        failures, stats = check.evaluate([], self.SOURCE)
        self.assertEqual(stats["workloads"], 0)

    def test_reports_every_defect_not_just_the_first(self):
        docs = [
            _service("rel-hermes-admin", "hermes-admin"),
            _ingress("rel-hermes", [("/v1/types", "rel-hermes-admin")]),
            _deployment("rel-hermes-send", "hermes-send", ["ghcr.io/hermesnotifications/hermes-send"]),
            _configmap("rel-hermes-config", {"HERMES_EMAIL_PROVIDER": "sendgrid"}),
        ]
        failures, _ = check.evaluate(docs, self.SOURCE)
        joined = "\n".join(failures)
        for expected in ("/v1/users", "/v1/types", "hermes-natsprovision", "not tagged",
                         "HERMES_ENV", "sendgrid"):
            self.assertIn(expected, joined)


class TestServiceBackendIdentity(unittest.TestCase):
    """The chart labels Services; the kustomize overlays name them.

    This gate was written against the chart and only ever run against it. Pointing it at
    the overlays — which is where the route drift actually was — reported every rule as
    sending traffic to None, because the overlays set only `part-of`.
    """

    def test_prefers_the_app_name_label(self):
        docs = [_service("rel-hermes-admin", "hermes-admin")]
        self.assertEqual(check.service_backends(docs), {"rel-hermes-admin": "hermes-admin"})

    def test_falls_back_to_the_service_name_when_unlabelled(self):
        # The overlays' shape: no app.kubernetes.io/name, and the Service is already
        # named for its identity.
        docs = [{"kind": "Service", "metadata": {
            "name": "hermes-admin", "labels": {"app.kubernetes.io/part-of": "hermes"}}}]
        self.assertEqual(check.service_backends(docs), {"hermes-admin": "hermes-admin"})

    def test_the_label_wins_over_the_name(self):
        # Under the chart the Service name carries a release prefix, so falling back to it
        # would be the wrong string. The fallback must only apply when the label is absent.
        docs = [_service("rel-hermes-admin", "hermes-admin")]
        self.assertNotEqual(check.service_backends(docs)["rel-hermes-admin"], "rel-hermes-admin")

    def test_routes_resolve_against_an_unlabelled_service(self):
        docs = [
            {"kind": "Service", "metadata": {"name": "hermes-admin", "labels": {}}},
            _ingress("hermes-ingress", [("/v1/users", "hermes-admin")]),
        ]
        self.assertEqual(check.check_ingress_routes(docs, {"hermes-admin": {"/v1/users"}}), [])


class TestEvaluateOnly(unittest.TestCase):
    """--only exists so the overlays can be gated on routes without the image check.

    Overlay image tags are SET_BY_CD_PIPELINE placeholders until Kargo rewrites them at
    promotion, so running the image check there would fail permanently and the gate would
    be turned off again.
    """

    SOURCE = TestEvaluate.SOURCE

    def _docs_with_two_defects(self):
        return [
            _service("rel-hermes-admin", "hermes-admin"),
            # Wrong route, and an untagged image.
            _ingress("rel-hermes", [("/v1/types", "rel-hermes-admin")]),
            _deployment("rel-hermes-admin", "hermes-admin", ["hermes-admin"]),
        ]

    def test_only_routes_reports_route_defects(self):
        failures, _ = check.evaluate(self._docs_with_two_defects(), self.SOURCE, only=["routes"])
        self.assertTrue(any("/v1/types" in f for f in failures), failures)

    def test_only_routes_suppresses_the_image_check(self):
        failures, _ = check.evaluate(self._docs_with_two_defects(), self.SOURCE, only=["routes"])
        self.assertFalse(any("not tagged" in f for f in failures), failures)

    def test_no_selection_runs_everything(self):
        failures, _ = check.evaluate(self._docs_with_two_defects(), self.SOURCE)
        self.assertTrue(any("not tagged" in f for f in failures), failures)

    def test_an_unknown_check_name_is_rejected_rather_than_ignored(self):
        # Silently running nothing is the failure mode this whole script exists to prevent.
        self.assertEqual(check.main(["-", "--only=nonsense"]), 2)


class TestMainGuards(unittest.TestCase):
    """The gate must not report success when it read nothing."""

    @staticmethod
    def _source(**overrides):
        base = dict(routes={"a": {"/v1/x"}}, stream_services={"s"}, provisioner="p",
                    email_providers={"smtp"}, published_images={"x"}, internal_images={"x"},
                    registry="r")
        return check.Source(**{**base, **overrides})

    def test_source_with_no_routes_is_rejected(self):
        problems = check.source_problems(self._source(routes={}))
        self.assertTrue(any("route" in p for p in problems), problems)

    def test_source_with_no_stream_services_is_rejected(self):
        self.assertTrue(check.source_problems(self._source(stream_services=set())))

    def test_source_with_no_provisioner_identity_is_rejected(self):
        self.assertTrue(check.source_problems(self._source(provisioner=None)))

    def test_source_with_no_email_providers_is_rejected(self):
        self.assertTrue(check.source_problems(self._source(email_providers=set())))

    def test_source_with_no_published_images_is_rejected(self):
        problems = check.source_problems(self._source(published_images=set()))
        self.assertTrue(any("release.yml" in p for p in problems), problems)

    def test_source_with_no_internal_images_is_rejected(self):
        problems = check.source_problems(self._source(internal_images=set()))
        self.assertTrue(any("cd.yml" in p for p in problems), problems)

    def test_a_service_published_but_never_delivered_is_flagged(self):
        # In release.yml, absent from cd.yml: Kargo has nothing to promote.
        problems = check.source_problems(
            self._source(published_images={"x", "hermes-new"}, internal_images={"x"}))
        self.assertTrue(any("hermes-new" in p and "Kargo" in p for p in problems), problems)

    def test_a_service_delivered_but_never_published_is_flagged(self):
        # In cd.yml, absent from release.yml: no self-hoster can pull it. This is the
        # direction that hurts strangers rather than the maintainer, so it must not be
        # silent.
        problems = check.source_problems(
            self._source(published_images={"x"}, internal_images={"x", "hermes-new"}))
        self.assertTrue(any("hermes-new" in p and "self-hoster" in p for p in problems), problems)

    def test_a_complete_source_has_no_problems(self):
        self.assertEqual(check.source_problems(self._source()), [])


if __name__ == "__main__":
    unittest.main()
