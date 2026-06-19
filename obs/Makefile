.PHONY: deploy
deploy:
	kubectl apply -f k8s/00-namespace.yaml
	kubectl apply -f k8s/01-prometheus-configmap.yaml
	kubectl apply -f k8s/02-prometheus-rbac.yaml
	kubectl apply -f k8s/03-prometheus-deployment.yaml
	kubectl apply -f k8s/04-grafana-provisioning.yaml
	kubectl apply -f k8s/05-grafana.yaml

DASHBOARDS_DIR := ./grafana/dashboards
EXTRA_DASHBOARDS := ./grafana/provisioning/dashboards/overview.json

.PHONY: dashboards-sync
dashboards-sync:
	kubectl create configmap grafana-dashboards \
		--namespace=monitoring \
		--from-file=./grafana/dashboards \
		--from-file=overview.json=./grafana/provisioning/dashboards/overview.json \
		--dry-run=client -o yaml | kubectl replace --force -f -
	kubectl rollout restart deployment/grafana -n monitoring