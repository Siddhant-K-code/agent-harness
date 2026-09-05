package agentcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/artifact"
	"github.com/Siddhant-K-code/agent-harness/internal/process"
	"github.com/Siddhant-K-code/agent-harness/internal/sandbox"
	"github.com/Siddhant-K-code/agent-harness/internal/workspace"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	runtime "github.com/aws/aws-sdk-go-v2/service/bedrockagentcore"
	control "github.com/aws/aws-sdk-go-v2/service/bedrockagentcorecontrol"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcorecontrol/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

var executionPattern = regexp.MustCompile(`^agent-harness-[a-f0-9]{32}-[1-9][0-9]*$`)
var pinnedImage = regexp.MustCompile(`^([0-9]{12})\.dkr\.ecr\.us-east-1\.amazonaws\.com/agent-harness-probe@sha256:[a-f0-9]{64}$`)

type Executor struct {
	Config            aws.Config
	Image, Role, Root string
	Workspace         *workspace.Workspace
	Progress          io.Writer
}

// Execution is a controller-private write-ahead record. The idempotency token
// and complete create parameters are durable before the first AWS request.
type Execution struct {
	Version           int                 `json:"version"`
	ID                string              `json:"execution_id"`
	Name              string              `json:"name"`
	Region            string              `json:"region"`
	Image             string              `json:"image"`
	Role              string              `json:"role"`
	RuntimeID         string              `json:"runtime_id,omitempty"`
	RuntimeARN        string              `json:"runtime_arn,omitempty"`
	Created           time.Time           `json:"created"`
	CreateAttempts    int                 `json:"create_attempts"`
	Rejected          bool                `json:"create_explicitly_rejected,omitempty"`
	Deleted           bool                `json:"runtime_deletion_confirmed"`
	CommandDispatched bool                `json:"command_dispatched"`
	CommandStarted    bool                `json:"command_started"`
	CommandResult     *Result             `json:"command_result,omitempty"`
	Input             workspace.Snapshot  `json:"input"`
	Candidate         *workspace.Snapshot `json:"candidate,omitempty"`
	Accepted          bool                `json:"candidate_accepted"`
}

func Check(ctx context.Context, region, image, role string) (aws.Config, error) {
	var cfg aws.Config
	match := pinnedImage.FindStringSubmatch(image)
	if region != "us-east-1" || len(match) != 2 || role != "arn:aws:iam::"+match[1]+":role/agent-harness-runtime" {
		return cfg, errors.New("agentcore requires the scoped runtime role and a digest-pinned agent-harness-probe ECR image in us-east-1")
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithRetryer(func() aws.Retryer { return aws.NopRetryer{} }))
	if err != nil {
		return cfg, err
	}
	identity, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return cfg, err
	}
	if aws.ToString(identity.Account) != match[1] || !strings.Contains(aws.ToString(identity.Arn), ":assumed-role/agent-harness-deploy/") {
		return cfg, errors.New("use the scoped agent-harness-deploy assumed role for remote execution")
	}
	return cfg, nil
}

func (e *Executor) Capabilities() sandbox.Capabilities {
	return sandbox.Capabilities{Backend: "agentcore", Isolation: "microvm_per_command_seccomp", NetworkDisabled: true, ReadOnlyWorkspace: true, DurableExecutionID: true}
}
func (e *Executor) log(message string) {
	if e.Progress != nil {
		fmt.Fprintln(e.Progress, "agentcore:", message)
	}
}
func (e *Executor) filename(id string) (string, error) {
	if !executionPattern.MatchString(id) {
		return "", errors.New("invalid execution reference")
	}
	return filepath.Join(e.Root, "aws-executions", id+".json"), nil
}
func runtimeName(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "agent_harness_probe_" + hex.EncodeToString(sum[:12])
}
func (e *Executor) cp() *control.Client { return control.NewFromConfig(e.Config) }

