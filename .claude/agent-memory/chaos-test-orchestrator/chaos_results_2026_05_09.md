---
name: Результаты хаос-прогона 2026-05-09
description: Количественные результаты, ключевые инсайты и артефакты полного chaos-workflow прогона от 2026-05-09
type: project
---

Прогон `nlb-resilience-suite-colima-safe` запущен 2026-05-09 19:53:56 UTC.
Базовая линия: RPS=50, ERR=0, p50=0.65ms, p95=4.3ms, p99=9.8ms.

## Фаза 1 — одиночные отказы (выполнено 8 из 10 шагов, остальные срезаны DeadlineExceed)

| Сценарий | Старт | ERR пик | p99 пик | Итог |
|---|---|---|---|---|
| single-hc-kill (pod-kill 75% hc-worker, 2m) | 19:54:03 | ~1.0/s (15s) | 15ms | Прошёл |
| single-hc-failure-long (pod-failure 75%, 4m) | 19:56:48 | 0 | 17ms | Прошёл |
| single-test-server-kill (container-kill 75%, 2m) | 20:01:33 | 0.84/s (15s) | 18ms | Прошёл |
| single-agent-failure (pod-failure 50%, 4m) | 20:04:18 | 1.36/s (30s) | 15ms | Прошёл |
| single-reconciler-kill (pod-kill 1, 3m) | 20:09:03 | 0 | 23ms | Прошёл |
| single-agent-cpu-stress (60% CPU all, 2m) | 20:12:48 | 0 | **87ms (+5x)** | Прошёл — критично |
| single-hc-mem-stress (256MB, ~30s срезан) | 20:15:33 | 1.88/s (30s) | 16ms | Прерван deadline |
| single-infra-latency | — | — | — | Пропущена (DeadlineExceed) |
| single-infra-partition | — | — | — | Пропущена (DeadlineExceed) |
| server-flap-test | — | — | — | Пропущена (DeadlineExceed) |

**Ключевой инсайт фазы 1:** CPU-стресс на агентах — единственный сценарий, дающий значимый латентный эффект (+5x p99) без потери трафика. Все pod-failure/kill дают кратковременный ERR-пик 15–30s с последующим возвратом к 0.

## Фаза 2 — парные отказы (в процессе на момент записи)

| Сценарий | Старт | ERR пик | p99 | Итог |
|---|---|---|---|---|
| pair-hc-plus-servers (75% hc + 50% test-server, 3m) | 20:18:02 | ~1.68/s кратко | ~10ms | Прошёл |
| pair-agent-plus-servers (50% agent + 50% test-server, 3m) | 20:21:47 | 0 | 11–15ms | Прошёл |
| pair-agent-plus-reconciler (50% agent + reconciler, 3m) | 20:25:42 | 0 | 15ms | В процессе |

## Инсайты о восстановлении

- StatefulSet (hc-worker) пересоздаёт поды за ~10s после pod-kill
- pod-failure через chaos-mesh инжектирует `runtime: failed to create new OS thread (errno=22)` в Go runtime
- Система возвращается в ERR=0 за 10–30s после снятия любого pod-failure
- Трафик не прерывается ни при одном одиночном отказе в установившемся режиме

## Workflow: что исправлено в yaml (2026-05-09)

- `cooldown-short`: 45s → 20s (реальное время восстановления 10–15s)
- `cooldown-long`: 2m → 45s
- `main` deadline: 75m → 95m
- `phase-1-singles` deadline: 22m → 35m (чтобы не срезались infra-сценарии)
- `phase-2-pairs` deadline: 28m → 23m
- `phase-3-triples` deadline: 22m → 20m

## Файлы артефактов

- `nir_text/section_8_testing.md` — обновлён §8.3 (кулдауны), §8.6 (таблица + абзац о CPU-стрессе)
- `chaos/chaos-mesh-workflow.yaml` — обновлены deadlines и cooldowns

## Prometheus queries (проверены, работают)

- `sum(rate(testclient_requests_total[30s])) by (code)` — RPS по кодам
- `sum(rate(testclient_request_errors_total[30s])) by (kind)` — ERR по типам
- `histogram_quantile(0.99, sum(rate(testclient_request_duration_seconds_bucket[30s])) by (le))` — p99
- `sum(rate(testclient_requests_total[1m])) by (served_by)` — распределение по бэкендам
- Prometheus доступен через port-forward: `kubectl port-forward -n monitoring svc/prometheus 9091:9090`

**Why:** Нужно для быстрого восстановления контекста в следующих сессиях.
**How to apply:** При старте новой сессии — читать этот файл, проверять статус workflow через `kubectl get workflownodes -n cloud-nlb`.
