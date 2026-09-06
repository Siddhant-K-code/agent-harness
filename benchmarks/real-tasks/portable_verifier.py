from pathlib import Path
import shutil
import tempfile
from types import SimpleNamespace

import llmtracefx
from llmtracefx.cache_audit.bundle import _portable_verifier, package_source_digest

# Generate the production verifier with a legitimate source-archive binding.
# The generator consumes these two manifest fields; no provider/executor is substituted.
source = _portable_verifier(SimpleNamespace(generator_commit=None, generator_package_digest=package_source_digest()))
namespace = {"__name__": "independent_portable_verifier"}
exec(compile(source, "generated-evidence-verifier.py", "exec"), namespace)

with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    package = Path(llmtracefx.__file__).parent
    shutil.copytree(package, root / "llmtracefx", ignore=shutil.ignore_patterns("__pycache__"))
    nested = root / "evidence" / "published"
    nested.mkdir(parents=True)
    resolve = namespace["resolve_package"]
    for explicit in (None, root):
        snapshot = resolve(nested, explicit)
        assert (snapshot / "llmtracefx" / "cache_audit" / "bundle.py").read_bytes() == (package / "cache_audit" / "bundle.py").read_bytes()
        shutil.rmtree(snapshot)
    (nested / "llmtracefx").symlink_to(root / "llmtracefx", target_is_directory=True)
    snapshot = resolve(nested, None)
    shutil.rmtree(snapshot)
    try:
        resolve(nested, nested)
    except SystemExit:
        pass
    else:
        raise AssertionError("explicit symlink source must be rejected")
    (root / "llmtracefx" / "__init__.py").write_text("# changed source\n")
    try:
        resolve(nested, root)
    except SystemExit:
        pass
    else:
        raise AssertionError("mismatched package digest was accepted")
print("PASS: generated verifier discovery and source binding")
