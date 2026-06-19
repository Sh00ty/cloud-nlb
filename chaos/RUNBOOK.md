# Chaos Testing Runbook — Cloud NLB

Дата последнего обновления: 2026-05-09. Кластер: colima (k3s v1.33.4), macOS aarch64 Virtualization.Framework.

---

## Содержание

1. Быстрая разведка окружения
2. Запуск инфраструктуры (etcd, Postgres, Redpanda, CDC)
3. Деплой компонентов Cloud NLB в Kubernetes
4. Анддеплой (teardown) компонентов
5. Сборка образов
6. Минимальный one-shot эксперимент
7. Запуск полного workflow
8. Где смотреть статус
9. Как корректно остановить эксперимент
10. Etcd keyspace — что и где лежит
11. Грабли которые встретили в этой сессии
12. Команды которые работают на этой машине
13. История прошлых запусков

---

## Быстрая разведка окружения

### Проверка контекста и переключение на colima

```bash
kubectl config get-contexts
# Нужный контекст называется "colima", он не выбран по умолчанию.
# Активный по умолчанию контекст — msk-kcd-dev-iaas-net (удалённый, требует OIDC).
# Он недоступен без VPN/корпоративной сети — kubectl зависает на 30 секунд.

kubectl config use-context colima
```

### Проверка кластера

```bash
kubectl get nodes
# Ожидаемый вывод: одна нода "colima" в статусе Ready (control-plane,master)

kubectl get namespaces
# Ожидаемые namespace: chaos-mesh, cloud-nlb, monitoring, default, kube-*
```

### Проверка chaos-mesh

```bash
kubectl get crd | grep chaos-mesh
# Должны присутствовать: podchaos, networkchaos, stresschaos, dnschaos, timechaos, iochaos и др.

kubectl -n chaos-mesh get pods
# Ожидаемые поды: chaos-controller-manager (3 реплики), chaos-daemon, chaos-dashboard, chaos-dns-server
# Все должны быть Running.
```

Пример вывода на рабочем стенде (2026-05-09):

```
NAME                                        READY   STATUS    RESTARTS
chaos-controller-manager-76456d6f7c-26xl5   1/1     Running   3
chaos-controller-manager-76456d6f7c-pmnkj   1/1     Running   3
chaos-controller-manager-76456d6f7c-whnwr   1/1     Running   2
chaos-daemon-jmpg4                          1/1     Running   2
chaos-dashboard-84cf59bdcc-7gsts            1/1     Running   2
chaos-dns-server-956cdccb8-znmk2            1/1     Running   3
```

### Проверка компонентов Cloud NLB

```bash
kubectl get pods -n cloud-nlb
# Если вывод "No resources found" — стенд не задеплоен. Запусти make deploy перед chaos-тестами.

kubectl get pods -n monitoring
# Prometheus и Grafana: prometheus-* и grafana-*
```

### Проверка существующих chaos ресурсов

```bash
kubectl get podchaos,networkchaos,stresschaos -A
kubectl get workflows.chaos-mesh.org -A
kubectl get workflownodes.chaos-mesh.org -n cloud-nlb | head -30
```

---

## Запуск инфраструктуры (etcd, Postgres, Redpanda, CDC)

Инфраструктура запускается в Docker (не в Kubernetes). Это намеренное архитектурное решение: хост-машина с IP `192.168.5.2` (colima bridge) доступна из подов k8s, поэтому компоненты в k8s обращаются к ней напрямую.

### Проверка текущего состояния Docker-контейнеров

```bash
docker ps -a --format "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"
# Ожидаемые контейнеры: etcd, postgres, redpanda, cdc-connector
# Если Status = "Exited" — нужно запустить (см. ниже)
```

### Запуск всей инфраструктуры одной командой

```bash
# Из корня репозитория:
make infra
# Это выполняет:
#   docker run -d --name etcd ...
#   cd ./healthcheck && docker-compose up -d
```

Если контейнеры уже существуют (Exited), команда docker run упадёт с ошибкой "already exists". В этом случае запустить существующие контейнеры:

```bash
docker start etcd
cd healthcheck && docker-compose start
# или стартовать всё разом:
docker start etcd postgres redpanda cdc-connector
```

