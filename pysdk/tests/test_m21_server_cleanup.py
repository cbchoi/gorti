"""Direct RTI processes are reaped by their owned handle, not a PID shell tool."""

from types import SimpleNamespace

from tests.spec.m21 import _processes


def test_windows_rti_cleanup_uses_owned_handle_and_waits(monkeypatch):
    calls = []
    process = SimpleNamespace(
        pid=12345, poll=lambda: None,
        kill=lambda: calls.append("kill-owned-handle"),
        wait=lambda **kwargs: calls.append(("wait", kwargs)),
    )
    monkeypatch.setattr(_processes, "os", SimpleNamespace(name="nt"))
    monkeypatch.setattr(
        _processes.subprocess, "run",
        lambda *args, **kwargs: calls.append("unexpected-taskkill"),
    )
    _processes.stop(process, has_children=False)
    assert calls == ["kill-owned-handle", ("wait", {"timeout": 5})]


def test_already_exited_rti_is_not_signalled(monkeypatch):
    calls = []
    process = SimpleNamespace(poll=lambda: 0, kill=lambda: calls.append("kill"))
    monkeypatch.setattr(_processes, "os", SimpleNamespace(name="nt"))
    _processes.stop(process, has_children=False)
    assert calls == []
