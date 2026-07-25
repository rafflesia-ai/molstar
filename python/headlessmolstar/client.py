from __future__ import annotations

import json
import os
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping, MutableMapping, Sequence


JSON = MutableMapping[str, Any]


@dataclass(frozen=True)
class CLIResult:
    args: list[str]
    report: JSON
    stdout: str
    stderr: str
    returncode: int


class MolstarError(RuntimeError):
    def __init__(self, message: str, *, result: CLIResult | None = None):
        super().__init__(message)
        self.result = result

    @property
    def report(self) -> JSON | None:
        if self.result is None:
            return None
        return self.result.report


class HeadlessMolstar:
    def __init__(
        self,
        binary: str | os.PathLike[str] | Sequence[str | os.PathLike[str]] = "molstar",
        *,
        cwd: str | os.PathLike[str] | None = None,
        env: Mapping[str, str] | None = None,
        timeout: float | None = None,
    ) -> None:
        if isinstance(binary, (str, os.PathLike)):
            self.command = [str(binary)]
        else:
            self.command = [str(part) for part in binary]
            if not self.command:
                raise ValueError("binary command cannot be empty")
        self.binary = self.command[0]
        self.cwd = Path(cwd) if cwd is not None else None
        self.env = dict(env) if env is not None else None
        self.timeout = timeout

    def run(
        self,
        args: Sequence[str | os.PathLike[str]],
        *,
        input_text: str | None = None,
        expect_ok: bool = True,
    ) -> CLIResult:
        command = [*self.command, *[str(arg) for arg in args]]
        completed = subprocess.run(
            command,
            input=input_text,
            text=True,
            capture_output=True,
            cwd=self.cwd,
            env=self._env(),
            timeout=self.timeout,
            check=False,
        )
        report = self._parse_json_report(command, completed.stdout, completed.stderr, completed.returncode)
        result = CLIResult(
            args=command,
            report=report,
            stdout=completed.stdout,
            stderr=completed.stderr,
            returncode=completed.returncode,
        )
        if completed.returncode != 0:
            raise MolstarError(_error_message(report, completed.stderr), result=result)
        if expect_ok and report.get("ok") is False:
            raise MolstarError(_error_message(report, completed.stderr), result=result)
        return result

    def version(self, *, runtime: bool = False) -> JSON:
        args = ["version", "--json"]
        if not runtime:
            args.append("--skip-runtime")
        return self.run(args).report

    def render(
        self,
        input_or_job: str | os.PathLike[str] | Mapping[str, Any] | None = None,
        *,
        out: str | os.PathLike[str] | None = None,
        size: tuple[int, int] | None = None,
        demo: bool = False,
        dry_run: bool = False,
        renderer_mode: str | None = None,
        extra_args: Sequence[str | os.PathLike[str]] = (),
    ) -> JSON:
        with tempfile.TemporaryDirectory(prefix="headlessmolstar-python-") as tmp:
            args = ["render"]
            if demo:
                args.append("--demo")
            else:
                if input_or_job is None:
                    raise ValueError("input_or_job is required unless demo=True")
                args.append(self._input_arg(input_or_job, Path(tmp)))
            if out is not None:
                args.extend(["--out", str(out)])
            if size is not None:
                args.extend(["--size", f"{size[0]}x{size[1]}"])
            if renderer_mode is not None:
                args.extend(["--renderer-mode", renderer_mode])
            if dry_run:
                args.append("--dry-run")
            args.extend([str(arg) for arg in extra_args])
            args.append("--json")
            return self.run(args).report

    def batch(
        self,
        jobs: Sequence[Mapping[str, Any]] | str | os.PathLike[str],
        *,
        concurrency: int | None = None,
        continue_on_error: bool = False,
        extra_args: Sequence[str | os.PathLike[str]] = (),
    ) -> JSON:
        with tempfile.TemporaryDirectory(prefix="headlessmolstar-python-") as tmp:
            if isinstance(jobs, (str, os.PathLike)):
                input_path = str(jobs)
            else:
                input_path = str(Path(tmp) / "jobs.jsonl")
                with open(input_path, "w", encoding="utf-8") as handle:
                    for job in jobs:
                        handle.write(json.dumps(job))
                        handle.write("\n")
            args = ["batch", input_path]
            if concurrency is not None:
                args.extend(["--concurrency", str(concurrency)])
            if continue_on_error:
                args.append("--continue-on-error")
            args.extend([str(arg) for arg in extra_args])
            args.append("--json")
            return self.run(args).report

    def validate_job(self, job: Mapping[str, Any] | str | os.PathLike[str], *, schema: bool = True) -> JSON:
        with tempfile.TemporaryDirectory(prefix="headlessmolstar-python-") as tmp:
            args = ["job", "validate", self._input_arg(job, Path(tmp)), "--json"]
            if schema:
                args.append("--schema")
            return self.run(args).report

    def compile_scene(
        self,
        job: Mapping[str, Any] | str | os.PathLike[str],
        out: str | os.PathLike[str],
        *,
        extra_args: Sequence[str | os.PathLike[str]] = (),
    ) -> JSON:
        with tempfile.TemporaryDirectory(prefix="headlessmolstar-python-") as tmp:
            args = ["scene", "compile", self._input_arg(job, Path(tmp)), "--out", str(out), "--json"]
            args.extend([str(arg) for arg in extra_args])
            return self.run(args).report

    def _input_arg(self, value: str | os.PathLike[str] | Mapping[str, Any], tmp: Path) -> str:
        if isinstance(value, Mapping):
            path = tmp / "job.json"
            path.write_text(json.dumps(value), encoding="utf-8")
            return str(path)
        return str(value)

    def _env(self) -> Mapping[str, str] | None:
        if self.env is None:
            return None
        merged = os.environ.copy()
        merged.update(self.env)
        return merged

    @staticmethod
    def _parse_json_report(command: Sequence[str], stdout: str, stderr: str, returncode: int) -> JSON:
        stripped = stdout.strip()
        if stripped:
            try:
                value = json.loads(stripped)
            except json.JSONDecodeError as exc:
                raise MolstarError(
                    f"command did not return JSON: {' '.join(command)}: {exc}: {stderr.strip()}"
                ) from exc
            if isinstance(value, dict):
                return value
            raise MolstarError(f"command returned non-object JSON: {' '.join(command)}")
        if returncode == 0:
            return {"ok": True}
        return {"ok": False, "error": {"message": stderr.strip() or "command failed"}}


