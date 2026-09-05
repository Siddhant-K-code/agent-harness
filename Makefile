.PHONY: build test integration
build:
	go build -o bin/harness ./cmd/harness
test:
	go test -race ./...
integration:
	HARNESS_DOCKER_TEST=1 go test -count=1 ./internal/sandbox ./internal/runner