### Запуск etcd отдельно

```bash
# Первый запуск (создание контейнера):
docker run -d \
  --name etcd \
  -p 2379:2379 \
  -e ALLOW_NONE_AUTHENTICATION=yes \
  bitnamilegacy/etcd

# Повторный запуск (контейнер уже создан):
docker start etcd

# Проверка:
docker logs etcd --tail=10
# Должна быть строка: "serving insecure client requests on [::]:2379"
```

etcd слушает на порту `2379`. Хост-адрес для подов k8s: `192.168.5.2:2379` (colima bridge IP). Для локального запуска компонентов (не в k8s): `localhost:2379`.

### Запуск Postgres + Redpanda + Debezium CDC

```bash
cd /Users/psshlykov/prog/mipt/cloud-nlb/healthcheck
docker-compose up -d

# Проверка:
docker-compose ps
docker logs postgres --tail=5
docker logs redpanda --tail=5
docker logs cdc-connector --tail=5
```

Что делает docker-compose:
- `postgres:17` — база данных HC, порт `5432`. При первом запуске автоматически применяет миграции из `migrations/000001_init_hc_worker_tables.sql` и `000002_add_testdata.sql` (монтируются как `/docker-entrypoint-initdb.d/`). Миграции применяются только если том `postgres_data` пустой.
- `redpanda` — Kafka-совместимый брокер, порт `19092` (external, доступен с хоста и из k8s). Внутренний порт `9092` используется только между контейнерами.
- `cdc-connector` (Debezium 2.3) — Kafka Connect, порт `8083`. Стартует после того как postgres и redpanda стали healthy.

Важно: `--advertise-kafka-addr external://192.168.5.2:19092` — этот адрес жёстко задан в docker-compose.yml как адрес для клиентов вне docker-сети (т.е. для k8s-подов). Если colima bridge IP изменится, нужно обновить этот параметр и перезапустить контейнер.

### Инициализация CDC-коннектора (обязательный шаг после первого старта)

Debezium-коннектор не создаётся автоматически. После запуска `cdc-connector` нужно зарегистрировать коннектор вручную:

```bash
cd /Users/psshlykov/prog/mipt/cloud-nlb/healthcheck/kafka-connect
bash init-pg.sh
```

Скрипт делает POST-запрос на `http://localhost:8083/connectors/` и создаёт коннектор `postgres-connector`. Он настроен на:
- Таблицы: `public.targets`, `public.target_statuses`
- CDC plugin: `pgoutput` (встроен в Postgres 17, дополнительная установка не нужна)
- Replication slot: `debezium_postgres`
- Publication: `dbz_publication` (создаётся автоматически в режиме `publication.autocreate.mode: filtered`)
- Топики Redpanda: `dbserver1.public.targets`, `dbserver1.public.target_statuses`

Проверка что коннектор создан и работает:

```bash
curl -s http://localhost:8083/connectors/postgres-connector/status | python3 -m json.tool
# Ожидаемое: "state": "RUNNING" как для коннектора, так и для task[0]
```

Если статус `FAILED`, смотреть детали ошибки:

```bash
curl -s http://localhost:8083/connectors/postgres-connector/status | python3 -m json.tool | grep -A5 '"tasks"'
docker logs cdc-connector --tail=30
```

### Проверка что топики созданы в Redpanda

```bash
docker exec redpanda rpk topic list
# Должны быть: dbserver1.public.targets, dbserver1.public.target_statuses
# Также служебные: my_connect_configs, my_connect_offsets, my_connect_statuses
```

### Остановка инфраструктуры

```bash
# Остановить без удаления данных (тома сохранятся):
docker stop etcd
cd healthcheck && docker-compose stop

# Удалить с очисткой томов (полный сброс, миграции применятся заново при следующем старте):
docker rm -f etcd
cd healthcheck && docker-compose down -v
```

### Сетевые адреса инфраструктуры

