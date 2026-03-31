.PHONY: build test clean

build:
	go build -o bin/pergent ./cmd/

test:
	go test ./...

clean:
	rm -rf bin/
