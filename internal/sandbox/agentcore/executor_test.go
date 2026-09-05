package agentcore

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AWS protocol test: a denial on recovery cannot erase an earlier uncertain
// create. This stub is not an executor or a substitute for the live AWS tests.
func TestCreateDenialPreservesEarlierUncertainty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amzn-Errortype", "AccessDeniedException")
		w.WriteHeader(403)
		w.Write([]byte(`{"message":"test denial"}`))
	}))
	defer server.Close()
	cfg := aws.Config{Region: "us-east-1", BaseEndpoint: aws.String(server.URL), Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
	}), Retryer: func() aws.Retryer { return aws.NopRetryer{} }}
	e := Executor{Config: cfg}
	for _, attempts := range []int{0, 1} {
		id := "agent-harness-" + strings.Repeat("a", 32) + "-1"
		s := Execution{Version: 1, ID: id, Name: runtimeName(id), Role: "arn:aws:iam::123456789012:role/test", Image: "test", CreateAttempts: attempts}
		if err := e.create(context.Background(), filepath.Join(t.TempDir(), "execution.json"), &s); err == nil {
			t.Fatal("accepted denied create")
		}
		if s.Rejected != (attempts == 0) {
			t.Fatal("denial erased earlier uncertain create")
		}
	}
}

func TestAbsentPreDispatchStateIsSafeAndForeignStateRefused(t *testing.T) {
	root := t.TempDir()
	e := Executor{Root: root}
	id := "agent-harness-" + strings.Repeat("b", 32) + "-1"
	if err := e.Stop(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := e.Stop(context.Background(), "../../other"); err == nil {
		t.Fatal("accepted invalid execution id")
	}
	filename, _ := e.filename(id)
	if err := saveExecution(filename, Execution{Version: 1, ID: id, Name: "foreign"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Stop(context.Background(), id); err == nil {
		t.Fatal("accepted foreign runtime record")
	}
	if _, err := os.Stat(filename); err != nil {
		t.Fatal("destroyed evidence")
	}
}