| Сервис | Адрес из k8s-подов | Адрес с хост-машины | Примечание |
|--------|-------------------|---------------------|------------|
| etcd | `192.168.5.2:2379` | `localhost:2379` | ALLOW_NONE_AUTHENTICATION=yes |
| Postgres | `192.168.5.2:5432` | `localhost:5432` | user/pass/db: postgres/postgres/postgres |
| Redpanda (Kafka) | `192.168.5.2:19092` | `localhost:19092` | external listener |
| Debezium REST API | недоступен из k8s | `localhost:8083` | только для init-скрипта |

---

## Деплой компонентов Cloud NLB в Kubernetes

Перед деплоем убедиться что:
1. `kubectl config use-context colima` выполнен
2. Инфраструктура (etcd, postgres, redpanda, cdc-connector) запущена в Docker
3. CDC-коннектор инициализирован (`init-pg.sh`)

### Полный деплой всего стенда

```bash
cd /Users/psshlykov/prog/mipt/cloud-nlb
make deploy
# Порядок: deploy-obs, deploy-hc, deploy-cp, deploy-agent, deploy-tools
# В конце автоматически выполняется make status
```

### Деплой отдельных компонентов

```bash
# Только observability (Prometheus + Grafana в namespace monitoring):
make deploy-obs
# Применяет: namespace, prometheus configmap/rbac/deployment, grafana provisioning+deployment
# После деплоя синхронизирует дашборды: kubectl create configmap grafana-dashboards ...

# Только Healthcheck (HC API + HC Workers в namespace cloud-nlb):
make deploy-hc
# Применяет: namespace, configmap, api-deployment+service+ingress, wrk-service+statefulset

# Только Control Plane (API + Reconciler в namespace cloud-nlb):
make deploy-cp
# Применяет: namespace, configmap, api-service+ingress+deployment, reconciler-deployment

# Только NLB Agent (StatefulSet в namespace cloud-nlb):
make deploy-agent
# Применяет: namespace, configmap, agent-service+statefulset

# Только тестовые инструменты (test-server + test-client в namespace cloud-nlb):
make deploy-tools
# Применяет: test-srv-deployment.yaml, test-clnt-deployment.yaml
# Второй сервер (test-srv-deployment-2.yaml) и второй клиент закомментированы в Makefile
```

### Проверка после деплоя

```bash
make status
# Показывает: nodes, monitoring pods, cloud-nlb pods, ingress/ingressroute

# Детально по компонентам:
kubectl get pods -n cloud-nlb -o wide
kubectl get pods -n monitoring
kubectl get statefulset -n cloud-nlb   # hc-worker (4 реплики), nlb-agent
kubectl get deployment -n cloud-nlb    # control-plane-apiserver, control-plane-reconciler, hc-api
kubectl get ingress -n cloud-nlb       # hc-server.local, control-plane-apiserver.local
```

### Ожидаемые поды после полного деплоя

```
NAMESPACE    NAME                                    READY   STATUS
cloud-nlb    control-plane-apiserver-*               1/1     Running
cloud-nlb    control-plane-reconciler-*              1/1     Running
cloud-nlb    hc-api-*                                1/1     Running
cloud-nlb    hc-worker-0                             1/1     Running
cloud-nlb    hc-worker-1                             1/1     Running
cloud-nlb    hc-worker-2                             1/1     Running
cloud-nlb    hc-worker-3                             1/1     Running
cloud-nlb    nlb-agent-0                             1/1     Running
cloud-nlb    test-server-0                           1/1     Running
cloud-nlb    test-server-1                           1/1     Running
cloud-nlb    test-client-*                           1/1     Running
monitoring   grafana-*                               1/1     Running
monitoring   prometheus-*                            1/1     Running
```

hc-worker и nlb-agent — StatefulSet с предсказуемыми именами (hc-worker-0..3, nlb-agent-0).

### Перезапуск отдельных компонентов без пересоздания

```bash
# Control Plane (есть отдельный target):
cd control-plane && make restart
# Делает: kubectl rollout restart deployment/control-plane-apiserver и control-plane-reconciler

# Любой deployment вручную:
kubectl rollout restart deployment/<name> -n cloud-nlb

# StatefulSet:
kubectl rollout restart statefulset/hc-worker -n cloud-nlb
kubectl rollout restart statefulset/nlb-agent -n cloud-nlb
```

### Hosts (Traefik ingress)

