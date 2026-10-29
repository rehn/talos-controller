.PHONY: dev-backend dev-frontend build build-arm64 test

# Run the Go API locally against your cluster (UI served by `make dev-frontend`).
dev-backend:
	cd backend && TALOSCONFIG=$${TALOSCONFIG:-$$HOME/.talos/config} KUBECONFIG=$${KUBECONFIG:-$$HOME/.kube/config} \
		ADMIN_PASSWORD=$${ADMIN_PASSWORD:-admin} DATA_DIR=../data go run .

# Next.js dev server on :3000, proxying /api to the Go backend on :8080.
dev-frontend:
	cd frontend && npm install && npm run dev

test:
	cd backend && go test ./...

build:
	docker compose build

# Cross-build an arm64 image on a faster machine, then: docker save talos-controller | ssh pi docker load
build-arm64:
	docker buildx build --platform linux/arm64 -t talos-controller:latest --load .
