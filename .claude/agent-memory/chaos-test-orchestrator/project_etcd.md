---
name: Etcd keyspace
description: Полная карта ключей etcd для Cloud NLB — где что хранится
type: reference
---

Файл с путями: `control-plane/internal/etcd/paths.go`

Корневые папки:
- `/cloud-nlb-registry` — весь keyspace приложения
- `/cloud-nlb-registry/target-groups` — TargetGroup данные
- `/cloud-nlb-registry/data-planes` — DataPlane данные
- `/cloud-nlb/reconciler/all-targets` — ключ leader election reconciler-а

TargetGroup ключи (tgID = имя группы, напр. "test-server"):
- `/cloud-nlb-registry/target-groups/spec/timestamp/<tgID>` — timestamp последнего изменения spec
- `/cloud-nlb-registry/target-groups/spec/desired/<tgID>/<version>` — версионированные spec (формат версии: %05d)
- `/cloud-nlb-registry/target-groups/spec/desired/latest/<tgID>` — latest spec
- `/cloud-nlb-registry/target-groups/spec/current/<tgID>` — текущая applied spec
- `/cloud-nlb-registry/target-groups/endpoints/timestamp/<tgID>` — timestamp эндпоинтов
- `/cloud-nlb-registry/target-groups/endpoints/changelog/<tgID>/<version>` — changelog событий ADD/REMOVE
- `/cloud-nlb-registry/target-groups/endpoints/compacted/<tgID>` — compacted snapshot эндпоинтов
- `/cloud-nlb-registry/target-groups/assigned/<tgID>/<nodeID>` — какой DataPlane node отвечает за TG

DataPlane ключи (nodeID = имя ноды, напр. "dpl-node-1"):
- `/cloud-nlb-registry/data-planes/placements/<nodeID>` — PlacementVersion + список TG на ноде
- `/cloud-nlb-registry/data-planes/statuses/<nodeID>` — статус ноды (alive/dead/drained/unknown)

Для инспекции через etcdctl (контейнер должен быть запущен):
```bash
docker exec etcd etcdctl get /cloud-nlb-registry --prefix --keys-only
docker exec etcd etcdctl get /cloud-nlb-registry/target-groups/spec/desired/latest --prefix
docker exec etcd etcdctl get /cloud-nlb-registry/data-planes/statuses --prefix
```
