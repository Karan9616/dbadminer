.DEFAULT_GOAL := check

GO_TOOL := go tool -modfile=$(CURDIR)/tools/go.mod
DATABASE_URL ?= postgres://dbadminer:dbadminer@127.0.0.1:5432/dbadminer?sslmode=disable
MODULES := cmd/dbadminer examples/http examples/chi examples/cobra examples/demo

.PHONY: check fmt vet lint test vuln tidy integration ui ui-check

fmt:
	gofmt -w .
	$(GO_TOOL) golangci-lint fmt ./...
	@for dir in $(MODULES); do (cd $$dir && $(GO_TOOL) golangci-lint fmt ./...); done

vet:
	go vet ./...
	@for dir in $(MODULES); do go -C $$dir vet ./...; done

lint:
	$(GO_TOOL) golangci-lint run ./...
	@for dir in $(MODULES); do (cd $$dir && $(GO_TOOL) golangci-lint run ./...); done

test:
	go test -race -count=1 ./...
	go -C cmd/dbadminer test -race -count=1 ./...
	@for dir in examples/http examples/chi examples/cobra; do go -C $$dir test -count=1 ./...; done

vuln:
	$(GO_TOOL) govulncheck ./...
	@for dir in $(MODULES); do go -C $$dir tool -modfile=$(CURDIR)/tools/go.mod govulncheck ./...; done

tidy:
	go mod tidy
	cd tools && go mod tidy
	@for dir in $(MODULES); do (cd $$dir && go mod tidy); done

check: fmt vet lint test vuln

integration:
	DBADMINER_INTEGRATION=1 DATABASE_URL='$(DATABASE_URL)' go -C cmd/dbadminer test -tags integration -count=1 -timeout 20m ./...

ui:
	npm ci --prefix web
	npm run build --prefix web

ui-check:
	npm ci --prefix web
	npm run check --prefix web
	npm run build --prefix web
