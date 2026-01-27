## Архитектура

Видимо мы хотим чтобы у нас был лог и статус для каждой ноды control-plane


Типо у нас есть:

- target-group destination state+settings
- targets
- target-group sequence or entry in tasks table

Потом control-plane считывает что что-то поменялось, понимает на какие ноды
lb нужно это распределить и выдает команду для lb ноды. 

Видимо создает таску, ее подбирают control-plane узлы и говорят в агента
что нужно сделать, но тут еще вопрос обратеной связи, им же нужно как-то знать что агент
сделал или нет, ну видимо мы еще хотим чтобы когда агент приходил с новой
версией мы понимали что все до он уже принял. 



/cpl/v1/
├── configs/
│   └── global_version → "42" (атомарный счетчик через CAS)
│
├── targetgroups/
│   ├── web-tg/
│   │   ├── spec → {
│   │   │   "protocol": "tcp",
│   │   │   "port": 80,
│   │   │   "health_check": {"interval": "5s", "timeout": "2s"},
│   │   │   "version": 15
│   │   │ }
│   │   └── endpoints → [
│   │       {"ip": "10.0.0.1", "port": 80, "weight": 1, "status": "UP"},
│   │       {"ip": "10.0.0.2", "port": 80, "weight": 1, "status": "DOWN"}
│   │   ]
│   └── db-tg/... (аналогично)
│
├── nodes/
│   ├── node-1/
│   │   ├── spec → {
│   │   │   "desired_config_version": 42,
│   │   │   "bgp": {
│   │   │       "local_asn": 65000,
│   │   │       "peers": [{"ip": "192.0.2.1", "asn": 65001}],
│   │   │       "advertised_prefixes": ["203.0.113.0/24"]
│   │   │   },
│   │   │   "assigned_targetgroups": ["web-tg", "db-tg"]
│   │   │ }
│   │   ├── status → {
│   │   │   "current_config_version": 40,
│   │   │   "last_heartbeat": "2024-01-23T15:30:45Z",
│   │   │   "vpp_state": "HEALTHY"
│   │   │ }
│   │   └── heartbeat_lease → "12345" (lease ID для автоматического обнаружения падений)
│   ├── node-2/... (аналогично)
│   └── ... (все ноды)
│
└── tasks/
    └── config_update/
        ├── task-123 → {
        │   "node_id": "node-1",
        │   "target_version": 42,
        │   "reason": "targetgroup_updated",
        │   "created_at": "2024-01-23T15:31:00Z"
        │ }
        └── ... (все задачи)




        /cpl/v1/
├── configs/
│   └── global_version → "42" (глобальный счетчик для всех изменений)
│
├── targetgroups/
│   └── web-tg/
│       ├── spec → {
│       │   "protocol": "tcp",
│       │   "port": 80,
│       │   "version": 15,          // Текущая версия TG
│       │   "last_modified_by": "admin@console",
│       │   "last_modified_at": "2024-01-23T15:30:45Z"
│       │ }
│       ├── endpoints → [
│       │   {"id": "ep-1", "ip": "10.0.0.1", "port": 80, "weight": 1, "status": "UP"},
│       │   {"id": "ep-2", "ip": "10.0.0.2", "port": 80, "weight": 1, "status": "DOWN"}
│       │ ]
│       └── history/                // История изменений endpoints
│           ├── change-20240123T153045Z → {
│           │   "operation": "ADD_ENDPOINT",
│           │   "target_version": 15,        // Версия TG после изменения
│           │   "endpoints_added": ["ep-3"],
│           │   "endpoints_removed": [],
│           │   "modified_by": "operator@console",
│           │   "timestamp": "2024-01-23T15:30:45Z"
│           │ }
│           └── change-20240123T152500Z → { ... }  // Предыдущее изменение
│
├── nodes/
│   └── node-1/
│       ├── spec → {
│       │   "desired_config_version": 42,   // Версия конфигурации, которую нужно применить
│       │   "assigned_targetgroups": ["web-tg", "api-tg"],
│       │   "bgp": { ... }
│       │ }
│       └── status → {
│           "current_config_version": 40,   // Версия, которая уже применена
│           "last_heartbeat": "2024-01-23T15:31:00Z",
│           "last_applied_change_id": "change-20240123T153045Z"  // Ссылка на последнее примененное изменение
│       }
│
└── tasks/
    └── config_update/
        └── task-123 → {
            "node_id": "node-1",
            "target_config_version": 42,      // Глобальная версия конфигурации
            "related_change_id": "change-20240123T153045Z",  // Ссылка на изменение в history
            "created_at": "2024-01-23T15:31:05Z",
            "lease_id": "67890"               // Lease с TTL=60s для автоматической очистки
        }