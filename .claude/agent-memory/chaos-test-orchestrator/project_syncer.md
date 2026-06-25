---
name: Syncer и container-kill
description: testsrvsyncer — обязательный шаг после редеплоя test-server; container-kill непригоден для chaos-тестов
type: project
---

Инструмент: `tools/cmd/testsrvsyncer/main.go`. Нет Makefile-target и k8s-манифеста, запускается локально:
`go run ./tools/cmd/testsrvsyncer/` из корня репозитория.

Что делает: берёт IP Ready-подов с selector app=test-server из k8s, синхронизирует их в CPL (UpsertEndpoint ADD/REMOVE) и HC (AddEndpoints/RemoveEndpoints). HC используется как промежуточный источник истины для вычисления stale IP в CPL.

Когда запускать: после любого make deploy-tools, rollout restart, scale test-server.
Dry-run проверка: `go run ./tools/cmd/testsrvsyncer/ -dry-run`

Дефолтные адреса: cpl-addr=control-plane-apiserver.local:30080, hc-addr=hc-server.local:30080.
Дефолтные порты: tg-port=8081 (трафик), hc-port=8090 (healthcheck /health/).

**Why container-kill не работает для chaos:** при container-kill pod IP не меняется, но если до теста был полный редеплой — в HC/CPL старые IP и новые поды там не зарегистрированы. Кроме того, container-kill вызывает кратковременный NotReady, HC помечает DOWN, восстановление занимает до 15 секунд (3×3с детекция + 2×3с восстановление).

**pod-failure работает:** chaos-mesh переводит pod в не-ready без убийства контейнера, pod IP не меняется, после окончания fault HC автоматически восстанавливает UP.

**How to apply:** для chaos-тестов test-server всегда использовать pod-failure. container-kill допустим только для тестирования сценария краш-рестарта процесса, и только если syncer был запущен непосредственно перед тестом.
