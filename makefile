.DEFAULT_GOAL := build

.PHONY: fmt vet build

fmt: 
	@go fmt ./...
	@gofmt -w loadbalancer/loadbalancer.go loadbalancer/loadbalancer_test.go

vet: fmt
	go vet ./...

build: vet
	go build 

test: vet 
	@go test ./loadbalancer -v -count=1

race: 
	go test -race ./loadbalancer -v