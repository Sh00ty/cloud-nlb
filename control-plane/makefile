NAMESPACE := cloud-nlb

.PHONY: deploy
deploy:
	kubectl apply -f ./k8s/namespace.yaml
	kubectl apply -f ./k8s/configmap.yaml

	kubectl apply -f ./k8s/api-service.yaml
	kubectl apply -f ./k8s/api-ingress.yaml
	kubectl apply -f ./k8s/api-deployment.yaml

	kubectl apply -f ./k8s/reconciler-deployment.yaml

.PHONY: delete
delete:
	kubectl delete -f ./k8s/reconciler-deployment.yaml --ignore-not-found

	kubectl delete -f ./k8s/api-ingress.yaml --ignore-not-found
	kubectl delete -f ./k8s/api-service.yaml --ignore-not-found
	kubectl delete -f ./k8s/api-deployment.yaml --ignore-not-found

	kubectl delete -f ./k8s/configmap.yaml --ignore-not-found

.PHONY: restart
restart:
	kubectl rollout restart deployment/control-plane-apiserver -n $(NAMESPACE)
	kubectl rollout restart deployment/control-plane-reconciler -n $(NAMESPACE)

.PHONY: build
build:
	colima nerdctl -- --namespace k8s.io build -t control-plane-apiserver:dev . -f ./docker/Dockerfile.api
	colima nerdctl -- --namespace k8s.io build -t control-plane-reconciler:dev . -f ./docker/Dockerfile.reconciler