Ingress-роуты используют имена `control-plane-apiserver.local`, `hc-server.local`, `grafana.local`. Эти имена резолвятся через `/etc/hosts`. Для обновления:

```bash
cd /Users/psshlykov/prog/mipt/cloud-nlb
make hosts-update
# Получает IP colima (colima list -j | jq .address) и добавляет в /etc/hosts (требует sudo)
```

---

## Анддеплой (teardown) компонентов

### Полная очистка всего стенда (k8s-поды)

```bash
cd /Users/psshlykov/prog/mipt/cloud-nlb
make clean
# Порядок: control-plane delete, healthcheck delete, nlb-agent delete, tools delete
# НЕ трогает: obs (Prometheus/Grafana), chaos-mesh, Docker-инфраструктуру
```

### Удаление отдельных компонентов

```bash
cd control-plane && make delete
# Удаляет: reconciler-deployment, api-ingress+service+deployment, configmap

cd healthcheck && make delete
# Удаляет: api-deployment+service+ingress, wrk-statefulset+service, configmap

cd nlb-agent && make delete
# Удаляет: agent-statefulset+service, configmap

cd tools && make delete
# Удаляет: test-srv-deployment, test-clnt-deployment
```

Все `delete` используют флаг `--ignore-not-found`, поэтому повторный вызов безопасен.

### Остановка Docker-инфраструктуры

```bash
# Мягкая остановка (данные сохраняются):
docker stop etcd postgres redpanda cdc-connector

# Или через docker-compose для pg/redpanda/cdc:
cd healthcheck && docker-compose stop

# Полная очистка с удалением томов (следующий start применит миграции заново):
docker rm -f etcd
cd healthcheck && docker-compose down -v
```

---

## Сборка образов

Все образы собираются в containerd namespace `k8s.io` через colima nerdctl. Это делает их сразу доступными в k8s без push в registry.

```bash
# Все компоненты сразу:
cd /Users/psshlykov/prog/mipt/cloud-nlb
make build

# Отдельно:
make build-hc       # hc-api-server:dev, hc-wrk:dev
make build-cp       # control-plane-apiserver:dev, control-plane-reconciler:dev
make build-agent    # nlb-agent:dev
make build-tools    # testsrv:dev, testclient:dev
```

Команда сборки под капотом:

```bash
colima nerdctl -- --namespace k8s.io build -t <image>:dev . -f <dockerfile>
```

Проверить что образы видны k8s:

```bash
colima nerdctl -- --namespace k8s.io images | grep -E "hc-wrk|control-plane|nlb-agent|testsrv|testclient"
```

---

## Синхронизация тестового сервера в CPL и HC (testsrvsyncer)

После каждого (пере)деплоя тестовых серверов их pod IP меняются. CPL и HC хранят эндпоинты с конкретными IP. Если не синхронизировать вручную, трафик уйдёт в никуда: в CPL и HC останутся старые IP, а новые поды не будут знать о себе.

Инструмент: `tools/cmd/testsrvsyncer/main.go`. Сборки и k8s-манифеста нет — запускается локально через `go run`.

### Что делает syncer (порядок шагов)

1. Подключается к k8s API и получает IP всех Ready-подов по селектору (`app=test-server` по умолчанию).
2. Вызывает `CPL.UpsertTargetGroupSpec` — создаёт или обновляет TargetGroup в Control Plane (идемпотентно).
3. Вызывает `HC.CreateSettings` — создаёт настройки healthcheck для TargetGroup (HTTP GET `/health/` каждые 3 секунды, 2 успеха для перехода в UP, 3 ошибки для DOWN).
4. Сверяет текущие эндпоинты в HC с desired (pod IPs). Добавляет новые, удаляет stale.
5. Из HC берёт список stale IP и удаляет их из CPL (`UpsertEndpoint` с `REMOVE`).
6. Добавляет все desired IP в CPL (`UpsertEndpoint` с `ADD`, идемпотентно).

HC используется как промежуточный источник истины для вычисления stale-эндпоинтов в CPL.

### Запуск

