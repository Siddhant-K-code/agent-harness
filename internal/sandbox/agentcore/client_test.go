package agentcore

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcore/types"
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
