APP_NAME := git-cascade
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/eukarya-inc/git-cascade/cmd/git-cascade/cmd.Version=$(VERSION)"

.PHONY: build run test lint clean

build:
	go build $(LDFLAGS) -o $(APP_NAME) ./cmd/git-cascade

run: build
	./$(APP_NAME)

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -f $(APP_NAME)
