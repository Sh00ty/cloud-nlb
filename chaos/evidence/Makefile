HERE := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
PY    := $(HERE).venv/bin/python

.PHONY: setup dryrun run clean-runs aggregate aggregate-ts resilience \
        backfill-hc hc-coverage

# One-time: isolated venv pinned for НИР reproducibility.
setup:
	python3 -m venv $(HERE).venv
	$(PY) -m pip install --upgrade pip >/dev/null
	$(PY) -m pip install -r $(HERE)requirements.txt
	chmod +x $(HERE)run_experiment.sh $(HERE)poll_timeline.sh
	@echo "evidence pipeline ready"

# ~2-3 min end-to-end pipeline validation.
dryrun:
	$(HERE)run_experiment.sh $(HERE)dryrun-workflow.yaml

# Full suite run (95m workflow). Best launched in background.
run:
	$(HERE)run_experiment.sh

# Cross-run box-plots + aggregate table from all frozen runs.
aggregate:
	$(PY) $(HERE)aggregate.py

# Existing consolidated time-series figures (HC main/summary, outage, ...).
aggregate-ts:
	$(PY) $(HERE)aggregate_timeseries.py

# Client-SLI resilience view -> aggregate/v2/ (additive, leaves the figures
# above untouched): recovery_when_impacted, client_errors_aligned,
# hc_coverage_when_impacted, recovery_summary.md.
resilience:
	$(PY) $(HERE)aggregate_resilience.py

# Freeze the live hc-worker count into already-frozen runs (needs the chaos
# stand reachable; valid only within Prometheus 7d retention). ADDITIVE:
# writes only metrics/avail__live_hc.json per run.
backfill-hc:
	$(PY) $(HERE)backfill_hc_up.py

# HC probe-coverage interval vs how many HC nodes were down -> aggregate/v2/
# hc_coverage_vs_health.{png,md} (additive). Run backfill-hc first for old
# runs; new runs already carry live_hc.
hc-coverage:
	$(PY) $(HERE)hc_coverage_vs_health.py

clean-runs:
	rm -rf $(HERE)runs/*
