TEST?=$$(go list ./... | grep -v 'vendor')
HOSTNAME=registry.terraform.io
NAMESPACE=mozilla
NAME=anthropic
BINARY=terraform-provider-${NAME}
VERSION=0.1.0
OS_ARCH?=$$(go env GOOS)_$$(go env GOARCH)

default: install

build:
	go build -v -o ${BINARY}

install: build
	mkdir -p ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}
	mv ${BINARY} ~/.terraform.d/plugins/${HOSTNAME}/${NAMESPACE}/${NAME}/${VERSION}/${OS_ARCH}

test:
	go test -v -cover -timeout=120s -parallel=4 ./...

testacc:
	TF_ACC=1 go test -v -cover $(TEST) -timeout 120m

vet:
	go vet ./...

fmt:
	gofmt -s -w -e .

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name anthropic

.PHONY: build install test testacc vet fmt docs
