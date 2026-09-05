"""Real adversarial checks; executed inside the guard by local and AWS probes."""
import ctypes
import errno
import json
import os
from pathlib import Path
import socket
import subprocess
import sys


def denied(name, action):
    try:
        action()
    except OSError as exc:
        assert exc.errno in (errno.EPERM, errno.EACCES, errno.EXDEV), (name, exc)
    else:
        raise AssertionError(name + ' unexpectedly allowed')


def network():
    attempts = 0
    for family, kind in [(socket.AF_INET, socket.SOCK_STREAM), (socket.AF_INET6, socket.SOCK_STREAM),
                         (socket.AF_INET, socket.SOCK_DGRAM), (socket.AF_INET6, socket.SOCK_DGRAM),
                         (socket.AF_UNIX, socket.SOCK_STREAM)]:
        denied('socket creation', lambda: socket.socket(family, kind))
        attempts += 1
    libc = ctypes.CDLL(None, use_errno=True)
    # io_uring can otherwise submit network operations without socket syscalls.
    assert libc.syscall(425, 1, ctypes.c_void_p()) == -1 and ctypes.get_errno() == errno.EPERM
    child = subprocess.run([sys.executable, '-c',
        'import socket,sys\ntry: socket.socket()\nexcept PermissionError: sys.exit(0)\nsys.exit(1)'], check=True)
    status = Path('/proc/self/status').read_text()
    assert 'NoNewPrivs:\t1' in status and 'Seccomp:\t2' in status
    print(json.dumps({'socket_families_denied': attempts, 'io_uring_denied': True,
                      'child_inherits_policy': child.returncode == 0, 'no_new_privs': True}))


def readonly():
    candidate = Path('/workspace/tags.js')
    before = candidate.read_bytes()
    mode = candidate.stat().st_mode
    denied('overwrite', lambda: candidate.write_text('tampered'))
    denied('truncate', lambda: os.truncate(candidate, 0))
    with candidate.open('rb') as original:
        denied('proc descriptor alias', lambda: Path('/proc/self/fd/' + str(original.fileno())).write_text('tampered'))
    denied('unlink', candidate.unlink)
    denied('rename', lambda: candidate.rename('/tmp/moved-candidate'))
    denied('new workspace file', lambda: Path('/workspace/created').write_text('bad'))
    denied('chmod', lambda: candidate.chmod(0o777))
    denied('touch', lambda: os.utime(candidate, None))
    denied('hardlink', lambda: os.link(candidate, '/tmp/candidate-hardlink'))
    alias = Path('/tmp/candidate-alias')
    alias.symlink_to(candidate)
    denied('symlink alias write', lambda: alias.write_text('tampered'))
    alias.unlink()
    # A nested launcher cannot relax the parent's Landlock/seccomp restrictions.
    child = subprocess.run(['/usr/local/bin/harness-guard', 'work', '--',
                            'printf tampered > /workspace/tags.js'], capture_output=True)
    assert child.returncode != 0
    assert candidate.read_bytes() == before and candidate.stat().st_mode == mode
    Path('/tmp/allowed-scratch').write_text('temporary files work')
    print(json.dumps({'workspace_mutation_attempts_denied': 11, 'scratch_writable': True,
                      'candidate_bytes_and_mode_unchanged': True}))

if __name__ == '__main__':
    {'network': network, 'readonly': readonly}[sys.argv[1]]()
