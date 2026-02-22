
test:
	go test -cover -v ./...

run:
	go run cmd/server/main.go

playground:
	cd playground && pnpm install
	cd playground && pnpm run dev

.PHONY: run test playground
