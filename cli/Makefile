.PHONY: build test lint fmt install clean

build:
	go build -o bin/zprof ./cmd/zprof

test:
	go test -race -count=1 ./...

lint:
	go vet ./...
	staticcheck ./... 2>/dev/null || true

fmt:
	gofmt -w .

install:
	go install ./cmd/zprof

clean:
	rm -rf bin/
