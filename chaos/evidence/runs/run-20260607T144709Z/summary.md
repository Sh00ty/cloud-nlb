# Доказательная сводка прогона

- Окно прогона: 5589 с (Prometheus-запрос с буфером ±60 с)
- Шаг выборки: 2 с
- Фаз хаоса: 63

> Разрешение ≈ 2 с (scrape 2 с + окно rate); простой/восстановление короче шага не различимы, числа консервативны сверху. Окно сценария = [старт, конец+30 с], ограничено стартом следующей фазы. Восстановление = время до возврата успеха к ≥95 % дофазового базлайна (0 — деградации не было; «≥» — не восстановился в окне). На стенде data-plane совмещён с nlb-agent: `*-agent-*` бьёт и forwarding — оценка консервативная (верхняя граница относительно прода с VPP).

## Простой и восстановление по сценариям

| Сценарий | Тип | Гр. | Длит. фазы, с | Простой, с | Восстановление, с | backend недост., с | dpl/агент недост., с |
|---|---|:-:|--:|--:|--:|--:|--:|
| chaos-agent-failure | PodChaos | 4 | 5 | 0.0 | 18 | 342 | 360 |
| chaos-hc-kill | PodChaos | 4 | 5 | 0.0 | 18 | 342 | 360 |
| chaos-reconciler-kill | PodChaos | 4 | 5 | 0.0 | 18 | 342 | 360 |
| chaos-server-1-kill | PodChaos | 4 | 5 | 0.0 | 18 | 342 | 360 |
| chaos-hc-gossip-loss | NetworkChaos | 4 | 5 | 0.0 | 18 | 342 | 360 |
| chaos-infra-latency | NetworkChaos | 4 | 1197 | 0.0 | 18 | 342 | 360 |
| chaos-dns-error | DNSChaos | 4 | 0 | 0.0 | 18 | 2 | 32 |
| chaos-agent-failure | PodChaos | 4 | 329 | 0.0 | 18 | 342 | 360 |
| chaos-hc-kill | PodChaos | 4 | 329 | 0.0 | 18 | 342 | 360 |
| chaos-reconciler-kill | PodChaos | 4 | 329 | 0.0 | 18 | 342 | 360 |
| chaos-server-1-kill | PodChaos | 4 | 329 | 0.0 | 18 | 342 | 360 |
| chaos-hc-gossip-loss | NetworkChaos | 4 | 648 | 0.0 | 18 | 342 | 360 |
| chaos-infra-latency | NetworkChaos | 4 | 1176 | 0.0 | 18 | 342 | 360 |
| chaos-dns-error | DNSChaos | 4 | 0 | 0.0 | 18 | 2 | 32 |
| pair-hc-kill-1 | PodChaos | 2 | 173 | 0.0 | 17 | 158 | 2 |
| pair-servers-kill-1 | PodChaos | 2 | 173 | 0.0 | 17 | 158 | 2 |
| pair-agent-kill-1 | PodChaos | 2 | 178 | 0.0 | 14 | 172 | 192 |
| pair-both-servers-kill | PodChaos | 2 | 178 | 0.0 | 14 | 172 | 192 |
| pair-agent-kill-2 | PodChaos | 2 | 178 | 0.0 | 0.0 | 2 | 180 |
| pair-reconciler-kill-1 | PodChaos | 2 | 178 | 0.0 | 0.0 | 2 | 180 |
| pair-hc-kill-2 | PodChaos | 2 | 173 | 0.0 | 0.0 | 0 | 0 |
| pair-hc-packet-loss | NetworkChaos | 2 | 193 | 0.0 | 0.0 | 0 | 0 |
| pair-reconciler-kill-2 | PodChaos | 2 | 0 | 0.0 | ≥203 | 0 | 0 |
| pair-infra-latency-1 | NetworkChaos | 2 | 0 | 0.0 | ≥203 | 0 | 0 |
| single-hc-kill | PodChaos | 1 | 114 | 0.0 | 0.0 | 0 | 0 |
| single-hc-failure-long | PodChaos | 1 | 240 | 0.0 | 0.0 | 0 | 0 |
| single-test-server-kill | PodChaos | 1 | 120 | 0.0 | 0.0 | 0 | 0 |
| single-agent-failure | PodChaos | 1 | 239 | 0.0 | 0.0 | 0 | 240 |
| single-reconciler-kill | PodChaos | 1 | 177 | 0.0 | 0.0 | 0 | 0 |
| single-agent-cpu-stress | StressChaos | 1 | 114 | 0.0 | 0.0 | 0 | 0 |
| single-hc-mem-stress | StressChaos | 1 | 114 | 0.0 | 15 | 0 | 0 |
| single-infra-latency | NetworkChaos | 1 | 177 | 0.0 | 0.0 | 0 | 2 |
| single-infra-partition | NetworkChaos | 1 | 21 | 28.0 | 46 | 10 | 2 |
| server-container-kill-1 | PodChaos | 1 | 11 | 0.0 | 0.0 | 0 | 0 |
| pair-hc-kill-1 | PodChaos | 2 | 177 | 0.0 | 17 | 158 | 2 |
| pair-servers-kill-1 | PodChaos | 2 | 177 | 0.0 | 17 | 158 | 2 |
| pair-agent-kill-1 | PodChaos | 2 | 171 | 0.0 | 14 | 172 | 192 |
| pair-both-servers-kill | PodChaos | 2 | 171 | 0.0 | 14 | 172 | 192 |
| pair-agent-kill-2 | PodChaos | 2 | 177 | 0.0 | 0.0 | 2 | 180 |
| pair-reconciler-kill-1 | PodChaos | 2 | 177 | 0.0 | 0.0 | 2 | 180 |
| pair-hc-kill-2 | PodChaos | 2 | 172 | 0.0 | 0.0 | 0 | 0 |
| pair-hc-packet-loss | NetworkChaos | 2 | 193 | 0.0 | 0.0 | 0 | 0 |
| pair-reconciler-kill-2 | PodChaos | 2 | 177 | 0.0 | ≥203 | 0 | 0 |
| pair-infra-latency-1 | NetworkChaos | 2 | 323 | 0.0 | ≥203 | 0 | 0 |
| pair-servers-kill-2 | PodChaos | 2 | 172 | 0.0 | 17 | 160 | 0 |
| pair-infra-latency-2 | NetworkChaos | 2 | 172 | 0.0 | 17 | 160 | 0 |
| triple-agent-kill-1 | PodChaos | 3 | 234 | 0.0 | 11 | 220 | 240 |
| triple-hc-kill-1 | PodChaos | 3 | 234 | 0.0 | 11 | 220 | 240 |
| triple-servers-kill-1 | PodChaos | 3 | 234 | 0.0 | 11 | 220 | 240 |
| triple-agent-kill-2 | PodChaos | 3 | 234 | 0.0 | 0.0 | 0 | 238 |
| triple-reconciler-kill-1 | PodChaos | 3 | 234 | 0.0 | 0.0 | 0 | 238 |
| triple-infra-latency-1 | NetworkChaos | 3 | 323 | 0.0 | 0.0 | 0 | 238 |
| triple-hc-kill-2 | PodChaos | 3 | 291 | 0.0 | 15 | 218 | 0 |
| triple-servers-kill-2 | PodChaos | 3 | 234 | 0.0 | 15 | 218 | 0 |
| triple-cdc-latency | NetworkChaos | 3 | 234 | 0.0 | 15 | 218 | 0 |
| triple-hc-gossip-loss | NetworkChaos | 3 | 234 | 0.0 | 15 | 218 | 0 |
| chaos-agent-failure | PodChaos | 4 | 355 | 0.0 | 18 | 342 | 360 |
| chaos-hc-kill | PodChaos | 4 | 355 | 0.0 | 18 | 342 | 360 |
| chaos-reconciler-kill | PodChaos | 4 | 355 | 0.0 | 18 | 342 | 360 |
| chaos-server-1-kill | PodChaos | 4 | 355 | 0.0 | 18 | 342 | 360 |
| chaos-hc-gossip-loss | NetworkChaos | 4 | 652 | 0.0 | 18 | 342 | 360 |
| chaos-infra-latency | NetworkChaos | 4 | 652 | 0.0 | 18 | 342 | 360 |
| chaos-dns-error | DNSChaos | 4 | 0 | 0.0 | 18 | 2 | 32 |

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
