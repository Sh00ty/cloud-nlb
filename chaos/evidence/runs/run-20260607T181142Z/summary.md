# Доказательная сводка прогона

- Окно прогона: 4393 с (Prometheus-запрос с буфером ±60 с)
- Шаг выборки: 2 с
- Фаз хаоса: 39

> Разрешение ≈ 2 с (scrape 2 с + окно rate); простой/восстановление короче шага не различимы, числа консервативны сверху. Окно сценария = [старт, конец+30 с], ограничено стартом следующей фазы. Восстановление = время до возврата успеха к ≥95 % дофазового базлайна (0 — деградации не было; «≥» — не восстановился в окне). На стенде data-plane совмещён с nlb-agent: `*-agent-*` бьёт и forwarding — оценка консервативная (верхняя граница относительно прода с VPP).

## Простой и восстановление по сценариям

| Сценарий | Тип | Гр. | Длит. фазы, с | Простой, с | Восстановление, с | backend недост., с | dpl/агент недост., с |
|---|---|:-:|--:|--:|--:|--:|--:|
| single-hc-kill | PodChaos | 1 | 110 | 0.0 | 0.0 | 138 | 0 |
| single-hc-failure-long | PodChaos | 1 | 234 | 0.0 | 254 | 250 | 0 |
| single-test-server-kill | PodChaos | 1 | 114 | 0.0 | 0.0 | 0 | 4 |
| single-agent-failure | PodChaos | 1 | 235 | 0.0 | 0.0 | 0 | 240 |
| single-reconciler-kill | PodChaos | 1 | 173 | 0.0 | 0.0 | 0 | 0 |
| single-agent-cpu-stress | StressChaos | 1 | 114 | 0.0 | 0.0 | 0 | 0 |
| single-hc-mem-stress | StressChaos | 1 | 115 | 0.0 | 15 | 0 | 0 |
| single-infra-latency | NetworkChaos | 1 | 172 | 0.0 | 0.0 | 0 | 0 |
| single-infra-partition | NetworkChaos | 1 | 26 | 24.0 | ≥52 | 8 | 2 |
| server-container-kill-1 | PodChaos | 1 | 11 | 0.0 | 0.0 | 0 | 0 |
| pair-hc-kill-1 | PodChaos | 2 | 178 | 0.0 | 0.0 | 172 | 0 |
| pair-servers-kill-1 | PodChaos | 2 | 178 | 0.0 | 0.0 | 172 | 0 |
| pair-agent-kill-1 | PodChaos | 2 | 177 | 0.0 | 0.0 | 198 | 198 |
| pair-both-servers-kill | PodChaos | 2 | 177 | 0.0 | 0.0 | 198 | 198 |
| pair-agent-kill-2 | PodChaos | 2 | 172 | 0.0 | 0.0 | 0 | 194 |
| pair-reconciler-kill-1 | PodChaos | 2 | 172 | 0.0 | 0.0 | 0 | 194 |
| pair-hc-kill-2 | PodChaos | 2 | 178 | 0.0 | 0.0 | 0 | 0 |
| pair-hc-packet-loss | NetworkChaos | 2 | 199 | 0.0 | 0.0 | 0 | 0 |
| pair-reconciler-kill-2 | PodChaos | 2 | 177 | 0.0 | ≥203 | 0 | 0 |
| pair-infra-latency-1 | NetworkChaos | 2 | 323 | 0.0 | ≥203 | 0 | 0 |
| pair-servers-kill-2 | PodChaos | 2 | 173 | 0.0 | 18 | 160 | 0 |
| pair-infra-latency-2 | NetworkChaos | 2 | 173 | 0.0 | 18 | 160 | 0 |
| triple-agent-kill-1 | PodChaos | 3 | 235 | 0.0 | 13 | 220 | 242 |
| triple-hc-kill-1 | PodChaos | 3 | 235 | 0.0 | 13 | 220 | 242 |
| triple-servers-kill-1 | PodChaos | 3 | 235 | 0.0 | 13 | 220 | 242 |
| triple-agent-kill-2 | PodChaos | 3 | 235 | 0.0 | 0.0 | 0 | 238 |
| triple-reconciler-kill-1 | PodChaos | 3 | 235 | 0.0 | 0.0 | 0 | 238 |
| triple-infra-latency-1 | NetworkChaos | 3 | 324 | 0.0 | 0.0 | 0 | 238 |
| triple-hc-kill-2 | PodChaos | 3 | 297 | 0.0 | 20 | 218 | 0 |
| triple-servers-kill-2 | PodChaos | 3 | 235 | 0.0 | 20 | 218 | 0 |
| triple-cdc-latency | NetworkChaos | 3 | 235 | 0.0 | 20 | 218 | 0 |
| triple-hc-gossip-loss | NetworkChaos | 3 | 235 | 0.0 | 20 | 218 | 0 |
| chaos-hc-gossip-loss | NetworkChaos | 4 | 652 | 0.0 | 0.0 | 0 | 4 |
| chaos-agent-failure | PodChaos | 4 | 354 | 0.0 | 370 | 340 | 358 |
| chaos-hc-kill | PodChaos | 4 | 354 | 0.0 | 370 | 340 | 358 |
| chaos-reconciler-kill | PodChaos | 4 | 354 | 0.0 | 370 | 340 | 358 |
| chaos-server-1-kill | PodChaos | 4 | 354 | 0.0 | 370 | 340 | 358 |
| chaos-infra-latency | NetworkChaos | 4 | 646 | 0.0 | 370 | 340 | 358 |
| chaos-dns-error | DNSChaos | 4 | 0 | 0.0 | 0.0 | 8 | 32 |

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
