import json
import os
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from headlessmolstar import HeadlessMolstar, MolstarError, demo_job, image_output


class HeadlessMolstarPythonWrapperTest(unittest.TestCase):
    def test_version_uses_json_and_skip_runtime(self):
        with fake_cli() as command:
            client = HeadlessMolstar(command)
            report = client.version()
        self.assertTrue(report["ok"])
        self.assertEqual(report["command"], "version")
        self.assertIn("--skip-runtime", report["args"])

    def test_render_mapping_writes_temp_job_and_parses_report(self):
        with fake_cli() as command:
            client = HeadlessMolstar(command)
            report = client.render(demo_job("demo.png"), dry_run=True, renderer_mode="auto")
        self.assertTrue(report["ok"])
        self.assertEqual(report["command"], "render")
        self.assertIn("--dry-run", report["args"])
        self.assertIn("--renderer-mode", report["args"])
        job_path = Path(report["input"])
        self.assertEqual(job_path.name, "job.json")
        self.assertFalse(job_path.exists(), "temporary job directory should be cleaned up")

    def test_batch_writes_jsonl(self):
        with fake_cli() as command:
            client = HeadlessMolstar(command)
            report = client.batch([demo_job("a.png"), demo_job("b.png")], concurrency=2)
        self.assertTrue(report["ok"])
        self.assertEqual(report["command"], "batch")
        self.assertIn("--concurrency", report["args"])

    def test_nonzero_raises_structured_error(self):
        with fake_cli(exit_code=12) as command:
            client = HeadlessMolstar(command)
            with self.assertRaises(MolstarError) as raised:
                client.version()
        self.assertEqual(raised.exception.result.returncode, 12)
        self.assertEqual(raised.exception.report["error"]["code"], "fake_failed")

    def test_helpers_build_job_shape(self):
        output = image_output("out.png", (64, 32), transparent=True)
        self.assertEqual(output["size"], [64, 32])
        self.assertTrue(output["transparent"])
        job = demo_job("out.png", size=(64, 32))
        self.assertEqual(job["version"], 1)
        self.assertEqual(job["outputs"][0]["size"], [64, 32])


class fake_cli:
    def __init__(self, exit_code=0):
        self.exit_code = exit_code
        self.tmp = tempfile.TemporaryDirectory()

    def __enter__(self):
        path = Path(self.tmp.name) / "fake_molstar.py"
        path.write_text(
            textwrap.dedent(
                f"""
                import json
                import sys

                args = sys.argv[1:]
                command = args[0] if args else ""
                report = {{
                    "ok": {str(self.exit_code == 0)},
                    "command": command,
                    "args": args,
                }}
                if command == "render" and "--demo" not in args:
                    report["input"] = args[1]
                if {self.exit_code} != 0:
                    report["error"] = {{"code": "fake_failed", "message": "fake failure"}}
                print(json.dumps(report))
                sys.exit({self.exit_code})
                """
            ),
            encoding="utf-8",
        )
        return [sys.executable, str(path)]

    def __exit__(self, exc_type, exc, tb):
        self.tmp.cleanup()


if __name__ == "__main__":
    unittest.main()
