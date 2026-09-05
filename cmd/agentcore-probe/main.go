// agentcore-probe runs a bounded, deterministic experiment on real AWS
// infrastructure. It is separate from the production model controller.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/sandbox/agentcore"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	control "github.com/aws/aws-sdk-go-v2/service/bedrockagentcorecontrol"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcorecontrol/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

type State struct {
	Version        int       `json:"version"`
	Name           string    `json:"name"`
	Region         string    `json:"region"`
	RuntimeID      string    `json:"runtime_id,omitempty"`
	RuntimeARN     string    `json:"runtime_arn,omitempty"`
	Sessions       []string  `json:"sessions"`
	Created        time.Time `json:"created"`
	Deleted        bool      `json:"runtime_deletion_confirmed"`
	CreateRejected bool      `json:"create_explicitly_rejected,omitempty"`
}
type Check struct {
	Name   string           `json:"name"`
	Result agentcore.Result `json:"result"`
	Error  string           `json:"error,omitempty"`
}
type Report struct {
	Version          int            `json:"version"`
	Region           string         `json:"region"`
	Image            string         `json:"image"`
	PatchSHA256      string         `json:"patch_sha256"`
	VerifierSHA256   string         `json:"verifier_sha256"`
	ModelCalls       int            `json:"new_model_calls"`
	Started          time.Time      `json:"started"`
	Finished         time.Time      `json:"finished"`
	Checks           []Check        `json:"checks"`
	StopAcknowledged bool           `json:"stop_acknowledged"`
	RuntimeDeleted   bool           `json:"runtime_deletion_confirmed"`
	Passed           bool           `json:"probe_passed"`
	Error            string         `json:"error,omitempty"`
	Limits           map[string]any `json:"limits_and_gaps"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	image := flag.String("image", "", "immutable ECR image URI with @sha256 digest")
	role := flag.String("execution-role", "", "scoped runtime execution role ARN")
	region := flag.String("region", "us-east-1", "experiment region (us-east-1 only)")
	filename := flag.String("state", ".harness/aws-probe.json", "private durable cleanup state")
	cleanupOnly := flag.Bool("cleanup", false, "reconcile and delete the runtime named in the state file")
	flag.Parse()
	if *region != "us-east-1" {
		return errors.New("this bounded experiment is restricted to us-east-1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region), config.WithRetryMaxAttempts(1))
	if err != nil {
		return err
	}
	who, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return err
	}
	if !strings.Contains(aws.ToString(who.Arn), ":assumed-role/agent-harness-deploy/") {
		return errors.New("probe requires the scoped agent-harness-deploy role; root and IAM user credentials are refused")
	}
	cp := control.NewFromConfig(cfg)
	if *cleanupOnly {
		b, err := os.ReadFile(*filename)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		var state State
		if err = json.Unmarshal(b, &state); err != nil {
			return err
		}
		return cleanup(ctx, cp, cfg, *filename, &state)
	}
	if !strings.Contains(*image, ".dkr.ecr.us-east-1.amazonaws.com/agent-harness-probe@sha256:") || len(strings.Split(*image, "@sha256:")[1]) != 64 || !strings.HasSuffix(*role, ":role/agent-harness-runtime") {
		return errors.New("provide the scoped ECR digest and runtime role")
	}
	if _, err := os.Lstat(*filename); err == nil {
		return errors.New("state file already exists; run --cleanup and choose a new state path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	state := State{Version: 1, Name: "agent_harness_probe_" + hex.EncodeToString(nonce[:]), Region: *region, Created: time.Now().UTC()}
	report := Report{Version: 1, Region: *region, Image: *image, Started: state.Created, Limits: map[string]any{"max_sessions": 3, "idle_session_seconds": 60, "max_session_lifetime_seconds": 600, "automatic_model_calls": false, "network": "PUBLIC", "network_disabled": false, "read_only_verifier_workspace_enforced": false, "session_absence_inspection": false, "workspace_disk_quota_enforced": false, "production_backend_enabled": false}}
	if err := save(*filename, state); err != nil {
		return err
	}
	experimentErr := experiment(ctx, cp, cfg, *filename, &state, &report, *image, *role)
	cleanupCtx, stop := context.WithTimeout(context.Background(), 8*time.Minute)
	cleanupErr := cleanup(cleanupCtx, cp, cfg, *filename, &state)
	stop()
	combined := errors.Join(experimentErr, cleanupErr)
	report.Finished = time.Now().UTC()
	report.RuntimeDeleted = state.Deleted
	report.Passed = combined == nil
	if combined != nil {
		report.Error = combined.Error()
	}
	if err := save(*filename+".report.json", report); err != nil {
		combined = errors.Join(combined, err)
	}
	b, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(b))
	return combined
}

func experiment(ctx context.Context, cp *control.Client, cfg aws.Config, filename string, state *State, report *Report, image, role string) error {
	created, err := cp.CreateAgentRuntime(ctx, &control.CreateAgentRuntimeInput{
		AgentRuntimeName: aws.String(state.Name), RoleArn: aws.String(role), ClientToken: aws.String(strings.ReplaceAll(state.Name, "_", "-")),
		AgentRuntimeArtifact:   &types.AgentRuntimeArtifactMemberContainerConfiguration{Value: types.ContainerConfiguration{ContainerUri: aws.String(image)}},
		NetworkConfiguration:   &types.NetworkConfiguration{NetworkMode: types.NetworkModePublic},
		ProtocolConfiguration:  &types.ProtocolConfiguration{ServerProtocol: types.ServerProtocolHttp},
		LifecycleConfiguration: &types.LifecycleConfiguration{IdleRuntimeSessionTimeout: aws.Int32(60), MaxLifetime: aws.Int32(600)},
		Tags:                   map[string]string{"Project": "agent-harness", "Purpose": "bounded-probe"},
	})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) && (api.ErrorCode() == "ValidationException" || api.ErrorCode() == "AccessDeniedException") {
			state.CreateRejected = true
			if e := save(filename, *state); e != nil {
				return errors.Join(err, e)
			}
		}
		return fmt.Errorf("create runtime: %w", err)
	}
	state.RuntimeID = aws.ToString(created.AgentRuntimeId)
	state.RuntimeARN = aws.ToString(created.AgentRuntimeArn)
	if err := save(filename, state); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "runtime created; waiting for READY")
	for {
		got, err := cp.GetAgentRuntime(ctx, &control.GetAgentRuntimeInput{AgentRuntimeId: aws.String(state.RuntimeID)})
		if err != nil {
			return err
		}
		if got.Status == types.AgentRuntimeStatusReady {
			break
		}
		if got.Status == types.AgentRuntimeStatusCreateFailed {
			return fmt.Errorf("runtime creation failed: %s", aws.ToString(got.FailureReason))
		}
		if err := pause(ctx, 5*time.Second); err != nil {
			return err
		}
	}
	client := agentcore.New(cfg, state.RuntimeARN)
	start := func(suffix string) (string, error) {
		session := state.Name + "-" + suffix
		state.Sessions = append(state.Sessions, session)
		if len(state.Sessions) > 3 {
			return "", errors.New("session limit exceeded")
		}
		if err := save(filename, state); err != nil {
			return "", err
		}
		fmt.Fprintln(os.Stderr, "initializing", suffix, "session")
		return session, client.Start(ctx, session)
	}
	command := func(name, session, script string, timeout int32) (agentcore.Result, error) {
		fmt.Fprintln(os.Stderr, "check:", name)
		result, err := client.Command(ctx, session, "/bin/bash -c "+quote(script), timeout)
		check := Check{Name: name, Result: result}
		if err != nil {
			check.Error = err.Error()
		}
		report.Checks = append(report.Checks, check)
		if e := save(filename+".progress.json", report); e != nil {
			err = errors.Join(err, e)
		}
		return result, err
	}
	expect := func(name, session, script string, timeout int32, exit int32, status string) error {
		result, err := command(name, session, script, timeout)
		if err != nil {
			return err
		}
		if result.ExitCode == nil || *result.ExitCode != exit || result.Status != status {
			return fmt.Errorf("%s: unexpected exit or status", name)
		}
		return nil
	}
	source, err := os.ReadFile("examples/normalize-tags/tags.js")
	if err != nil {
		return err
	}
	patch, err := os.ReadFile("docs/evidence/2026-09-05/changes.patch")
	if err != nil {
		return err
	}
	verifier, err := os.ReadFile("examples/normalize-tags/verify.sh")
	if err != nil {
		return err
	}
	report.PatchSHA256 = digest(patch)
	report.VerifierSHA256 = digest(verifier)
	install := put("/workspace/tags.js", source) + put("/tmp/candidate.patch", patch) + "patch -d /workspace -p1 < /tmp/candidate.patch\n"
	session, err := start("work")
	if err != nil {
		return err
	}
	if err = expect("nonroot_and_tools", session, "set -eu; test \"$(id -u)\" -ne 0; node --version; python3 --version; git --version", 20, 0, "COMPLETED"); err != nil {
		return err
	}
	if err = expect("original_bug_rejected", session, "set -eu\n"+put("/workspace/tags.js", source)+"/bin/bash -c "+quote(string(verifier)), 20, 1, "COMPLETED"); err != nil {
		return err
	}
	if err = expect("apply_real_gpt54_patch", session, "set -eu\n"+install+"touch /workspace/session-marker\nnode /workspace/tags.test.js", 20, 0, "COMPLETED"); err != nil {
		return err
	}
	if err = expect("file_persistence", session, "set -eu; test -f /workspace/session-marker; sha256sum /workspace/tags.js; node /workspace/tags.test.js", 20, 0, "COMPLETED"); err != nil {
		return err
	}
	verifySession, err := start("verification")
	if err != nil {
		return err
	}
	if err = expect("fresh_session_verification", verifySession, "set -eu; test ! -e /workspace/session-marker\n"+install+"/bin/bash -c "+quote(string(verifier)), 20, 0, "COMPLETED"); err != nil {
		return err
	}
	result, err := command("nonzero_exit_and_stderr", session, "echo deliberate-error >&2; exit 7", 10)
	if err != nil {
		return err
	}
	if result.ExitCode == nil || *result.ExitCode != 7 || result.Status != "COMPLETED" || result.Stderr != "deliberate-error\n" {
		return errors.New("nonzero exit or stderr was not preserved")
	}
	result, err = command("bounded_output", session, "python3 -c 'print(\"x\"*70000)'", 20)
	if err != nil {
		return err
	}
	if !result.Truncated || len(result.Stdout) != agentcore.MaxOutput || result.ExitCode == nil || *result.ExitCode != 0 {
		return errors.New("output truncation check failed")
	}
	result, err = command("server_timeout", session, "sleep 10", 2)
	if err != nil {
		return err
	}
	if result.Status != "TIMED_OUT" {
		return errors.New("server did not report timeout")
	}
	cancelSession, err := start("cancellation")
	if err != nil {
		return err
	}
	cancelCtx, stop := context.WithTimeout(ctx, time.Second)
	result, err = client.Command(cancelCtx, cancelSession, "/bin/bash -c 'sleep 10'", 10)
	stop()
	check := Check{Name: "controller_disconnect", Result: result}
	if err != nil {
		check.Error = err.Error()
	}
	report.Checks = append(report.Checks, check)
	if !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("disconnect must end at the controller deadline, got: %v", err)
	}
	if err = client.Stop(ctx, cancelSession); err != nil {
		return err
	}
	report.StopAcknowledged = true
	return nil
}

func cleanup(ctx context.Context, cp *control.Client, cfg aws.Config, filename string, state *State) error {
	if state.Version != 1 || state.Region != "us-east-1" || !strings.HasPrefix(state.Name, "agent_harness_probe_") || len(state.Name) != len("agent_harness_probe_")+24 {
		return errors.New("invalid cleanup identity")
	}
	if state.Deleted {
		return nil
	}
	// Recover an ambiguous Create response using the exact journaled name. Never
	// delete unrelated or merely prefix-matching resources.
	if state.RuntimeID == "" {
		pages := control.NewListAgentRuntimesPaginator(cp, &control.ListAgentRuntimesInput{})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				return err
			}
			for _, item := range page.AgentRuntimes {
				if aws.ToString(item.AgentRuntimeName) == state.Name {
					state.RuntimeID = aws.ToString(item.AgentRuntimeId)
					state.RuntimeARN = aws.ToString(item.AgentRuntimeArn)
				}
			}
		}
		if state.RuntimeID == "" {
			if state.CreateRejected {
				state.Deleted = true
				return save(filename, *state)
			}
			return errors.New("create outcome remains unknown; no runtime is visible yet; retain state and retry cleanup")
		}
	}
	if !strings.HasPrefix(state.RuntimeID, state.Name+"-") || !strings.HasSuffix(state.RuntimeARN, ":runtime/"+state.RuntimeID) {
		return errors.New("runtime identity mismatch")
	}
	client := agentcore.New(cfg, state.RuntimeARN)
	for _, session := range state.Sessions {
		if !strings.HasPrefix(session, state.Name+"-") {
			return errors.New("foreign session")
		}
		// Continue to whole-runtime deletion even if individual stop is uncertain.
		if err := client.Stop(ctx, session); err != nil {
			fmt.Fprintln(os.Stderr, "session stop not confirmed; proceeding with runtime deletion:", err)
		}
	}
	fmt.Fprintln(os.Stderr, "deleting experimental runtime")
	_, err := cp.DeleteAgentRuntime(ctx, &control.DeleteAgentRuntimeInput{AgentRuntimeId: aws.String(state.RuntimeID)})
	if err != nil && !notFound(err) {
		return err
	}
	for {
		_, err := cp.GetAgentRuntime(ctx, &control.GetAgentRuntimeInput{AgentRuntimeId: aws.String(state.RuntimeID)})
		if notFound(err) {
			state.Deleted = true
			return save(filename, *state)
		}
		if err != nil {
			return err
		}
		if err = pause(ctx, 5*time.Second); err != nil {
			return err
		}
	}
}
func notFound(err error) bool {
	var api smithy.APIError
	return errors.As(err, &api) && api.ErrorCode() == "ResourceNotFoundException"
}
func pause(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func put(path string, data []byte) string {
	return "printf %s " + quote(base64.StdEncoding.EncodeToString(data)) + " | base64 -d > " + quote(path) + "\n"
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func save(filename string, value any) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".probe-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(append(b, '\n')); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filename); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
