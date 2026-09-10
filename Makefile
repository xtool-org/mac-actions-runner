.PHONY: build
build:
	@mkdir -p out
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o out/tartscaleset ./cmd/tartscaleset

.PHONY: run
run: build
	@./out/tartscaleset

.PHONY: test
test:
	go test -race ./...
