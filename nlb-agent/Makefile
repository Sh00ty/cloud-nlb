NAMESPACE := cloud-nlb

.PHONY: deploy
deploy:
	kubectl apply -f ./k8s/namespace.yaml

	kubectl apply -f ./k8s/configmap.yaml

	kubectl apply -f ./k8s/agent-service.yaml
	kubectl apply -f ./k8s/agent-statefullset.yaml

.PHONY: delete
delete:
	kubectl delete -f ./k8s/agent-statefullset.yaml --ignore-not-found
	kubectl delete -f ./k8s/agent-service.yaml --ignore-not-found

	kubectl delete -f ./k8s/configmap.yaml --ignore-not-found

.PHONY: build
build:
	colima nerdctl -- --namespace k8s.io build -t nlb-agent:dev . -f ./docker/Dockerfile.agent