func (e *Executor) Execute(ctx context.Context, request sandbox.Request) (result process.Result, runErr error) {
	result.ExitCode = -1
	filename, err := e.filename(request.ID)
	if err != nil {
		return result, err
	}
	if request.TimeoutMS < 100 || request.TimeoutMS > 3600000 || e.Workspace == nil {
		return result, errors.New("invalid remote command timeout or workspace")
	}
	if _, err := os.Lstat(filename); !errors.Is(err, os.ErrNotExist) {
		return result, errors.New("execution reference already used; reconcile instead of replaying")
	}
	input, err := e.Workspace.SnapshotTo(ctx, filepath.Join(e.Root, "snapshots"))
	if err != nil {
		return result, err
	}
	if input.Bytes > artifact.MaxBytes {
		return result, errors.New("agentcore archive exceeds 8 MiB")
	}
	s := Execution{Version: 1, ID: request.ID, Name: runtimeName(request.ID), Region: e.Config.Region, Image: e.Image, Role: e.Role, Created: time.Now().UTC(), Input: input}
	if err := saveExecution(filename, s); err != nil {
		return result, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		if err := e.destroy(cleanup, filename, &s); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("%w: %v", sandbox.ErrCleanup, err))
		}
	}()
	if err := e.create(ctx, filename, &s); err != nil {
		return result, err
	}
	e.log("waiting for disposable runtime")
	if err := e.ready(ctx, s); err != nil {
		return result, err
	}
	client := New(e.Config, s.RuntimeARN)
	session := s.ID
	if err := client.Start(ctx, session); err != nil {
		return result, err
	}
	b, err := os.ReadFile(filepath.Join(e.Root, "snapshots", input.SHA256+".tar"))
	if err != nil {
		return result, err
	}
	ack, err := client.Artifact(ctx, session, artifact.Message{Operation: "import", Snapshot: input, Archive: b})
	if err != nil {
		return result, fmt.Errorf("import workspace: %w", err)
	}
	if ack.Operation != "imported" || ack.Snapshot != input {
		return result, errors.New("remote import identity mismatch")
	}
	s.CommandDispatched = true
	if err := saveExecution(filename, s); err != nil {
		return result, err
	}
	profile := "work"
	if request.ReadOnly {
		profile = "verify"
	}
	e.log("executing " + profile + " command")
	commandCtx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
	out, err := client.commandObserved(commandCtx, session, request.Command, profile, int32((request.TimeoutMS+999)/1000), func() error { s.CommandStarted = true; return saveExecution(filename, s) })
	cancel()
	s.CommandResult = &out
	if persist := saveExecution(filename, s); persist != nil {
		return result, errors.Join(err, persist)
	}
	result.Output, result.Stderr, result.Truncated = out.Stdout, out.Stderr, out.Truncated
	if out.ExitCode != nil {
		result.ExitCode = int(*out.ExitCode)
	}
	if err != nil {
		return result, fmt.Errorf("remote command outcome: %w", err)
	}
	if out.Status != "COMPLETED" {
		return result, errors.New("remote command timed out; candidate not accepted")
	}
	var exported artifact.Message
	if !request.ReadOnly {
		exported, err = client.Artifact(ctx, session, artifact.Message{Operation: "export"})
		if err != nil {
			return result, fmt.Errorf("export candidate: %w", err)
		}
		if exported.Operation != "exported" {
			return result, errors.New("unexpected artifact response")
		}
		if err := saveArchive(e.Root, exported); err != nil {
			return result, err
		}
		s.Candidate = &exported.Snapshot
		if err := saveExecution(filename, s); err != nil {
			return result, err
		}
	}
	// Snapshot bytes remain untrusted until validated; background processes may
	// mutate the guest while they are captured. Delete the entire runtime before
	// publishing that exact artifact locally or sending another model request.
	if err := e.destroy(ctx, filename, &s); err != nil {
		return result, err
	}
	if !request.ReadOnly {
		if err := e.Workspace.AcceptSnapshot(ctx, filepath.Join(e.Root, "snapshots", exported.Snapshot.SHA256+".tar"), exported.Snapshot); err != nil {
			return result, err
		}
		s.Accepted = true
		if err := saveExecution(filename, s); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (e *Executor) create(ctx context.Context, filename string, s *Execution) error {
	if s.RuntimeID != "" || s.Rejected || s.Deleted {
		return nil
	}
	s.CreateAttempts++
	if err := saveExecution(filename, *s); err != nil {
		return err
	}
	out, err := e.cp().CreateAgentRuntime(ctx, &control.CreateAgentRuntimeInput{
		AgentRuntimeName: aws.String(s.Name), ClientToken: aws.String(s.ID), RoleArn: aws.String(s.Role),
		AgentRuntimeArtifact:   &types.AgentRuntimeArtifactMemberContainerConfiguration{Value: types.ContainerConfiguration{ContainerUri: aws.String(s.Image)}},
		NetworkConfiguration:   &types.NetworkConfiguration{NetworkMode: types.NetworkModePublic},
		ProtocolConfiguration:  &types.ProtocolConfiguration{ServerProtocol: types.ServerProtocolHttp},
		LifecycleConfiguration: &types.LifecycleConfiguration{IdleRuntimeSessionTimeout: aws.Int32(60), MaxLifetime: aws.Int32(3600)},
		Tags:                   map[string]string{"Project": "agent-harness", "Purpose": "bounded-probe"},
	})
	if err != nil {
		if s.CreateAttempts == 1 && (apiCode(err, "ValidationException") || apiCode(err, "AccessDeniedException") || apiCode(err, "ServiceQuotaExceededException")) {
			s.Rejected = true
			return errors.Join(err, saveExecution(filename, *s))
		}
		return err
	}
	s.RuntimeID, s.RuntimeARN = aws.ToString(out.AgentRuntimeId), aws.ToString(out.AgentRuntimeArn)
	return saveExecution(filename, *s)
}
func apiCode(err error, code string) bool {
	var a smithy.APIError
	return errors.As(err, &a) && a.ErrorCode() == code
}
func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return nil
	}
}
func (e *Executor) ready(ctx context.Context, s Execution) error {
	for {
		out, err := e.cp().GetAgentRuntime(ctx, &control.GetAgentRuntimeInput{AgentRuntimeId: aws.String(s.RuntimeID)})
		if err != nil {
			return err
		}
		if out.Status == types.AgentRuntimeStatusReady {
			return nil
		}
		if out.Status != types.AgentRuntimeStatusCreating {
			return fmt.Errorf("runtime status %s", out.Status)
		}
		if err := pause(ctx); err != nil {
			return err
		}
	}
}
func (e *Executor) destroy(ctx context.Context, filename string, s *Execution) error {
	if s.Deleted {
		return nil
	}
	if s.Rejected {
		s.Deleted = true
		return saveExecution(filename, *s)
	}
	// Reissue only Create with its persisted idempotency token, never Command.
	// This resolves a lost create response before cleanup, including no-dispatch.
	if err := e.create(ctx, filename, s); err != nil {
		return err
	}
	if !strings.HasPrefix(s.RuntimeID, s.Name+"-") || !strings.HasSuffix(s.RuntimeARN, ":runtime/"+s.RuntimeID) {
		return errors.New("foreign runtime identity")
	}
	e.log("deleting runtime and confirming absence")
	for {
		got, err := e.cp().GetAgentRuntime(ctx, &control.GetAgentRuntimeInput{AgentRuntimeId: aws.String(s.RuntimeID)})
		if apiCode(err, "ResourceNotFoundException") {
			s.Deleted = true
			return saveExecution(filename, *s)
		}
		if err != nil {
			return err
		}
		if got.Status != types.AgentRuntimeStatusDeleting {
			_, err = e.cp().DeleteAgentRuntime(ctx, &control.DeleteAgentRuntimeInput{AgentRuntimeId: aws.String(s.RuntimeID)})
			if err != nil && !apiCode(err, "ResourceNotFoundException") && !apiCode(err, "ConflictException") {
				return err
			}
		}
		if err := pause(ctx); err != nil {
			return err
		}
	}
}

