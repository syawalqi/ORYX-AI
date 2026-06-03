.PHONY: build clean test run fmt

build:
	go build -o flare .

clean:
	rm -f flare

test:
	go test ./...

run: build
	./flare chat

fmt:
	go fmt ./...
