"""Execute the release workflow's registry selection without publishing anything."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[1]
DIGEST = "sha256:" + "a" * 64


class RegistrySelectionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        shutil.copytree(ROOT / "bundle", self.root / "bundle")
        chart = self.root / "charts/paperclip-operator"
        chart.mkdir(parents=True)
        self.values = chart / "values.yaml"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.requests = self.root / "requests"
        self.env = {
            **os.environ,
            "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
            "REQUESTS": str(self.requests),
            "DOCKER_DIGEST": DIGEST,
            "DOCKER_STATUS": "0",
            "GITHUB_OUTPUT": str(self.root / "outputs"),
            "KUBECONFIG": "/dev/null",
        }
        self.command("docker", '''printf "%s\\n" "$*" >> "$REQUESTS"
if [ "${DOCKER_MANIFEST+x}" = x ]; then
  printf '%s\\n' "$DOCKER_MANIFEST"
else
  case "$*" in
    *'json .Manifest'*) python3 -c 'import json, os; print(json.dumps({"digest": os.environ["DOCKER_DIGEST"]}))' ;;
    *) printf 'Name: fixture\\nDigest: %s\\n' "$DOCKER_DIGEST" ;;
  esac
fi
exit "$DOCKER_STATUS"
''')

    def command(self, name, body):
        path = self.bin / name
        path.write_text("#!/bin/sh\nset -eu\n" + body)
        path.chmod(0o755)

    def prepare(self, job, tag, repository):
        shutil.rmtree(self.root / "submission", ignore_errors=True)
        (self.root / "outputs").unlink(missing_ok=True)
        self.requests.unlink(missing_ok=True)
        self.values.write_text(f"image:\n  repository: {repository}\n")
        workflow = yaml.safe_load((ROOT / ".github/workflows/operatorhub.yaml").read_text())
        step = next(s for s in workflow["jobs"][job]["steps"] if s.get("name") == "Prepare bundle")
        script = step["run"].replace("${{ steps.version.outputs.version }}", tag.removeprefix("v"))
        script = script.replace("${{ steps.version.outputs.tag }}", tag)
        return subprocess.run(["bash", "-euo", "pipefail", "-c", script], cwd=self.root,
                              env=self.env, capture_output=True, text=True)

    def test_each_catalog_uses_the_tagged_chart_repository(self):
        for job in ["submit", "submit-redhat"]:
            for tag, namespace in [("v0.19.1", "paperclipinc"), ("v0.19.2", "stubbi"), ("v1.0.0", "future-org")]:
                with self.subTest(job=job, tag=tag):
                    repository = f"ghcr.io/{namespace}/paperclip-operator"
                    result = self.prepare(job, tag, repository)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn(f"inspect {repository}:{tag} ", self.requests.read_text())
                    csv = next((self.root / f"submission/operators/paperclip-operator/{tag[1:]}/manifests").glob("*.clusterserviceversion.yaml"))
                    bundle = yaml.safe_load(csv.read_text())
                    expected = f"{repository}@{DIGEST}"
                    self.assertEqual(bundle["metadata"]["annotations"]["containerImage"], expected)
                    self.assertEqual(bundle["spec"]["install"]["spec"]["deployments"][0]["spec"]["template"]["spec"]["containers"][0]["image"], expected)
                    self.assertEqual(bundle["spec"]["relatedImages"][0]["image"], expected)

    def test_each_catalog_rejects_missing_or_malformed_digests(self):
        for job in ["submit", "submit-redhat"]:
            for digest, status in [("", "1"), (DIGEST, "1"), ("", "0"), ("null", "0"), ("sha256:abc", "0"), ("<html>504</html>", "0"), (DIGEST + "\nextra=garbage", "0"), (DIGEST + "\n" + DIGEST, "0")]:
                with self.subTest(job=job, digest=digest, status=status):
                    self.env.update(DOCKER_DIGEST=digest, DOCKER_STATUS=status)
                    result = self.prepare(job, "v0.19.1", "ghcr.io/paperclipinc/paperclip-operator")
                    self.assertNotEqual(result.returncode, 0, result.stdout)
                    self.assertFalse((self.root / "submission").exists())
                    self.assertFalse((self.root / "outputs").exists())

    def test_each_catalog_rejects_invalid_manifest_json(self):
        for job in ["submit", "submit-redhat"]:
            for payload in ["", "Name: fixture\nDigest: " + DIGEST, "{broken", "{}", '{"digest":null}']:
                with self.subTest(job=job, payload=payload):
                    self.env["DOCKER_MANIFEST"] = payload
                    result = self.prepare(job, "v0.19.1", "ghcr.io/paperclipinc/paperclip-operator")
                    self.assertNotEqual(result.returncode, 0, result.stdout)
                    self.assertFalse((self.root / "submission").exists())
                    self.assertFalse((self.root / "outputs").exists())

    def test_chart_defaults_keep_legacy_versions_in_the_published_namespace(self):
        chart = yaml.safe_load((ROOT / "charts/paperclip-operator/Chart.yaml").read_text())
        values = yaml.safe_load((ROOT / "charts/paperclip-operator/values.yaml").read_text())
        version = tuple(int(part) for part in chart["appVersion"].split("."))
        repository = values["image"]["repository"]
        if version < (0, 19, 2):
            self.assertEqual(repository, "ghcr.io/paperclipinc/paperclip-operator",
                             "legacy release tags were not published in stubbi")
        images = yaml.safe_load(chart["annotations"]["artifacthub.io/images"])
        self.assertEqual(images[0]["image"], repository + ":v" + chart["appVersion"])

    def test_each_catalog_rejects_an_invalid_chart_repository(self):
        for job in ["submit", "submit-redhat"]:
            with self.subTest(job=job):
                result = self.prepare(job, "v0.19.1", "example.com/unexpected/image")
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertFalse(self.requests.exists())

    def test_verify_signing_uses_the_latest_releases_tagged_repository(self):
        self.command("gh", 'case "$1" in\nrelease) echo v0.19.1 ;;\napi) printf "image:\\n  repository: ghcr.io/paperclipinc/paperclip-operator\\n" ;;\nesac\n')
        self.command("cosign", 'printf "%s\\n" "$*" >> "$REQUESTS"\n')
        result = subprocess.run(["make", "-s", "-f", str(ROOT / "Makefile"), "verify-signing"],
                                cwd=self.root, env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        requests = self.requests.read_text().splitlines()
        self.assertEqual(len(requests), 2)
        self.assertTrue(all("ghcr.io/paperclipinc/paperclip-operator:v0.19.1 " in line for line in requests))


if __name__ == "__main__":
    unittest.main()
