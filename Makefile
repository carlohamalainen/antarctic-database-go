.PHONY: all clean build install test

# Default target builds all binaries
all: build

# Build all commands
build: build-fulltext build-ocr build-prepare-pipeline

# Build run-fulltext command
build-fulltext:
	go build -o run-fulltext ./cmd/run-fulltext

# Build run-ocr command
build-ocr:
	go build -o run-ocr ./cmd/run-ocr

# Build prepare-document-pipeline command
build-prepare-pipeline:
	go build -o prepare-document-pipeline ./cmd/prepare-document-pipeline

# Clean build artifacts
clean:
	rm -f run-fulltext run-ocr prepare-document-pipeline

# Install binaries to $GOPATH/bin
# install:
# 	go install ./cmd/run-fulltext
# 	go install ./cmd/run-ocr
# 	go install ./cmd/prepare-document-pipeline

# Run tests
test:
	go test -v ./...