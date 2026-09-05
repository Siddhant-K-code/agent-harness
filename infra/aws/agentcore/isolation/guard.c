/* Mandatory, single-threaded Linux command policy, inherited by descendants. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <linux/sched.h>
#include <seccomp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <unistd.h>

static void fail(const char *message) { perror(message); exit(125); }
static void rule(scmp_filter_ctx filter, const char *name, unsigned action) {
    int number = seccomp_syscall_resolve_name(name);
    /* Absent native syscalls cannot be called; other ABIs are rejected by libseccomp. */
    if (number >= 0 && seccomp_rule_add(filter, action, number, 0)) {
        errno = EINVAL; fail("seccomp rule");
    }
}
int main(int argc, char **argv) {
    if (argc != 4 || strcmp(argv[2], "--") ||
        (strcmp(argv[1], "work") && strcmp(argv[1], "verify"))) {
        fputs("usage: harness-guard work|verify -- SCRIPT\n", stderr); return 125;
    }
    if (getuid() == 0 || geteuid() == 0) { fputs("guard refuses root\n", stderr); return 125; }
    struct stat null_device;
    if (stat("/dev/null", &null_device)) fail("stat null device");
    for (int fd = 0; fd < 3; fd++) {
        struct stat st;
        if (fstat(fd, &st) || (!S_ISFIFO(st.st_mode) && !(S_ISCHR(st.st_mode) && st.st_rdev == null_device.st_rdev))) {
            fputs("guard requires pipe/device standard streams\n", stderr); return 125;
        }
    }
    if (syscall(SYS_close_range, 3u, ~0u, 0)) fail("close inherited descriptors");
    if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)) fail("no_new_privs");
    int readonly = !strcmp(argv[1], "verify");
    /* Allowlisting also rejects future syscall additions until reviewed. */
    scmp_filter_ctx filter = seccomp_init(SCMP_ACT_ERRNO(EPERM));
    if (!filter) fail("seccomp init");
    const char *common[] = {
        "access", "faccessat", "faccessat2", "arch_prctl", "brk", "chdir", "fchdir",
        "clock_getres", "clock_gettime", "clock_nanosleep", "close", "close_range",
        "dup", "dup2", "dup3", "epoll_create", "epoll_create1", "epoll_ctl",
        "epoll_wait", "epoll_pwait", "epoll_pwait2", "eventfd", "eventfd2", "exit", "exit_group",
        "execve", "execveat", "flock", "fstat", "fstat64", "newfstatat",
        "fstatat64", "stat", "stat64", "lstat", "lstat64", "statx", "statfs", "statfs64",
        "fstatfs", "fstatfs64", "getcwd", "getdents", "getdents64", "getegid", "geteuid",
        "getgid", "getuid", "getgroups", "getpid", "getppid", "gettid", "getpgrp", "getpgid",
        "getsid", "getpriority", "getrandom", "getresgid", "getresuid", "getrlimit", "getrusage",
        "gettimeofday", "lseek", "_llseek", "madvise", "membarrier", "mincore", "mmap", "mmap2",
        "mprotect", "mremap", "munmap", "mseal", "msync", "nanosleep", "pause", "pipe", "pipe2",
        "poll", "ppoll", "select", "_newselect", "pselect6", "pread64", "preadv", "preadv2",
        "read", "readv", "pwrite64", "pwritev", "pwritev2", "write", "writev", "fsync", "fdatasync",
        "futex", "futex_waitv", "futex_wait", "futex_wake", "futex_requeue", "restart_syscall",
        "rseq", "set_tid_address", "set_robust_list", "get_robust_list", "sched_getaffinity",
        "sched_yield", "sched_getparam", "sched_getscheduler", "sched_get_priority_max",
        "sched_get_priority_min", "rt_sigaction", "rt_sigpending", "rt_sigprocmask", "rt_sigreturn",
        "rt_sigsuspend", "rt_sigtimedwait", "sigaltstack", "signalfd", "signalfd4", "sigreturn",
        "wait4", "waitid", "waitpid", "fork", "vfork", "uname", "sysinfo", "times", "umask",
        "getxattr", "lgetxattr", "fgetxattr", "listxattr", "llistxattr", "flistxattr",
        "readlink", "readlinkat", "readahead", "fadvise64", "fadvise64_64", "copy_file_range",
        "sendfile", "sendfile64", "splice", "tee", "vmsplice", NULL
    };
    for (int i = 0; common[i]; i++) rule(filter, common[i], SCMP_ACT_ALLOW);
    /* Exclude async-I/O signalling and descriptor ownership manipulation. */
    const char *fcntls[] = {"fcntl", "fcntl64"};
    int commands[] = {F_DUPFD, F_DUPFD_CLOEXEC, F_GETFD, F_SETFD, F_GETFL, F_SETLK, F_SETLKW, F_GETLK};
    for (unsigned i = 0; i < 2; i++) {
        int nr = seccomp_syscall_resolve_name(fcntls[i]);
        if (nr < 0) continue;
        for (unsigned j = 0; j < sizeof(commands)/sizeof(commands[0]); j++)
            if (seccomp_rule_add(filter, SCMP_ACT_ALLOW, nr, 1,
                                SCMP_A1(SCMP_CMP_EQ, commands[j]))) fail("fcntl policy");
        if (seccomp_rule_add(filter, SCMP_ACT_ALLOW, nr, 2, SCMP_A1(SCMP_CMP_EQ, F_SETFL),
                            SCMP_A2(SCMP_CMP_MASKED_EQ, O_ASYNC, 0))) fail("fd flags policy");
    }
    /* File writes through inherited descriptors are impossible except standard I/O:
       all other descriptors were closed, and verify cannot open a writable file. */
    unsigned write_flags = O_ACCMODE | O_CREAT | O_TRUNC | O_APPEND;
    const char *opens[] = {"open", "openat"};
    for (unsigned i = 0; i < 2; i++) {
        int nr = seccomp_syscall_resolve_name(opens[i]);
        if (nr < 0) continue;
        int rc = readonly ? seccomp_rule_add(filter, SCMP_ACT_ALLOW, nr, 1,
                        SCMP_CMP(i + 1, SCMP_CMP_MASKED_EQ, write_flags, 0))
                          : seccomp_rule_add(filter, SCMP_ACT_ALLOW, nr, 0);
        if (rc) fail("file open policy");
    }
    /* Pointer-encoded open flags cannot be checked by classic seccomp. */
    rule(filter, "openat2", SCMP_ACT_ERRNO(ENOSYS));
    rule(filter, "clone3", SCMP_ACT_ERRNO(ENOSYS));
    unsigned long namespaces = CLONE_NEWUSER | CLONE_NEWNET | CLONE_NEWNS | CLONE_NEWPID |
                              CLONE_NEWIPC | CLONE_NEWUTS | CLONE_NEWCGROUP;
    if (seccomp_rule_add(filter, SCMP_ACT_ALLOW, SCMP_SYS(clone), 1,
                        SCMP_A0(SCMP_CMP_MASKED_EQ, namespaces, 0))) fail("clone policy");
    if (seccomp_rule_add(filter, SCMP_ACT_ALLOW, SCMP_SYS(ioctl), 1,
                        SCMP_A1(SCMP_CMP_EQ, FIOCLEX))) fail("ioctl policy");
    /* Nonblocking mode is needed for Node's pipe-backed stdio; it grants no write or socket access. */
    if (seccomp_rule_add(filter, SCMP_ACT_ALLOW, SCMP_SYS(ioctl), 1,
                        SCMP_A1(SCMP_CMP_EQ, FIONBIO))) fail("nonblocking I/O policy");
    int prctls[] = {PR_GET_NAME, PR_SET_NAME, PR_GET_NO_NEW_PRIVS, PR_GET_SECCOMP};
    for (unsigned i = 0; i < sizeof(prctls)/sizeof(prctls[0]); i++)
        if (seccomp_rule_add(filter, SCMP_ACT_ALLOW, SCMP_SYS(prctl), 1,
                            SCMP_A0(SCMP_CMP_EQ, prctls[i]))) fail("prctl policy");
    if (seccomp_rule_add(filter, SCMP_ACT_ALLOW, SCMP_SYS(prctl), 2,
                        SCMP_A0(SCMP_CMP_EQ, PR_SET_NO_NEW_PRIVS), SCMP_A1(SCMP_CMP_EQ, 1))) fail("privilege policy");
    rule(filter, "seccomp", SCMP_ACT_ALLOW); /* Filters can only add restrictions under no_new_privs. */
    if (seccomp_rule_add(filter, SCMP_ACT_ALLOW, SCMP_SYS(prlimit64), 1,
                        SCMP_A0(SCMP_CMP_EQ, 0))) fail("self rlimit policy");
    if (!readonly) {
        const char *writes[] = {"creat", "truncate", "truncate64", "ftruncate", "ftruncate64",
            "fallocate", "rename", "renameat", "renameat2", "unlink", "unlinkat", "rmdir", "mkdir",
            "mkdirat", "link", "linkat", "symlink", "symlinkat", "mknod", "mknodat", "chmod", "fchmod",
            "fchmodat", "fchmodat2", "chown", "fchown", "lchown", "fchownat", "utime", "utimes",
            "futimesat", "utimensat", "setxattr", "lsetxattr", "fsetxattr", "removexattr",
            "lremovexattr", "fremovexattr", NULL};
        for (int i = 0; writes[i]; i++) rule(filter, writes[i], SCMP_ACT_ALLOW);
    }
    if (seccomp_load(filter)) fail("enforce seccomp policy");
    seccomp_release(filter);
    if (clearenv() || setenv("PATH", "/usr/local/bin:/usr/bin:/bin", 1) ||
        setenv("HOME", "/tmp", 1) || setenv("LANG", "C.UTF-8", 1) ||
        setenv("PYTHONDONTWRITEBYTECODE", "1", 1)) fail("clean environment");
    if (chdir("/workspace")) fail("workspace");
    execl("/bin/bash", "bash", "--noprofile", "--norc", "-c", argv[3], (char *)NULL);
    fail("exec command");
}
