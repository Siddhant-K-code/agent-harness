"""Run inside the real non-root image, without Docker network restrictions."""
import os
from pathlib import Path
import socket
import subprocess

# Positive controls: the surrounding container permits both networking and writes.
for family, kind in [(socket.AF_INET, socket.SOCK_STREAM), (socket.AF_INET, socket.SOCK_DGRAM),
                     (socket.AF_UNIX, socket.SOCK_STREAM)]:
    socket.socket(family, kind).close()
Path('/workspace/tags.js').write_text('original')


def guard(profile, script):
    result = subprocess.run(['/usr/local/bin/harness-guard', profile, '--', script],
                            input='', capture_output=True, text=True,
                            env={**os.environ, 'HARNESS_SECRET_CANARY': 'must-not-reach-command'})
    assert result.returncode == 0, (result.returncode, result.stdout, result.stderr)
    return result.stdout

print(guard('work', 'python3 /opt/harness/isolation-checks.py network'))
print(guard('verify', 'python3 /opt/harness/isolation-checks.py readonly'))
print(guard('work', 'test -z "${HARNESS_SECRET_CANARY:-}"; printf changed > /workspace/tags.js; node --version'))
assert Path('/workspace/tags.js').read_text() == 'changed'
# Policy is mandatory; malformed profile must never execute its payload.
bad = subprocess.run(['/usr/local/bin/harness-guard', 'unrestricted', '--', 'touch /workspace/bypass'],
                     input='', capture_output=True, text=True)
assert bad.returncode == 125 and not Path('/workspace/bypass').exists()
# Even an explicitly inherited socket descriptor is closed before execution.
with socket.socket() as inherited:
    os.dup2(inherited.fileno(), 64)
    closed = subprocess.run(['/usr/local/bin/harness-guard', 'work', '--',
        "python3 -c 'import os,errno;\ntry: os.fstat(64)\nexcept OSError as e: assert e.errno==errno.EBADF; exit(0)\nexit(1)'"],
        input='', capture_output=True, text=True, pass_fds=(64,))
    os.close(64)
    assert closed.returncode == 0, (closed.returncode, closed.stderr)
print('Isolation checks and positive controls passed.')
