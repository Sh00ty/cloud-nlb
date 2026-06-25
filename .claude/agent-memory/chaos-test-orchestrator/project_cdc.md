---
name: CDC инициализация
description: Debezium коннектор к Postgres, init-pg.sh, топики Redpanda — обязательный ручной шаг
type: project
---

Файлы: `healthcheck/docker-compose.yml`, `healthcheck/kafka-connect/init-pg.sh`

После первого `docker-compose up` нужно вручную зарегистрировать Debezium-коннектор:
```bash
cd healthcheck/kafka-connect && bash init-pg.sh
```

Коннектор не создаётся автоматически. Без него топики `dbserver1.public.targets` и `dbserver1.public.target_statuses` не появятся в Redpanda, и агенты не получат обновления статусов эндпоинтов.

Конфигурация коннектора:
- Имя: `postgres-connector`
- Класс: `io.debezium.connector.postgresql.PostgresConnector`
- Таблицы: `public.targets`, `public.target_statuses`
- Plugin: `pgoutput` (встроен в Postgres 17, доп. установка не нужна)
- Replication slot: `debezium_postgres`
- Publication: `dbz_publication` (autocreate mode=filtered)
- Топики: `dbserver1.public.targets`, `dbserver1.public.target_statuses`

Проверка статуса: `curl -s http://localhost:8083/connectors/postgres-connector/status | python3 -m json.tool`
Список топиков: `docker exec redpanda rpk topic list`

Postgres инициализируется автоматически при первом старте через `/docker-entrypoint-initdb.d/`:
- `000001_init_hc_worker_tables.sql` — создаёт таблицы settings, targets, target_statuses
- `000002_add_testdata.sql` — добавляет тестовые данные (web-frontend, api-backend, critical-db, mws-k8s-kubeproxy)

Миграции применяются только если том postgres_data пустой. При `docker-compose down -v` том удаляется и миграции применятся заново.

Redpanda advertise-kafka-addr для внешних клиентов (k8s-подов): `192.168.5.2:19092`. Если изменится colima bridge IP — нужно обновить этот параметр в docker-compose.yml.

**How to apply:** при любой проверке связки HC-агент (почему агенты не получают статусы) — сначала проверить что коннектор Running и топики существуют.
