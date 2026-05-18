.PHONY: all clean build test vet run-web \
        build-fulltext build-ocr build-prepare-pipeline \
        build-ocr-validation-web build-ocr-users

# Default target builds all binaries
all: build

# Build all commands
build: build-fulltext build-ocr build-prepare-pipeline \
       build-ocr-validation-web build-ocr-users

# Build run-fulltext command
build-fulltext:
	go build -o run-fulltext ./cmd/run-fulltext

# Build run-ocr command
build-ocr:
	go build -o run-ocr ./cmd/run-ocr

# Build prepare-document-pipeline command
build-prepare-pipeline:
	go build -o prepare-document-pipeline ./cmd/prepare-document-pipeline

# --- OCR-validation toolkit ---

build-ocr-validation-web:
	go build -o ocr-validation-web ./cmd/ocr-validation-web

build-ocr-users:
	go build -o ocr-users ./cmd/ocr-users

# Run the web server with debug logging
run-web: build-ocr-validation-web
	./ocr-validation-web -debug

# --- Test / vet ---

test:
	go test -count=1 ./...

vet:
	go vet ./...

# --- Clean ---

clean:
	rm -f run-fulltext run-ocr prepare-document-pipeline \
	      ocr-validation-web ocr-users
