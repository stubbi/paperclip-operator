#!/usr/bin/env python3
"""Exercise the real Make target and workflow steps with isolated CLI fixtures."""

import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = '''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
tool = Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ["SIGNING_TEST_LOG"], "a") as out:
    out.write(json.dumps([tool, *args]) + "\\n")
failure = os.environ.get("SIGNING_TEST_FAIL")
if tool == "cosign":
    raise SystemExit(1 if failure == args[0] else 0)
if args[:2] == ["release", "view"]:
    if failure == "release":
        raise SystemExit(1)
    print(os.environ.get("SIGNING_TEST_TAG", "v0.19.1"))
elif args[:1] == ["api"]:
    if failure == "chart":
        raise SystemExit(1)
    print(os.environ.get("SIGNING_TEST_CHART", "image:\\n  repository: ghcr.io/paperclipinc/paperclip-operator\\n"))
elif args[:2] == ["issue", "list"]:
    if failure == "issue-list":
        raise SystemExit(1)
    print("42" if os.environ.get("SIGNING_TEST_EXISTING") == "1" else "")
elif args[:2] == ["issue", "create"]:
    if "--label" in args and args[args.index("--label") + 1] != "bug":
        raise SystemExit("unknown repository label")
    print("https://example.invalid/issues/42")
else:
    raise SystemExit("unexpected gh fixture call: " + repr(args))
'''


class VerifySigningTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name)
        self.log = self.path / "calls.jsonl"
        self.env = os.environ.copy()
        self.env.update(
            PATH=str(self.path) + os.pathsep + self.env["PATH"],
            SIGNING_TEST_LOG=str(self.log),
            GH_TOKEN="synthetic-test-token",
            GITHUB_OUTPUT=str(self.path / "output"),
            GITHUB_REPOSITORY="stubbi/paperclip-operator",
            GITHUB_RUN_ID="123",
            GITHUB_SERVER_URL="https://github.com",
            KUBECONFIG="/dev/null",
            DOCKER_CONFIG=str(self.path / "docker"),
        )
        for tool in ("gh", "cosign"):
            file = self.path / tool
            file.write_text(FIXTURE)
            file.chmod(0o700)

    def calls(self, tool):
        return [x[1:] for line in self.log.read_text().splitlines()
                if (x := json.loads(line))[0] == tool] if self.log.exists() else []

    def workflow(self):
        return yaml.safe_load((ROOT / ".github/workflows/verify-signing.yaml").read_text())

    def target(self, tag="v0.19.1", repository="ghcr.io/paperclipinc/paperclip-operator"):
        self.env["SIGNING_TEST_TAG"] = tag
        self.env["SIGNING_TEST_CHART"] = f"image:\n  repository: {repository}\n"
        return subprocess.run(
            ["make", "verify-signing"],
            cwd=ROOT, env=self.env, text=True, capture_output=True, timeout=30,
        )

    def workflow_step(self, name):
        step = next(x for x in self.workflow()["jobs"]["verify"]["steps"] if x.get("name") == name)
        script = step["run"].replace("${{ github.repository }}", "stubbi/paperclip-operator")
        script = script.replace("${{ github.run_id }}", "123")
        return subprocess.run(
            ["bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", script],
            cwd=ROOT, env=self.env, text=True, capture_output=True, timeout=30,
        )

    def assert_target(self, tag, repository):
        result = self.target(tag, repository)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("ref=" + tag, self.calls("gh")[1])
        expected = repository + ":" + tag
        calls = self.calls("cosign")
        self.assertEqual([x[:2] for x in calls], [["verify", expected], ["verify-attestation", expected]])
        self.assertEqual(calls[1][calls[1].index("--type") + 1], "spdxjson")
        for call in calls:
            self.assertEqual(call[call.index("--certificate-oidc-issuer") + 1],
                             "https://token.actions.githubusercontent.com")
            policy = call[call.index("--certificate-identity-regexp") + 1]
            for owner in ("paperclipinc", "stubbi"):
                self.assertRegex(f"https://github.com/{owner}/paperclip-operator/.github/workflows/release.yaml@refs/tags/{tag}", policy)
            self.assertIsNone(re.search(policy, "https://github.com/attacker/paperclip-operator/.github/workflows/release.yaml@refs/tags/" + tag))
            self.assertFalse(any(x.startswith("--insecure-ignore") for x in call))

    def test_latest_legacy_release_uses_its_tagged_chart_repository(self):
        self.assert_target("v0.19.1", "ghcr.io/paperclipinc/paperclip-operator")

    def test_new_release_uses_its_tagged_chart_repository(self):
        self.assert_target("v0.19.2", "ghcr.io/stubbi/paperclip-operator")

    def test_explicit_custom_chart_namespace_is_preserved(self):
        self.assert_target("v1.0.0", "ghcr.io/custom-mirror/paperclip-operator")

    def test_release_lookup_failure_stops_before_chart_lookup(self):
        self.env["SIGNING_TEST_FAIL"] = "release"
        self.assertNotEqual(self.target().returncode, 0)
        self.assertEqual(len(self.calls("gh")), 1)
        self.assertEqual(self.calls("cosign"), [])

    def test_empty_release_tag_stops_before_chart_lookup(self):
        self.assertNotEqual(self.target(tag="").returncode, 0)
        self.assertEqual(len(self.calls("gh")), 1)
        self.assertEqual(self.calls("cosign"), [])

    def test_chart_lookup_failure_stops_before_verification(self):
        self.env["SIGNING_TEST_FAIL"] = "chart"
        self.assertNotEqual(self.target().returncode, 0)
        self.assertEqual(self.calls("cosign"), [])

    def test_invalid_chart_repository_stops_before_verification(self):
        for repository in ("", "example.com/unexpected/image", "ghcr.io/stubbi/other-operator"):
            with self.subTest(repository=repository):
                self.assertNotEqual(self.target(repository=repository).returncode, 0)
                self.assertEqual(self.calls("cosign"), [])

    def test_bad_signature_fails_before_attestation_without_fallback(self):
        self.env["SIGNING_TEST_FAIL"] = "verify"
        self.assertNotEqual(self.target().returncode, 0)
        self.assertEqual([x[:2] for x in self.calls("cosign")],
                         [["verify", "ghcr.io/paperclipinc/paperclip-operator:v0.19.1"]])

    def test_bad_attestation_fails_without_fallback(self):
        self.env["SIGNING_TEST_FAIL"] = "verify-attestation"
        self.assertNotEqual(self.target().returncode, 0)
        self.assertEqual([x[:2] for x in self.calls("cosign")],
                         [["verify", "ghcr.io/paperclipinc/paperclip-operator:v0.19.1"],
                          ["verify-attestation", "ghcr.io/paperclipinc/paperclip-operator:v0.19.1"]])

    def test_workflow_preserves_verification_and_metadata_failures(self):
        for failure in ("verify", "verify-attestation", "release", "chart"):
            with self.subTest(failure=failure):
                self.env["SIGNING_TEST_FAIL"] = failure
                result = self.workflow_step("Verify latest release")
                self.assertNotEqual(result.returncode, 0, "verification failure became a successful workflow step")

    def test_workflow_preserves_an_empty_release_tag_failure(self):
        self.env["SIGNING_TEST_TAG"] = ""
        self.assertNotEqual(self.workflow_step("Verify latest release").returncode, 0)
        self.assertEqual(self.calls("cosign"), [])

    def test_workflow_success_remains_successful(self):
        result = self.workflow_step("Verify latest release")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_failure_reporting_uses_an_existing_label_and_neutral_diagnosis(self):
        result = self.workflow_step("Open issue on failure")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        creates = [x for x in self.calls("gh") if x[:2] == ["issue", "create"]]
        self.assertEqual(len(creates), 1)
        self.assertEqual(creates[0][creates[0].index("--label") + 1], "bug")
        self.assertIn("--repo", creates[0])
        body = creates[0][creates[0].index("--body") + 1]
        self.assertIn("https://github.com/stubbi/paperclip-operator/actions/runs/123", body)
        self.assertNotIn("no longer", body)

    def test_failure_reporting_reuses_an_open_tracker(self):
        self.env["SIGNING_TEST_EXISTING"] = "1"
        result = self.workflow_step("Open issue on failure")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(any(x[:2] == ["issue", "create"] for x in self.calls("gh")))
        lookup = self.calls("gh")[0]
        self.assertEqual(lookup[lookup.index("--state") + 1], "open")
        self.assertIn("select(.title ==", lookup[lookup.index("--jq") + 1])

    def test_tracker_lookup_failure_does_not_create_a_duplicate(self):
        self.env["SIGNING_TEST_FAIL"] = "issue-list"
        self.assertNotEqual(self.workflow_step("Open issue on failure").returncode, 0)
        self.assertFalse(any(x[:2] == ["issue", "create"] for x in self.calls("gh")))

    def test_live_verification_and_issue_writes_are_excluded_from_pull_requests(self):
        workflow = self.workflow()
        # PyYAML's YAML 1.1 loader treats the workflow's `on` key as True.
        triggers = workflow.get("on", workflow.get(True))
        self.assertEqual(set(triggers), {"pull_request", "schedule", "workflow_dispatch"})
        self.assertEqual(triggers["schedule"], [{"cron": "0 13 * * 1"}])
        self.assertIn("hack/test-verify-signing.py", triggers["pull_request"]["paths"])
        jobs = workflow["jobs"]
        self.assertEqual(jobs["verify"].get("if"), "github.event_name != 'pull_request'")
        self.assertEqual(jobs["verify"].get("needs"), "regression")
        self.assertEqual(jobs.get("regression", {}).get("permissions"), {"contents": "read"})
        reporting = next(x for x in jobs["verify"]["steps"] if x.get("name") == "Open issue on failure")
        self.assertEqual(reporting["if"], "failure() && steps.verify.outcome == 'failure'")


if __name__ == "__main__":
    unittest.main(verbosity=2)