```bash
cd /Users/psshlykov/prog/mipt/cloud-nlb

# Стандартный запуск (все параметры по умолчанию):
go run ./tools/cmd/testsrvsyncer/

# С явным kubeconfig (если context не colima по умолчанию):
go run ./tools/cmd/testsrvsyncer/ \
  -kubeconfig ~/.kube/config

# Dry-run — только показать diff, ничего не менять:
go run ./tools/cmd/testsrvsyncer/ -dry-run

# Нестандартные адреса (если port-forward вместо ingress):
go run ./tools/cmd/testsrvsyncer/ \
  -cpl-addr localhost:9091 \
  -hc-addr localhost:9090
```

### Флаги

| Флаг | Значение по умолчанию | Описание |
|------|-----------------------|----------|
| `-kubeconfig` | `""` (in-cluster) | Путь к kubeconfig; пустая строка — in-cluster конфиг |
| `-namespace` | `cloud-nlb` | Namespace для поиска подов |
| `-selector` | `app=test-server` | Label selector для подов |
| `-cpl-addr` | `control-plane-apiserver.local:30080` | gRPC адрес Control Plane |
| `-hc-addr` | `hc-server.local:30080` | gRPC адрес HC API |
| `-target-group` | `test-server` | Имя TargetGroup в CPL и HC |
| `-tg-port` | `8081` | Порт трафика (регистрируется в CPL) |
| `-tg-protocol` | `TCP` | Протокол TargetGroup |
| `-tg-vip` | `10.96.0.100` | Виртуальный IP TargetGroup |
| `-hc-port` | `8090` | Порт healthcheck на подах (регистрируется в HC) |
| `-dry-run` | `false` | Только показать diff |

Порты важно не перепутать: `-tg-port 8081` это порт трафика (metrics/traffic endpoint у test-server), `-hc-port 8090` это порт на котором test-server отвечает на healthcheck-запросы (`/health/`).

### Когда запускать syncer

Запускать после каждого из этих событий:

- `make deploy-tools` или повторный `kubectl apply` на test-srv-deployment.yaml — у подов новые IP
- `kubectl rollout restart deployment/test-server -n cloud-nlb` — те же причины
- Масштабирование (`kubectl scale`) test-server — добавились или убрались поды
- Полный `make deploy` (деплоит tools в том числе)

Проверить что синк нужен (dry-run):

```bash
go run ./tools/cmd/testsrvsyncer/ -dry-run
# Если выводит "hc: already in sync" — синкать не нужно
# Если выводит "hc: will add" или "hc: will remove" — синк нужен
```

### Почему container-kill плохо работает в chaos-тестах

При `action: container-kill` chaos-mesh убивает контейнер внутри пода. Kubernetes немедленно поднимает его заново — под остаётся живым с тем же именем и тем же pod IP. Это штатное поведение.

Проблема в другом: при перезапуске контейнера pod IP не меняется, но состояние регистрации в HC и CPL не сбрасывается. Кажется что всё хорошо — pod IP тот же, в HC и CPL он зарегистрирован.

Однако в реальных прогонах возникает ситуация когда под после container-kill проходит короткий период NotReady (контейнер стартует), а healthcheck в HC успевает пометить эндпоинт как DOWN. После восстановления HC снова переводит его в UP, но это занимает `failures_before_critical * interval` (3 × 3с = 9 секунд) для детекции + `success_before_passing * interval` (2 × 3с = 6 секунд) для восстановления. Итого до 15 секунд "серого" периода.

Более принципиальная проблема: если в ходе chaos-теста произошёл полный редеплой test-server (make deploy-tools), то после него pod IP у всех подов новые, а в HC и CPL — старые. container-kill в этом состоянии просто убивает контейнер в поде с новым IP, которого в HC нет, и трафик на него всё равно не идёт. Syncer нужно запустить руками.

`action: pod-failure` этой проблемы не имеет: chaos-mesh переводит pod в состояние "не проходит readiness probe", но не убивает и не пересоздаёт контейнер. Pod IP не меняется, регистрация в HC и CPL остаётся актуальной. После окончания pod-failure под снова проходит readiness probe и HC автоматически возвращает его в UP.

