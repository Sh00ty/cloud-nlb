COLIMA_IP := $(shell colima list -j 2>/dev/null | jq -r '.address // empty')
TRAEFIK_PORT := 30080
HOSTS := grafana.local control-plane-apiserver.local hc-server.local

.PHONY: set-up-traefik
set-up-traefik:
	helm repo add traefik https://traefik.github.io/charts 2>/dev/null || true
	helm repo update
	helm install traefik traefik/traefik \
		--namespace kube-system \
		--set service.type=NodePort \
		--set ports.web.nodePort=30080 \
		--set ports.web.expose.default=true \
		--set ports.websecure.nodePort=30443 \
		--set ports.websecure.expose.default=true \
		2>/dev/null || helm upgrade traefik traefik/traefik \
		--namespace kube-system \
		--set service.type=NodePort \
		--set ports.web.nodePort=30080 \
		--set ports.web.expose.default=true \
		--set ports.websecure.nodePort=30443 \
		--set ports.websecure.expose.default=true
		--set "additionalArguments[0]=--entrypoints.web.http2.maxConcurrentStreams=250"

.PHONY: infra
infra:
	docker run -d --name etcd -p 2379:2379 -e ALLOW_NONE_AUTHENTICATION=yes bitnamilegacy/etcd
	cd ./healthcheck && docker-compose up -d


.PHONY: chaos
chaos:
	kubectl create namespace chaos-mesh

	helm install chaos-mesh chaos-mesh/chaos-mesh \
	--namespace chaos-mesh \
	--set chaosDaemon.runtime=containerd \
	--set chaosDaemon.socketPath=/run/containerd/containerd.sock \
	--set dashboard.securityMode=false

	kubectl create clusterrolebinding chaos-controller-manager-cluster-view \
		--clusterrole=cluster-admin \
		--serviceaccount=chaos-mesh:chaos-controller-manager

.PHONY: hosts-update
hosts-update:
	@if [ -z "$(COLIMA_IP)" ]; then echo "ERROR: colima not running"; exit 1; fi
	@for h in $(HOSTS); do \
		sudo sed -i '' "/$$h/d" /etc/hosts; \
	done
	@echo "$(COLIMA_IP)  $(HOSTS)" | sudo tee -a /etc/hosts
	@echo "Updated /etc/hosts with $(COLIMA_IP)"


.PHONY: build
build: build-hc build-cp build-tools build-agent

.PHONY: build-hc
build-hc:
	cd healthcheck && make build

.PHONY: build-cp
build-cp:
	cd control-plane && make build

.PHONY: build-agent
build-agent:
	cd nlb-agent && make build

.PHONY: build-tools
build-tools:
	cd tools && make build && cd ..

.PHONY: deploy
deploy: deploy-obs deploy-hc deploy-cp deploy-agent deploy-tools
	@echo "All deployed!"
	@$(MAKE) status

.PHONY: deploy-obs
deploy-obs:
	cd obs && make deploy && make dashboards-sync

.PHONY: deploy-hc
deploy-hc:
	cd healthcheck && make deploy

.PHONY: deploy-cp
deploy-cp:
	cd control-plane && make deploy

.PHONY: deploy-agent
deploy-agent:
	cd nlb-agent && make deploy

.PHONY: deploy-tools
deploy-tools:
	cd tools && make deploy

.PHONY: status
status:
	@echo "\n=== Nodes ==="
	kubectl get nodes
	@echo "\n=== Monitoring ==="
	kubectl get pods -n monitoring
	@echo "\n=== Cloud NLB ==="
	kubectl get pods -n cloud-nlb
	@echo "\n=== Ingress ==="
	kubectl get ingress -A
	kubectl get ingressroute -A 2>/dev/null || true

.PHONY: clean
clean:
	cd control-plane && make delete
	cd healthcheck && make delete
	cd nlb-agent && make delete
	cd tools && make delete