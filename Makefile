.PHONY: build clean test run fmt

build:
	go build -o oryx .

clean:
	rm -f oryx

test:
	go test ./...

run: build
	./oryx chat

fmt:
	go fmt ./...
