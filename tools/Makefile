NAMESPACE := cloud-nlb

.PHONY: deploy
deploy: deploy-srv deploy-clnt

.PHONY: delete
delete: delete-srv delete-clnt

.PHONY: deploy-srv
deploy-srv:
	kubectl apply -f ./k8s/test-srv-deployment.yaml
# 	kubectl apply -f ./k8s/test-srv-deployment-2.yaml

.PHONY: delete-srv
delete-srv:
	kubectl delete -f ./k8s/test-srv-deployment.yaml --ignore-not-found
# 	kubectl delete -f ./k8s/test-srv-deployment-2.yaml --ignore-not-found

.PHONY: deploy-clnt
deploy-clnt:
	kubectl apply -f ./k8s/test-clnt-deployment.yaml
# 	kubectl apply -f ./k8s/test-clnt-deployment-2.yaml

.PHONY: delete-clnt
delete-clnt:
	kubectl delete -f ./k8s/test-clnt-deployment.yaml --ignore-not-found
# 	kubectl delete -f ./k8s/test-clnt-deployment-2.yaml --ignore-not-found

.PHONY: build
build:
	colima nerdctl -- --namespace k8s.io build -t testsrv:dev . -f ./docker/Dockerfile.testsrv
	colima nerdctl -- --namespace k8s.io build -t testclient:dev . -f ./docker/Dockerfile.testclnt