.PHONY: build

build:
	GOOS=linux go build -ldflags="-s -w" -o handler && zip handler.zip handler
	rm handler