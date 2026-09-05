// Package agentcore implements the real AWS command protocol. It intentionally
// supports command execution and artifact transport. Executor adds durable
// per-command runtimes and whole-runtime deletion. Commands require the guard.
package agentcore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	runtime "github.com/aws/aws-sdk-go-v2/service/bedrockagentcore"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcore/types"
)

const MaxOutput = 64 << 10

type Client struct {
	api *runtime.Client
	ARN string
}
type Result struct {
	Started   bool   `json:"started"`
	Finished  bool   `json:"finished"`
	Status    string `json:"status"`
	ExitCode  *int32 `json:"exit_code,omitempty"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
}

func New(cfg aws.Config, arn string) Client {
	return Client{api: runtime.NewFromConfig(cfg, func(o *runtime.Options) { o.Retryer = aws.NopRetryer{} }), ARN: arn}
}

func (c Client) Start(ctx context.Context, session string) error {
	out, err := c.api.InvokeAgentRuntime(ctx, &runtime.InvokeAgentRuntimeInput{
		AgentRuntimeArn: aws.String(c.ARN), RuntimeSessionId: aws.String(session),
		ContentType: aws.String("application/json"), Payload: []byte(`{"operation":"health"}`),
	})
	if err != nil {
		return err
	}
	defer out.Response.Close()
	body, err := io.ReadAll(io.LimitReader(out.Response, 4097))
	if err != nil {
		return err
	}
	if len(body) > 4096 || aws.ToInt32(out.StatusCode) != 200 {
		return fmt.Errorf("session initialization failed (status %d)", aws.ToInt32(out.StatusCode))
	}
	return nil
}

func appendBounded(dst *string, src string) bool {
	remaining := MaxOutput - len(*dst)
	if len(src) > remaining {
		*dst += src[:remaining]
		return true
	}
	*dst += src
	return false
}

func (r *Result) consume(chunk types.ResponseChunk) error {
	if r.Finished {
		return errors.New("event after command completion")
	}
	fields := 0
	if chunk.ContentStart != nil {
		fields++
	}
	if chunk.ContentDelta != nil {
		fields++
	}
	if chunk.ContentStop != nil {
		fields++
	}
	if fields != 1 {
		return errors.New("invalid command event shape")
	}
	if chunk.ContentStart != nil {
		if r.Started {
			return errors.New("duplicate command start")
		}
		r.Started = true
	} else if !r.Started {
		return errors.New("command event before start")
	}
	if d := chunk.ContentDelta; d != nil {
		r.Truncated = appendBounded(&r.Stdout, aws.ToString(d.Stdout)) || r.Truncated
		r.Truncated = appendBounded(&r.Stderr, aws.ToString(d.Stderr)) || r.Truncated
	}
	if s := chunk.ContentStop; s != nil {
		if s.ExitCode == nil || (s.Status != types.CommandExecutionStatusCompleted && s.Status != types.CommandExecutionStatusTimedOut) {
			return errors.New("missing command outcome")
		}
		r.Finished = true
		r.ExitCode = s.ExitCode
		r.Status = string(s.Status)
	}
	return nil
}

// GuardedCommand is the only command path exposed by this client. A missing
// launcher or unsupported kernel fails the command; there is no raw fallback.
func GuardedCommand(profile, script string) (string, error) {
	if profile != "work" && profile != "verify" {
		return "", errors.New("invalid command isolation profile")
	}
	return "/usr/local/bin/harness-guard " + profile + " -- '" + strings.ReplaceAll(script, "'", "'\"'\"'") + "'", nil
}
func (c Client) Command(ctx context.Context, session, script string, timeoutSeconds int32) (Result, error) {
	return c.CommandProfile(ctx, session, script, "work", timeoutSeconds)
}
func (c Client) CommandProfile(ctx context.Context, session, script, profile string, timeoutSeconds int32) (Result, error) {
	return c.commandObserved(ctx, session, script, profile, timeoutSeconds, nil)
}
func (c Client) commandObserved(ctx context.Context, session, script, profile string, timeoutSeconds int32, onStart func() error) (Result, error) {
	command, err := GuardedCommand(profile, script)
	if err != nil {
		return Result{}, err
	}
	return c.invokeCommand(ctx, session, command, timeoutSeconds, onStart)
}

func (c Client) invokeCommand(ctx context.Context, session, command string, timeoutSeconds int32, onStart func() error) (result Result, err error) {
	if len(session) < 33 || len(session) > 256 || len(command) == 0 || len(command) > 64<<10 || timeoutSeconds < 1 || timeoutSeconds > 3600 {
		return result, errors.New("invalid command request bounds")
	}
	out, err := c.api.InvokeAgentRuntimeCommand(ctx, &runtime.InvokeAgentRuntimeCommandInput{
		AgentRuntimeArn: aws.String(c.ARN), RuntimeSessionId: aws.String(session),
		ContentType: aws.String("application/json"), Accept: aws.String("application/vnd.amazon.eventstream"),
		Body: &types.InvokeAgentRuntimeCommandRequestBody{Command: aws.String(command), Timeout: aws.Int32(timeoutSeconds)},
	})
	if err != nil {
		return result, err
	}
	stream := out.GetStream()
	if stream == nil {
		return result, errors.New("missing command stream; outcome unknown")
	}
	defer stream.Close()
	for event := range stream.Events() {
		chunk, ok := event.(*types.InvokeAgentRuntimeCommandStreamOutputMemberChunk)
		if !ok {
			return result, errors.New("unknown command stream event")
		}
		if err := result.consume(chunk.Value); err != nil {
			return result, err
		}
		if chunk.Value.ContentStart != nil && onStart != nil {
			if err := onStart(); err != nil {
				return result, err
			}
		}
	}
	if err := stream.Err(); err != nil {
		return result, err
	}
	if !result.Finished {
		return result, errors.New("stream closed without command outcome; effects unknown")
	}
	return result, nil
}

// Stop acknowledges a request. It does not claim the microVM is absent: AWS
// exposes no session-inspection operation that proves that fact here.
func (c Client) Stop(ctx context.Context, session string) error {
	out, err := c.api.StopRuntimeSession(ctx, &runtime.StopRuntimeSessionInput{AgentRuntimeArn: aws.String(c.ARN), RuntimeSessionId: aws.String(session)})
	if err != nil {
		return err
	}
	if aws.ToInt32(out.StatusCode) < 200 || aws.ToInt32(out.StatusCode) >= 300 {
		return fmt.Errorf("stop not acknowledged: %d", aws.ToInt32(out.StatusCode))
	}
	return nil
}
