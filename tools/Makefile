NAMESPACE := cloud-nlb

.PHONY: deploy
deploy:
	kubectl apply -f ./k8s/test-srv-deployment.yaml

.PHONY: delete
delete:
	kubectl delete -f ./k8s/test-srv-deployment.yaml --ignore-not-found

.PHONY: build
build:
	colima nerdctl -- --namespace k8s.io build -t testsrv:dev . -f ./docker/Dockerfile.testsrv