Вывод: для chaos-тестов тестового сервера использовать `pod-failure`, а не `container-kill`. `container-kill` полезен только для тестирования сценария быстрого перезапуска процесса (краш) — но в этом случае перед тестом нужно убедиться что syncer был запущен и HC/CPL содержат актуальные IP.

---

## Минимальный one-shot эксперимент

Самый дешёвый сценарий — `pod-failure` на nlb-agent с duration 60 секунд. Применяется как отдельный PodChaos CRD, без запуска всего workflow.

### YAML для одиночного эксперимента

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: nlb-oneshot-agent-failure
  namespace: cloud-nlb
spec:
  selector:
    namespaces:
      - cloud-nlb
    labelSelectors:
      app: nlb-agent
  mode: random-max-percent
  value: "50"
  action: pod-failure
  duration: 60s
```

Сохранить в `/tmp/nlb-oneshot.yaml` и применить:

```bash
kubectl apply -f /tmp/nlb-oneshot.yaml
```

### Вариант для test-server (pod-failure, не container-kill)

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: nlb-oneshot-server-failure
  namespace: cloud-nlb
spec:
  selector:
    namespaces:
      - cloud-nlb
    labelSelectors:
      app: test-server
  mode: random-max-percent
  value: "50"
  action: pod-failure
  duration: 60s
```

Для test-server использовать `pod-failure`, а не `container-kill`. Подробности и причина — в разделе "Синхронизация тестового сервера" выше.

---

## Где смотреть статус

### Статус chaos эксперимента

```bash
# Краткий статус всех podchaos
kubectl get podchaos -n cloud-nlb

# Детальный статус с условиями и событиями
kubectl describe podchaos -n cloud-nlb <name>

# Полезные поля в describe:
# Conditions.Type=Selected (True = нашёл поды-цели)
# Conditions.Type=AllInjected (True = fault внедрён)
# Conditions.Type=AllRecovered (True = всё восстановлено)
# Conditions.Type=Paused (True = эксперимент на паузе или завершён)
# Events: показывает Apply/Recover операции с timestamp
```

### Статус workflow

```bash
kubectl get workflows.chaos-mesh.org -n cloud-nlb
kubectl get workflownodes.chaos-mesh.org -n cloud-nlb

# Фаза всего workflow (Accomplished / Running / Failed):
kubectl get workflows.chaos-mesh.org -n cloud-nlb <name> -o jsonpath='{.status.phase}'

# Полный статус как JSON:
kubectl get workflows.chaos-mesh.org -n cloud-nlb <name> -o jsonpath='{.status}' | python3 -m json.tool
```

### Статус компонентов Cloud NLB

```bash
# Все поды
kubectl get pods -n cloud-nlb -o wide

# События в реальном времени
kubectl get events -n cloud-nlb --sort-by=.lastTimestamp

# Логи конкретного компонента
kubectl logs -n cloud-nlb -l app=nlb-agent --tail=50
kubectl logs -n cloud-nlb -l app=hc-worker --tail=50
kubectl logs -n cloud-nlb -l app=control-plane-reconciler --tail=50
```

### Метрики (если задеплоен monitoring)

```bash
# Проброс Grafana (порт 3000)
kubectl port-forward -n monitoring svc/grafana 3000:3000 &
# Открыть http://localhost:3000 (admin/admin или admin/prom-operator)

# Проброс Prometheus (порт 9090)
kubectl port-forward -n monitoring svc/prometheus 9090:9090 &
# Открыть http://localhost:9090
```

---

## Как корректно остановить эксперимент

### Удалить отдельный chaos ресурс

```bash
# PodChaos
kubectl delete podchaos -n cloud-nlb <name>

# NetworkChaos
kubectl delete networkchaos -n cloud-nlb <name>

# StressChaos
kubectl delete stresschaos -n cloud-nlb <name>
```

После удаления chaos-mesh автоматически отправляет Recover-операцию на затронутые поды. Это видно в `describe` в разделе Events (Operation: Recover).

### Проверка что dangling ресурсов нет

```bash
kubectl get podchaos,networkchaos,stresschaos -n cloud-nlb
# Убедиться что собственноручно созданный ресурс исчез из списка
```

### Остановить workflow (не удалять, а приостановить)

