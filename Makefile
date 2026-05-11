.PHONY: build
build:
	go build -o mackerel-plugin-tidb-process-list

.PHONY: test
test:
	go test -v ./...

.PHONY: lint
lint:
	gofmt -w .
