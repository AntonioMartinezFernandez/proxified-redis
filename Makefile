.PHONY: run-cluster
run-cluster:
	@echo "Creating cluster..."
	kind create cluster --name proxified-redis --config ./kind-cluster.yaml

.PHONY: delete-cluster
delete-cluster:
	@echo "Destroying cluster..."
	kind delete cluster --name proxified-redis

.PHONY: skaffold
skaffold:
	skaffold dev  --wait-for-deletions=false  --cleanup=false

.PHONY: build
build:
	go build -o bin/proxified-redis-app ./cmd/proxified-redis-app/main.go

.PHONY: run
run:
	go run ./cmd/proxified-redis-app/main.go