```bash
kubectl annotate workflow -n cloud-nlb <name> experiment.chaos-mesh.org/pause=true
# Для возобновления:
kubectl annotate workflow -n cloud-nlb <name> experiment.chaos-mesh.org/pause-
```

### Cooldown периоды из проекта

Согласно конвенции проекта (из chaos-mesh-workflow.yaml):
- Между отдельными экспериментами внутри фазы: 45 секунд
- Между фазами: 2 минуты

---

## Запуск полного workflow

```bash
# Из директории chaos/
cd /Users/psshlykov/prog/mipt/cloud-nlb/chaos
make start
# Это делает: kubectl replace --force -f chaos-mesh-workflow.yaml

# Мониторинг прогресса
kubectl get workflownodes.chaos-mesh.org -n cloud-nlb -w
```

Workflow `nlb-resilience-suite-colima-safe` выполняется последовательно: phase-1-singles, phase-2-pairs, phase-3-triples, phase-4-total-chaos. Общий deadline 75 минут.

---

## Грабли которые встретили в этой сессии

### 1. Неверный kubectl context

По умолчанию активен контекст `msk-kcd-dev-iaas-net` (удалённый корпоративный кластер с OIDC). При недоступности keycloak.mws-team.ru каждая команда зависает на 30 секунд с ошибкой:

```
error: get-token: authentication error: oidc error: oidc discovery error: 
Get "https://keycloak.mws-team.ru/realms/...": read tcp ...: connection reset by peer
```

Перед любой работой выполнять `kubectl config use-context colima`.

### 2. Поды Cloud NLB не задеплоены при чистом стенде

При `kubectl get pods -n cloud-nlb` выводится `No resources found`. Это означает что стенд не запущен. Chaos-эксперименты в этом состоянии создаются без ошибок, но завершаются предупреждением:

```
Warning  Failed  3s  records  Failed to select targets: no pod is selected
```

Conditions.Type=Selected остаётся False. Эксперимент технически не сломан, но фактически ничего не инжектирует. Для реальных тестов необходимо предварительно задеплоить стенд: `make deploy` из корня репозитория.

### 3. Старые chaos ресурсы из прошлых сессий

В namespace cloud-nlb остаются podchaos и networkchaos ресурсы от прошлых запусков (возраст 40-50 дней). Они находятся в состоянии Paused / AllRecovered. Они не активны и не влияют на новые эксперименты, но засоряют вывод `kubectl get podchaos -A`. При необходимости можно убрать: `kubectl delete podchaos -n cloud-nlb --all` (осторожно — удалит всё включая нужные).

### 4. kubectl version --short не работает

Флаг `--short` удалён в kubectl v1.34. Использовать `kubectl version` без флагов.

### 5. NetworkChaos pg-etcd-latency-z2r7n

В namespace cloud-nlb есть "висящий" networkchaos ресурс `pg-etcd-latency-z2r7n` (action: delay, duration: 5m) из прошлой сессии. При включённых подах он может влиять на latency к etcd/postgres. Проверить его статус перед началом экспериментов.

---

## Команды которые работают на этой машине

```bash
# Платформа: macOS aarch64, colima v0.x, containerd runtime
# Kubernetes: k3s v1.33.4
# kubectl client: v1.34.1

# Работает: colima status
# Работает: kubectl config use-context colima
# Работает: kubectl apply / delete / describe / get для chaos CRD
# Работает: kubectl port-forward

# НЕ работает без VPN: kubectl с контекстами msk-*, mdb-*, sdf-*
# НЕ работает: kubectl version --short (удалён в v1.34)
# НЕ работает: colima nerdctl -- <cmd> если colima остановлена

# Для сборки образов (из Makefile):
# colima nerdctl -- --namespace k8s.io build -t <image>:dev . -f <dockerfile>
```

---

## История прошлых запусков

| Дата | Workflow | Статус | Примечание |
|------|----------|--------|------------|
| 2026-03-19 | nlb-resilience-suite | Accomplished (частично) | Отдельные podchaos применялись вручную |
| 2026-03-29 | nlb-resilience-suite-colima-safe | Accomplished | Полный прогон, endTime 13:52 UTC |

Артефакты прошлых запусков хранятся в `chaos/backups/`.
