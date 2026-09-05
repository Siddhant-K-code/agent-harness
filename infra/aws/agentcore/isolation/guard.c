/* Command boundary: single-threaded setup, then inherited Linux restrictions. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <linux/landlock.h>
#include <linux/sched.h>
#include <seccomp.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <unistd.h>

static void fail(const char *message) { perror(message); exit(125); }
static void rule(scmp_filter_ctx filter, const char *name, uint32_t action) {
    int number = seccomp_syscall_resolve_name(name);
    if (number != __NR_SCMP_ERROR && seccomp_rule_add(filter, action, number, 0)) {
        errno = EINVAL; fail("seccomp rule");
    }
}
static void allow_path(int ruleset, const char *path, uint64_t access) {
    int fd = open(path, O_PATH | O_CLOEXEC | O_NOFOLLOW);
    if (fd < 0) fail("open policy path");
    struct landlock_path_beneath_attr rule = {.allowed_access = access, .parent_fd = fd};
    if (syscall(SYS_landlock_add_rule, ruleset, LANDLOCK_RULE_PATH_BENEATH, &rule, 0)) fail("landlock rule");
    close(fd);
}
int main(int argc, char **argv) {
    if (argc != 4 || strcmp(argv[2], "--") ||
        (strcmp(argv[1], "work") && strcmp(argv[1], "verify"))) {
        fputs("usage: harness-guard work|verify -- SCRIPT\n", stderr); return 125;
    }
    if (getuid() == 0 || geteuid() == 0) { fputs("guard refuses root\n", stderr); return 125; }
    /* No inherited service sockets or arbitrary writable files cross the boundary. */
    for (int fd = 0; fd < 3; fd++) {
        struct stat st;
        if (fstat(fd, &st) || (!S_ISFIFO(st.st_mode) && !S_ISCHR(st.st_mode))) {
            fputs("guard requires pipe/device standard streams\n", stderr); return 125;
        }
    }
    if (syscall(SYS_close_range, 3u, ~0u, 0)) fail("close inherited descriptors");
    if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)) fail("no_new_privs");
    int abi = syscall(SYS_landlock_create_ruleset, NULL, 0, LANDLOCK_CREATE_RULESET_VERSION);
    if (abi < 3) { fputs("Landlock ABI >= 3 required; command refused\n", stderr); return 125; }
    uint64_t writes = LANDLOCK_ACCESS_FS_WRITE_FILE | LANDLOCK_ACCESS_FS_REMOVE_DIR |
        LANDLOCK_ACCESS_FS_REMOVE_FILE | LANDLOCK_ACCESS_FS_MAKE_CHAR | LANDLOCK_ACCESS_FS_MAKE_DIR |
        LANDLOCK_ACCESS_FS_MAKE_REG | LANDLOCK_ACCESS_FS_MAKE_SOCK | LANDLOCK_ACCESS_FS_MAKE_FIFO |
        LANDLOCK_ACCESS_FS_MAKE_BLOCK | LANDLOCK_ACCESS_FS_MAKE_SYM | LANDLOCK_ACCESS_FS_REFER |
        LANDLOCK_ACCESS_FS_TRUNCATE;
    struct landlock_ruleset_attr attr = {.handled_access_fs = writes};
    int ruleset = syscall(SYS_landlock_create_ruleset, &attr, sizeof(attr), 0);
    if (ruleset < 0) fail("create Landlock policy");
    allow_path(ruleset, "/tmp", writes);
    allow_path(ruleset, "/dev/null", LANDLOCK_ACCESS_FS_WRITE_FILE);
    int readonly = !strcmp(argv[1], "verify");
    if (!readonly) allow_path(ruleset, "/workspace", writes);
    if (syscall(SYS_landlock_restrict_self, ruleset, 0)) fail("enforce Landlock policy");
    close(ruleset);

    scmp_filter_ctx filter = seccomp_init(SCMP_ACT_ALLOW);
    if (!filter) fail("seccomp init");
    const char *denied[] = {"socket", "socketpair", "socketcall", "connect", "bind", "listen",
        "accept", "accept4", "sendto", "sendmsg", "sendmmsg", "recvfrom", "recvmsg", "recvmmsg",
        "io_uring_setup", "io_uring_enter", "io_uring_register", "ptrace", "process_vm_readv",
        "process_vm_writev", "pidfd_getfd", "pidfd_send_signal", "kill", "tkill", "tgkill",
        "unshare", "setns", "mount", "umount2", "pivot_root", "chroot", "open_by_handle_at",
        "bpf", "perf_event_open", "userfaultfd", NULL};
    for (int i = 0; denied[i]; i++) rule(filter, denied[i], SCMP_ACT_ERRNO(EPERM));
    /* musl fopen uses FIOCLEX; it only marks an fd close-on-exec. */
    if (seccomp_rule_add(filter, SCMP_ACT_ERRNO(EPERM), SCMP_SYS(ioctl), 1,
                        SCMP_A1(SCMP_CMP_NE, FIOCLEX))) fail("ioctl policy");
    /* libc can fall back to clone for ordinary threads. Namespace clones fail. */
    rule(filter, "clone3", SCMP_ACT_ERRNO(ENOSYS));
    unsigned long flags[] = {CLONE_NEWUSER, CLONE_NEWNET, CLONE_NEWNS, CLONE_NEWPID,
                            CLONE_NEWIPC, CLONE_NEWUTS, CLONE_NEWCGROUP};
    for (unsigned i = 0; i < sizeof(flags)/sizeof(flags[0]); i++) {
        if (seccomp_rule_add(filter, SCMP_ACT_ERRNO(EPERM), SCMP_SYS(clone), 1,
                            SCMP_A0(SCMP_CMP_MASKED_EQ, flags[i], flags[i]))) fail("clone policy");
    }
    if (readonly) {
        const char *metadata[] = {"chmod", "fchmod", "fchmodat", "fchmodat2", "chown", "fchown",
            "lchown", "fchownat", "utime", "utimes", "futimesat", "utimensat", "setxattr",
            "lsetxattr", "fsetxattr", "removexattr", "lremovexattr", "fremovexattr", NULL};
        for (int i = 0; metadata[i]; i++) rule(filter, metadata[i], SCMP_ACT_ERRNO(EPERM));
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
