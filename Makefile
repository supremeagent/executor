run:
	go run cmd/server/main.go

test:
	go test -cover -v ./...

.PHONY: run test
