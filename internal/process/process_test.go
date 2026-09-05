package process

import (
	"context"
	"testing"
)

func TestOutputBoundAndExitCode(t *testing.T) {
	r, err := Run(context.Background(), "", nil, nil, 16, "sh", "-c", "printf '123456789012345678901234567890'; exit 7")
	if err != nil {
		t.Fatal(err)
	}
	if r.ExitCode != 7 || len(r.Output) != 16 || !r.Truncated {
		t.Fatalf("bad result: %+v", r)
	}
}
