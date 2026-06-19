# Доказательная сводка прогона

- Окно прогона: 4385 с (Prometheus-запрос с буфером ±60 с)
- Шаг выборки: 2 с
- Фаз хаоса: 39

> Разрешение ≈ 2 с (scrape 2 с + окно rate); простой/восстановление короче шага не различимы, числа консервативны сверху. Окно сценария = [старт, конец+30 с], ограничено стартом следующей фазы. Восстановление = время до возврата успеха к ≥95 % дофазового базлайна (0 — деградации не было; «≥» — не восстановился в окне). На стенде data-plane совмещён с nlb-agent: `*-agent-*` бьёт и forwarding — оценка консервативная (верхняя граница относительно прода с VPP).

## Простой и восстановление по сценариям

| Сценарий | Тип | Гр. | Длит. фазы, с | Простой, с | Восстановление, с | backend недост., с | dpl/агент недост., с |
|---|---|:-:|--:|--:|--:|--:|--:|
| single-hc-kill | PodChaos | 1 | 115 | 0.0 | 0.0 | 138 | 0 |
| single-hc-failure-long | PodChaos | 1 | 236 | 0.0 | 0.0 | 8 | 0 |
| single-test-server-kill | PodChaos | 1 | 110 | 0.0 | 0.0 | 0 | 0 |
| single-agent-failure | PodChaos | 1 | 237 | 0.0 | 10 | 0 | 240 |
| single-reconciler-kill | PodChaos | 1 | 174 | 0.0 | 0.0 | 0 | 0 |
| single-agent-cpu-stress | StressChaos | 1 | 115 | 0.0 | 0.0 | 0 | 0 |
| single-hc-mem-stress | StressChaos | 1 | 116 | 0.0 | 18 | 0 | 0 |
| single-infra-latency | NetworkChaos | 1 | 174 | 0.0 | 0.0 | 0 | 0 |
| single-infra-partition | NetworkChaos | 1 | 27 | 24.0 | 44 | 8 | 2 |
| server-container-kill-1 | PodChaos | 1 | 10 | 0.0 | 0.0 | 0 | 0 |
| pair-hc-kill-1 | PodChaos | 2 | 179 | 0.0 | 17 | 162 | 0 |
| pair-servers-kill-1 | PodChaos | 2 | 179 | 0.0 | 17 | 162 | 0 |
| pair-agent-kill-1 | PodChaos | 2 | 180 | 0.0 | 17 | 170 | 194 |
| pair-both-servers-kill | PodChaos | 2 | 180 | 0.0 | 17 | 170 | 194 |
| pair-agent-kill-2 | PodChaos | 2 | 179 | 0.0 | 0.0 | 0 | 180 |
| pair-reconciler-kill-1 | PodChaos | 2 | 179 | 0.0 | 0.0 | 0 | 180 |
| pair-hc-kill-2 | PodChaos | 2 | 174 | 0.0 | 0.0 | 0 | 0 |
| pair-hc-packet-loss | NetworkChaos | 2 | 200 | 0.0 | 0.0 | 0 | 0 |
| pair-reconciler-kill-2 | PodChaos | 2 | 173 | 0.0 | 0.0 | 0 | 0 |
| pair-infra-latency-1 | NetworkChaos | 2 | 326 | 0.0 | 0.0 | 0 | 0 |
| pair-servers-kill-2 | PodChaos | 2 | 179 | 0.0 | 20 | 162 | 0 |
| pair-infra-latency-2 | NetworkChaos | 2 | 174 | 0.0 | 20 | 162 | 0 |
| triple-agent-kill-1 | PodChaos | 3 | 232 | 0.0 | 16 | 220 | 240 |
| triple-hc-kill-1 | PodChaos | 3 | 238 | 0.0 | 16 | 220 | 240 |
| triple-servers-kill-1 | PodChaos | 3 | 238 | 0.0 | 16 | 220 | 240 |
| triple-agent-kill-2 | PodChaos | 3 | 237 | 0.0 | 0.0 | 0 | 240 |
| triple-reconciler-kill-1 | PodChaos | 3 | 237 | 0.0 | 0.0 | 0 | 240 |
| triple-infra-latency-1 | NetworkChaos | 3 | 327 | 0.0 | 0.0 | 0 | 240 |
| triple-hc-kill-2 | PodChaos | 3 | 290 | 0.0 | 11 | 218 | 0 |
| triple-servers-kill-2 | PodChaos | 3 | 232 | 0.0 | 11 | 218 | 0 |
| triple-cdc-latency | NetworkChaos | 3 | 232 | 0.0 | 11 | 218 | 0 |
| triple-hc-gossip-loss | NetworkChaos | 3 | 232 | 0.0 | 11 | 218 | 0 |
| chaos-agent-failure | PodChaos | 4 | 353 | 0.0 | 10 | 342 | 358 |
| chaos-hc-kill | PodChaos | 4 | 353 | 0.0 | 10 | 342 | 358 |
| chaos-reconciler-kill | PodChaos | 4 | 353 | 0.0 | 10 | 342 | 358 |
| chaos-server-1-kill | PodChaos | 4 | 353 | 0.0 | 10 | 342 | 358 |
| chaos-hc-gossip-loss | NetworkChaos | 4 | 654 | 0.0 | 10 | 342 | 358 |
| chaos-infra-latency | NetworkChaos | 4 | 654 | 0.0 | 10 | 342 | 358 |
| chaos-dns-error | DNSChaos | 4 | 0 | 0.0 | 10 | 4 | 32 |

## Графики (обзор, заливка по группам фаз)

- `graphs/recovery.png` — Длительность недоступности и восстановления по сценариям
- `graphs/availability_vs_traffic.png` — Недоступность backend / data-plane(агент) vs реакция трафика
- `graphs/throughput.png` — Пропускная способность: успех vs ошибки
- `graphs/latency.png` — Латентность ответа (перцентили)
- `graphs/errors_by_kind.png` — Ошибки по типу
- `graphs/hc_health.png` — Health-check: покрытие и устаревание
- `graphs/backends.png` — Живые бэкенды, принимающие трафик

## Зум по группам фаз (читаемые метки)

- `graphs/zoom/1_phase1_singles/` — Фаза 1: одиночные отказы
- `graphs/zoom/2_phase2_pairs/` — Фаза 2: парные отказы
- `graphs/zoom/3_phase3_triples/` — Фаза 3: тройные отказы
- `graphs/zoom/4_phase4_total/` — Фаза 4: тотальный хаос
