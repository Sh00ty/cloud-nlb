---
name: project-runs
description: "История chaos-прогонов: run IDs, статусы, ключевые наблюдения"
metadata:
  type: project
---

## Прогон 1 — run-20260516T144414Z (2026-05-16)

Workflow: nlb-resilience-suite-colima-safe, 39 фаз, ~68 мин.
Результат: одиночные/парные/тройные отказы — 0 с простоя. Простой ≤10 с только при single-infra-partition (etcd) и phase-4 total chaos. CPU/mem-стресс — рост p99 до ~80 мс без простоя. 16 бэкендов держали ~50 rps.
Артефакты: `chaos/evidence/runs/run-20260516T144414Z/`

## Прогон 2 — run-20260517T082127Z (2026-05-17) — В ПРОЦЕССЕ

Workflow: nlb-resilience-suite-colima-safe.
Запущен: 2026-05-17T08:21:27Z (entryNode=main-ppt64).
run_experiment.sh PID: 61008, лог: /tmp/chaos-run-20260517T112127.log
Baseline перед стартом: ~50 rps 2xx, 16 бэкендов, hc: already in sync.
Первая фаза: single-hc-kill-297fz — ~50 rps сохранялся во время инжекта.
Статус: run_experiment.sh ждёт Accomplished=True (deadline 7800s).
Артефакты после: `chaos/evidence/runs/run-20260517T082127Z/`

**Why:** нужно N≥5 повторов для box-plot по сценариям в НИР.
**How to apply:** после завершения run_experiment.sh проверить summary.md и graphs/ в run-20260517T082127Z/.
