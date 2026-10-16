.PHONY: all generate clean

# Check for required tools
PROTOC := $(shell command -v protoc 2> /dev/null)
GO := $(shell command -v go 2> /dev/null)

all: generate

generate:
ifndef PROTOC
	$(error "protoc is not installed. Please install Protocol Buffers compiler")
endif
ifndef GO
	$(error "go is not installed. Please install Go")
endif
	@echo "Generating Go code from Protocol Buffers..."
	@mkdir -p pkg/proto/racehub
	protoc --go_out=pkg/proto/racehub \
		--go_opt=paths=source_relative \
		--proto_path=. \
		racehub.proto
	@echo "Generation complete"

clean:
	@echo "Cleaning generated files..."
	rm -f pkg/proto/racehub/*.go
	@echo "Clean complete"