.PHONY: build test vet fmt clean

build:
	go build -o bin/pergent ./cmd/

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/
