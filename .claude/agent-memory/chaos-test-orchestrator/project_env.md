---
name: Окружение и контексты
description: Детали окружения colima, адреса всех компонентов, Docker-инфраструктура
type: project
---

Кластер: colima, k3s v1.33.4, macOS aarch64. kubectl client v1.34.1.

По умолчанию активен контекст msk-kcd-dev-iaas-net (корпоративный OIDC, зависает 30с без VPN). Перед работой: `kubectl config use-context colima`.

Инфраструктура запущена в Docker на хост-машине, доступна из k8s-подов по IP 192.168.5.2 (colima bridge):
- etcd: 192.168.5.2:2379 (контейнер bitnamilegacy/etcd, ALLOW_NONE_AUTHENTICATION=yes)
- Postgres: 192.168.5.2:5432 (postgres:17, user/pass/db = postgres/postgres/postgres)
- Redpanda: 192.168.5.2:19092 (external Kafka listener)
- Debezium CDC: только localhost:8083 (не проброшен в k8s)

Запуск инфраструктуры из корня: `make infra` (создаёт etcd + docker-compose up в healthcheck/).
Если контейнеры уже созданы (Exited): `docker start etcd postgres redpanda cdc-connector`.

Namespace k8s:
- cloud-nlb: все компоненты приложения
- monitoring: Prometheus + Grafana
- chaos-mesh: chaos-mesh (3x controller-manager, daemon, dashboard, dns-server)

**Why:** colima bridge IP фиксирован, это точка интеграции между Docker-инфраструктурой и k8s-подами.
**How to apply:** при любой проверке сетевой связности — сначала убедиться что Docker-контейнеры запущены и colima bridge доступен.
