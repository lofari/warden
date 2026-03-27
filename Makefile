.PHONY: build test test-integration vet clean all

WARDEN_BIN := warden
INIT_BIN := warden-init
NETSETUP_BIN := warden-netsetup
SHIM_BIN := warden-shim
BRIDGE_BIN := warden-bridge

build:
	go build -o $(WARDEN_BIN) ./cmd/warden
	go build -o $(INIT_BIN) ./cmd/warden-init
	go build -o $(NETSETUP_BIN) ./cmd/warden-netsetup
	CGO_ENABLED=0 go build -o $(SHIM_BIN) ./cmd/warden-shim
	CGO_ENABLED=0 go build -o $(BRIDGE_BIN) ./cmd/warden-bridge

test:
	go test ./...

test-integration:
	go test -tags integration ./tests/

vet:
	go vet ./...
	go vet -tags integration ./...

clean:
	rm -f $(WARDEN_BIN) $(INIT_BIN) $(NETSETUP_BIN) $(SHIM_BIN) $(BRIDGE_BIN)

all: vet test build
