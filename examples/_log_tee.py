"""Shared helper: stream a subprocess's stdout/stderr to BOTH a log
file AND the parent's stdout/stderr with a per-line prefix tag.

Used by every cross-process example runner so that:

  1. The user sees what each subprocess is doing in real time.
  2. A complete log of every subprocess is also captured to disk
     under the example's working directory.

Usage::

    proc = subprocess.Popen([...], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    tee = LogTee(proc, log_path=workdir / "rtid.log", prefix="rtid")
    tee.start()
    # ... later, after proc exits:
    tee.join()

The tee runs a background daemon thread that reads the subprocess's
combined stdout+stderr stream line-by-line and writes each line to:

  - the log file (binary, no prefix — preserves the subprocess's
    raw output for grep / structured parsing).
  - the parent's stderr (text, with prefix — for live observability).

Runs cleanly on POSIX + Windows. No external dependencies.
"""

from __future__ import annotations

import io
import sys
import threading
from pathlib import Path
from subprocess import Popen
from typing import IO


class LogTee:
    """Tee a subprocess's combined stdout+stderr to a log file + parent stderr.

    The subprocess MUST be created with::

        Popen([...], stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
              env={**os.environ, "PYTHONUNBUFFERED": "1"})

    so the pipe receives both streams in interleaved arrival order
    and Python child processes flush every line.
    """

    def __init__(
        self,
        proc: Popen,  # type: ignore[type-arg]
        *,
        log_path: Path,
        prefix: str,
        echo_stream: IO[str] | None = None,
    ) -> None:
        if proc.stdout is None:
            raise ValueError(
                "LogTee: subprocess must be Popen'd with stdout=subprocess.PIPE"
            )
        log_path.parent.mkdir(parents=True, exist_ok=True)
        self._proc = proc
        self._stream = proc.stdout
        # Open the log file in binary so child output is preserved
        # byte-for-byte (matches the pre-tee behavior of dumping raw
        # bytes to disk).
        self._log_fh: IO[bytes] = log_path.open("wb")  # noqa: SIM115
        self._prefix = prefix
        self._echo: IO[str] = echo_stream if echo_stream is not None else sys.stderr
        self._thread: threading.Thread | None = None

    def start(self) -> None:
        """Spawn the daemon thread that pumps the subprocess output."""
        self._thread = threading.Thread(
            target=self._pump, name=f"log-tee-{self._prefix}", daemon=True,
        )
        self._thread.start()

    def join(self, timeout: float | None = None) -> None:
        """Wait for the pump thread to drain the subprocess's pipe.

        Call after ``proc.wait()``. Bounded by ``timeout`` if supplied.
        Closes the log file at the end.
        """
        if self._thread is not None:
            self._thread.join(timeout=timeout)
        try:
            self._log_fh.flush()
            self._log_fh.close()
        except Exception:  # noqa: BLE001
            pass

    def _pump(self) -> None:
        # readline returns b"" on EOF (subprocess pipe closed). The
        # subprocess Popen's stdout is a buffered binary reader.
        try:
            for raw_line in iter(self._stream.readline, b""):
                # Echo to console with the prefix tag.
                try:
                    text = raw_line.decode("utf-8", errors="replace")
                except Exception:  # noqa: BLE001
                    text = repr(raw_line)
                # Echo: include the prefix; keep the trailing newline
                # from the subprocess so the parent's view stays
                # aligned with the file.
                self._echo.write(f"[{self._prefix}] {text}")
                try:
                    self._echo.flush()
                except Exception:  # noqa: BLE001
                    pass
                # File: write raw bytes verbatim (no prefix; preserves
                # the subprocess's exact output for grep + tooling).
                try:
                    self._log_fh.write(raw_line)
                    self._log_fh.flush()
                except (OSError, ValueError):
                    # File closed or device full — drop, console echo
                    # already happened.
                    return
        finally:
            # Best-effort close on EOF.
            try:
                self._stream.close()
            except Exception:  # noqa: BLE001
                pass


def open_log_writer(log_path: Path) -> IO[bytes]:
    """Plain log-file opener for callers that don't need teeing.

    Convenience helper so callers can switch between tee'd and
    file-only logging without touching their Popen code.
    """
    log_path.parent.mkdir(parents=True, exist_ok=True)
    return log_path.open("wb")  # noqa: SIM115


# Re-export so callers can `from examples._log_tee import LogTee, open_log_writer`.
__all__ = ["LogTee", "open_log_writer"]
