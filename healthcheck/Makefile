NAMESPACE := 
APP_NAME := health
IMAGE_TAG := latest
REGISTRY := your-registry

.PHONY: deploy
deploy:
	kubectl apply -f ./statefullset/namespace.yaml
	kubectl apply -f ./statefullset/configmap.yaml
	kubectl apply -f ./statefullset/wrk-service.yaml
	kubectl apply -f ./statefullset/api-deployment.yaml
	kubectl apply -f ./statefullset/statefullset.yaml

.PHONY: delete
delete:
	kubectl delete -f ./statefullset/statefullset.yaml --ignore-not-found
	kubectl delete -f ./statefullset/api-deployment.yaml --ignore-not-found
	kubectl delete -f ./statefullset/wrk-service.yaml --ignore-not-found
	kubectl delete -f ./statefullset/configmap.yaml --ignore-not-found