func (e *Executor) load(id string) (Execution, string, error) {
	var s Execution
	filename, err := e.filename(id)
	if err != nil {
		return s, filename, err
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return s, filename, err
	}
	if len(b) > 2<<20 || json.Unmarshal(b, &s) != nil || s.Version != 1 || s.ID != id || s.Name != runtimeName(id) || s.Region != e.Config.Region || s.Image != e.Image || s.Role != e.Role {
		return s, filename, errors.New("invalid or foreign remote execution state")
	}
	return s, filename, nil
}
func (e *Executor) Inspect(ctx context.Context, id string) (sandbox.ExecutionState, error) {
	state := sandbox.ExecutionState{ID: id}
	s, _, err := e.load(id)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if s.Deleted || s.Rejected {
		return state, nil
	}
	state.Exists = true
	state.Running = true // Includes unresolved create; Stop resolves it.
	return state, nil
}
func (e *Executor) Stop(ctx context.Context, id string) error {
	s, filename, err := e.load(id)
	// No AWS request can precede the durable state. The caller owns the dead
	// worker's OS lock, so a missing state is a confirmed pre-dispatch failure.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return e.destroy(ctx, filename, &s)
}

func saveExecution(filename string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".execution-")
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filename); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func saveArchive(root string, message artifact.Message) error {
	if len(message.Archive) > artifact.MaxBytes || int64(len(message.Archive)) != message.Snapshot.Bytes {
		return errors.New("remote artifact size mismatch")
	}
	sum := sha256.Sum256(message.Archive)
	if hex.EncodeToString(sum[:]) != message.Snapshot.SHA256 {
		return errors.New("remote artifact checksum mismatch")
	}
	dir := filepath.Join(root, "snapshots")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".remote-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(message.Archive); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := workspace.VerifySnapshot(f.Name(), message.Snapshot); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, message.Snapshot.SHA256+".tar")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (c Client) Artifact(ctx context.Context, session string, message artifact.Message) (artifact.Message, error) {
	var response artifact.Message
	body, err := json.Marshal(message)
	if err != nil {
		return response, err
	}
	if len(body) > artifact.MaxWireBytes {
		return response, errors.New("artifact request too large")
	}
	out, err := c.api.InvokeAgentRuntime(ctx, &runtime.InvokeAgentRuntimeInput{AgentRuntimeArn: aws.String(c.ARN), RuntimeSessionId: aws.String(session), ContentType: aws.String("application/json"), Payload: body})
	if err != nil {
		return response, err
	}
	defer out.Response.Close()
	b, err := io.ReadAll(io.LimitReader(out.Response, artifact.MaxWireBytes+1))
	if err != nil {
		return response, err
	}
	if len(b) > artifact.MaxWireBytes || aws.ToInt32(out.StatusCode) != 200 {
		return response, fmt.Errorf("artifact transfer failed (status %d)", aws.ToInt32(out.StatusCode))
	}
	if err := json.Unmarshal(b, &response); err != nil {
		return response, err
	}
	return response, nil
}
