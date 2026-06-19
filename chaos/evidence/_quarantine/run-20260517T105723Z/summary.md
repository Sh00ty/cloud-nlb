# Доказательная сводка прогона

- Окно прогона: 1606 с (Prometheus-запрос с буфером ±60 с)
- Шаг выборки: 10 с
- Фаз хаоса: 30

> Разрешение ≈ 10 с (scrape 5 с + окно rate); простой/восстановление короче шага не различимы, числа консервативны сверху. Окно сценария = [старт, конец+30 с], ограничено стартом следующей фазы. Восстановление = время до возврата успеха к ≥95 % дофазового базлайна (0 — деградации не было; «≥» — не восстановился в окне). На стенде data-plane совмещён с nlb-agent: `*-agent-*` бьёт и forwarding — оценка консервативная (верхняя граница относительно прода с VPP).

## Простой и восстановление по сценариям

| Сценарий | Тип | Гр. | Длит. фазы, с | Простой, с | Восстановление, с | backend недост., с | dpl/агент недост., с |
|---|---|:-:|--:|--:|--:|--:|--:|
| chaos-agent-failure | PodChaos | 4 | 5 | 0.0 | 44 | 320 | 350 |
| chaos-hc-kill | PodChaos | 4 | 5 | 0.0 | 44 | 320 | 350 |
| chaos-reconciler-kill | PodChaos | 4 | 5 | 0.0 | 44 | 320 | 350 |
| chaos-server-1-kill | PodChaos | 4 | 5 | 0.0 | 44 | 320 | 350 |
| chaos-hc-gossip-loss | NetworkChaos | 4 | 591 | 0.0 | 44 | 320 | 350 |
| chaos-infra-latency | NetworkChaos | 4 | 643 | 0.0 | 44 | 320 | 350 |
| chaos-dns-error | DNSChaos | 4 | 0 | 0.0 | ≥30 | 0 | 30 |
| chaos-agent-failure | PodChaos | 4 | 343 | 0.0 | 44 | 320 | 350 |
| chaos-hc-kill | PodChaos | 4 | 343 | 0.0 | 44 | 320 | 350 |
| chaos-reconciler-kill | PodChaos | 4 | 343 | 0.0 | 44 | 320 | 350 |
| chaos-server-1-kill | PodChaos | 4 | 343 | 0.0 | 44 | 320 | 350 |
| chaos-hc-gossip-loss | NetworkChaos | 4 | 655 | 0.0 | 44 | 320 | 350 |
| chaos-infra-latency | NetworkChaos | 4 | 1190 | 0.0 | 44 | 320 | 350 |
| chaos-dns-error | DNSChaos | 4 | 0 | 0.0 | ≥30 | 0 | 30 |
| pair-hc-kill-1 | PodChaos | 2 | 174 | 0.0 | 33 | 130 | 0 |
| pair-servers-kill-1 | PodChaos | 2 | 174 | 0.0 | 33 | 130 | 0 |
| pair-agent-kill-1 | PodChaos | 2 | 174 | 0.0 | 33 | 150 | 180 |
| pair-both-servers-kill | PodChaos | 2 | 174 | 0.0 | 33 | 150 | 180 |
| pair-agent-kill-2 | PodChaos | 2 | 173 | 0.0 | 0.0 | 200 | 180 |
| pair-reconciler-kill-1 | PodChaos | 2 | 173 | 0.0 | 0.0 | 200 | 180 |
| pair-hc-kill-2 | PodChaos | 2 | 178 | 0.0 | 0.0 | 10 | 0 |
| pair-hc-packet-loss | NetworkChaos | 2 | 199 | 0.0 | 0.0 | 10 | 0 |
| pair-reconciler-kill-2 | PodChaos | 2 | 0 | 0.0 | 0.0 | 0 | 0 |
| pair-infra-latency-1 | NetworkChaos | 2 | 16 | 0.0 | 0.0 | 0 | 0 |
| pair-reconciler-kill-2 | PodChaos | 2 | 5 | 0.0 | 0.0 | 0 | 0 |
| pair-infra-latency-1 | NetworkChaos | 2 | 137 | 0.0 | 0.0 | 0 | 0 |
| pair-reconciler-kill-2 | PodChaos | 2 | 69 | 0.0 | 0.0 | 0 | 0 |
| pair-infra-latency-1 | NetworkChaos | 2 | 127 | 0.0 | 0.0 | 0 | 0 |
| single-hc-kill | PodChaos | 1 | 116 | 0.0 | 0.0 | 0 | 0 |
| single-hc-failure-long | PodChaos | 1 | 100 | 0.0 | ≥130 | 0 | 0 |

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
- `graphs/zoom/4_phase4_total/` — Фаза 4: тотальный хаос
