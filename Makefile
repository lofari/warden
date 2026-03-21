.PHONY: build test test-integration vet clean all

WARDEN_BIN := warden
INIT_BIN := warden-init
NETSETUP_BIN := warden-netsetup

build:
	go build -o $(WARDEN_BIN) ./cmd/warden
	go build -o $(INIT_BIN) ./cmd/warden-init
	go build -o $(NETSETUP_BIN) ./cmd/warden-netsetup

test:
	go test ./...

test-integration:
	go test -tags integration ./tests/

vet:
	go vet ./...
	go vet -tags integration ./...

clean:
	rm -f $(WARDEN_BIN) $(INIT_BIN) $(NETSETUP_BIN)

all: vet test build