def image_output(path: str | os.PathLike[str], size: tuple[int, int] = (800, 800), *, transparent: bool = False) -> JSON:
    return {
        "type": "image",
        "path": str(path),
        "size": [size[0], size[1]],
        "transparent": transparent,
    }


def demo_job(path: str | os.PathLike[str], *, size: tuple[int, int] = (96, 72)) -> JSON:
    return {
        "version": 1,
        "runtime": {"strict": True},
        "inputs": {"input": {"id": "1cbs", "provider": "pdbe"}},
        "scene": {
            "canvas": {"background": "white"},
            "structures": [
                {
                    "ref": "structure",
                    "source": "input",
                    "components": [
                        {
                            "ref": "polymer",
                            "select": "polymer",
                            "representation": {"type": "cartoon", "color": "chain"},
                        },
                        {
                            "ref": "ligand",
                            "select": "ligand",
                            "representation": {"type": "ball-and-stick", "color": "#cc3399"},
                        },
                    ],
                }
            ],
            "camera": {"focus": "ligand"},
        },
        "outputs": [image_output(path, size)],
    }


def _error_message(report: Mapping[str, Any], stderr: str) -> str:
    error = report.get("error")
    if isinstance(error, Mapping):
        message = error.get("message")
        if isinstance(message, str) and message:
            return message
    if stderr.strip():
        return stderr.strip()
    return "molstar command failed"
