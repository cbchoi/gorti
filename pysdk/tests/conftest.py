"""Collection scope for the maintained Python SDK tests."""

# Preserve the legacy fixture without requiring a C++ SDK in the default suite.
collect_ignore = ["spec/m17/test_cpp_python_interop.py"]
