NAMESPACE := cloud-nlb

.PHONY: deploy
deploy:
	kubectl apply -f ./k8s/namespace.yaml

	kubectl apply -f ./k8s/configmap.yaml

	kubectl apply -f ./k8s/api-deployment.yaml
	kubectl apply -f ./k8s/api-service.yaml
	kubectl apply -f ./k8s/api-ingress.yaml

	kubectl apply -f ./k8s/wrk-service.yaml
	kubectl apply -f ./k8s/wrk-statefullset.yaml

.PHONY: delete
delete:
	kubectl delete -f ./k8s/api-deployment.yaml --ignore-not-found
	kubectl delete -f ./k8s/api-service.yaml --ignore-not-found
	kubectl delete -f ./k8s/api-ingress.yaml --ignore-not-found

	kubectl delete -f ./k8s/wrk-statefullset.yaml --ignore-not-found
	kubectl delete -f ./k8s/wrk-service.yaml --ignore-not-found

	kubectl delete -f ./k8s/configmap.yaml --ignore-not-found

.PHONY: build
build:
	colima nerdctl -- --namespace k8s.io build -t hc-api-server:dev . -f ./docker/Dockerfile.api
	colima nerdctl -- --namespace k8s.io build -t hc-wrk:dev . -f ./docker/Dockerfile.wrk