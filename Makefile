.PHONY: build run test vet fmt clean

BINARY := guard

build:
	go build -o bin/$(BINARY) ./cmd/guard

run: build
	./bin/$(BINARY) run --provider mock "诊断 default 命名空间里未就绪的 pod"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin
