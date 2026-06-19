
.PHONY: start
start:
	kubectl replace --force -f chaos-mesh-workflow.yaml

.PHONY: init-metrics
init-metrics:
	helm upgrade chaos-mesh chaos-mesh/chaos-mesh \
	-n chaos-mesh \
	--reuse-values \
	--set-string controllerManager.podAnnotations."prometheus\.io/scrape"="true" \
	--set-string controllerManager.podAnnotations."prometheus\.io/port"="10080" \
  	--set-string controllerManager.podAnnotations."prometheus\.io/path"="/metrics"