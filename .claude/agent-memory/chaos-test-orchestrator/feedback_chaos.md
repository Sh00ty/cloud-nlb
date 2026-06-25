---
name: Chaos рекомендации
description: pod-failure vs container-kill для test-server; когда нужен syncer после chaos
type: feedback
---

Для test-server в chaos-экспериментах использовать `action: pod-failure`, а не `action: container-kill`.

**Why:** при container-kill pod пересоздаётся, новый под получает новый IP (если k8s решит его пересоздать) или тот же IP но HC успевает пометить DOWN на период перезапуска. Главное: если до chaos-теста делался редеплой test-server без запуска syncer, то в HC/CPL старые IP и container-kill в этом состоянии вообще не создаёт нагрузки на LB. pod-failure не меняет IP и не требует ручной синхронизации после восстановления.

**How to apply:** в любом chaos-манифесте где target app=test-server — всегда проверять что action=pod-failure. Если вижу container-kill на test-server в workflow — предупреждать пользователя.

Перед запуском chaos-теста с test-server всегда проверять синхронизацию:
```bash
go run ./tools/cmd/testsrvsyncer/ -dry-run
```
Если выводит "will add" или "will remove" — запустить синк без dry-run перед тестом.
