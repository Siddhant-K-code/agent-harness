package agentcore

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcore/types"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandEvents(t *testing.T) {
	var r Result
	if err := r.consume(types.ResponseChunk{ContentDelta: &types.ContentDeltaEvent{Stdout: aws.String("early")}}); err == nil {
		t.Fatal("accepted output before start")
	}
	if err := r.consume(types.ResponseChunk{ContentStart: &types.ContentStartEvent{}}); err != nil {
		t.Fatal(err)
	}
	if err := r.consume(types.ResponseChunk{ContentDelta: &types.ContentDeltaEvent{Stdout: aws.String(strings.Repeat("x", MaxOutput+10)), Stderr: aws.String("err")}}); err != nil {
		t.Fatal(err)
	}
	if !r.Truncated || len(r.Stdout) != MaxOutput || r.Stderr != "err" {
		t.Fatalf("unbounded or lost output: %+v", r)
	}
	if err := r.consume(types.ResponseChunk{ContentStop: &types.ContentStopEvent{ExitCode: aws.Int32(7), Status: types.CommandExecutionStatusCompleted}}); err != nil {
		t.Fatal(err)
	}
	if !r.Finished || *r.ExitCode != 7 {
		t.Fatal("lost actual nonzero exit")
	}
	if err := r.consume(types.ResponseChunk{ContentStart: &types.ContentStartEvent{}}); err == nil {
		t.Fatal("accepted event after stop")
	}
}

func TestMissingOutcomeRejected(t *testing.T) {
	r := Result{Started: true}
	if err := r.consume(types.ResponseChunk{ContentStop: &types.ContentStopEvent{Status: types.CommandExecutionStatusCompleted}}); err == nil {
		t.Fatal("invented missing exit code")
	}
}

func TestGuardedCommandCannotEscapeArgument(t *testing.T) {
	for _, script := range []string{"echo hi", "echo 'hello'; $(printf injection)", "line one\nline two", "'; exit 99; #"} {
		wrapped, err := GuardedCommand("verify", script)
		if err != nil {
			t.Fatal(err)
		}
		// Real shell parsing must deliver exactly one script argument to the launcher.
		out, err := exec.Command("/bin/sh", "-c", "set -- "+wrapped+"; test \"$#\" -eq 4 && test \"$2\" = verify && printf %s \"$4\"").Output()
		if err != nil || string(out) != script {
			t.Fatalf("unsafe command quoting: %q %v", out, err)
		}
	}
	if _, err := GuardedCommand("verify; exit 0", "ignored"); err == nil {
		t.Fatal("accepted arbitrary profile")
	}
}
