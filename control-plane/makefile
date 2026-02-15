NAMESPACE := cloud-nlb
APP_NAME := control-plane
IMAGE_TAG := latest
REGISTRY := your-registry

.PHONY: deploy
deploy:
	kubectl apply -f ./deployment/namespace.yaml
	kubectl apply -f ./deployment/configmap.yaml
	kubectl apply -f ./deployment/service.yaml
	kubectl apply -f ./deployment/deployment.yaml
	kubectl apply -f ./deployment/control-plane-ingress.yaml

.PHONY: delete
delete:
	kubectl delete -f ./deployment/control-plane-ingress.yaml --ignore-not-found
	kubectl delete -f ./deployment/deployment.yaml --ignore-not-found
	kubectl delete -f ./deployment/service.yaml --ignore-not-found
	kubectl delete -f ./deployment/configmap.yaml --ignore-not-found

.PHONY: restart
restart:
	kubectl rollout restart deployment/$(APP_NAME) -n $(NAMESPACE)

.PHONY: logs
logs:
	kubectl logs -f -n $(NAMESPACE) -l app=$(APP_NAME) --tail=100

.PHONY: status
status:
	kubectl get pods,svc,ingress -n $(NAMESPACE) -l app=$(APP_NAME)

.PHONY: test-grpc
test-grpc:
	@echo "Testing gRPC endpoint..."
	grpcurl -plaintext control-plane.cloud-nlb.svc.cluster.local:50051 list