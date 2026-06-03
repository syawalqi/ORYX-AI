.PHONY: build clean test

build:
	go build -o flare .

clean:
	rm -f flare

test:
	go test ./...

run: build
	./flare chat
