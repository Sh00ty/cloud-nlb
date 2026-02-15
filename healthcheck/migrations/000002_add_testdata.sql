
INSERT INTO settings (target_group, interval, success_before_passing, failures_before_critical, initial_state, strategy, strategy_settings)
VALUES
    ('web-frontend', '2s', 1, 2, true, 'mock', '{"description": "Always returns UP"}'),

    ('api-backend', '3s', 2, 3, false, 'mock', '{"description": "Mock with custom metadata"}'),

    ('critical-db', '1s', 1, 1, true, 'mock', '{"description": "High-frequency mock"}'),

    ('mws-k8s-kubeproxy', '2s', 2, 4, true, 'http', '{"timeout": 0, "path": "/health/", "scheme": "http", "method": "GET"}')
ON CONFLICT (target_group) DO NOTHING;

INSERT INTO targets (real_ip, port, target_group, vshard)
VALUES
    ('192.168.10.10', 80, 'web-frontend',  122),
    ('192.168.10.11', 80, 'web-frontend', 39),
    ('192.168.10.12', 80, 'web-frontend', 140),

    ('10.0.0.5', 443, 'api-backend', 23),
    ('10.0.0.6', 443, 'api-backend', 57),

    ('172.16.0.100', 5432, 'critical-db', 295),
    ('172.16.0.101', 5432, 'critical-db', 48),

    ('192.168.5.2', 9050, 'mws-k8s-kubeproxy', 231),
    ('192.168.5.2', 9051, 'mws-k8s-kubeproxy', 144),
    ('192.168.5.2', 9052, 'mws-k8s-kubeproxy', 149)
ON CONFLICT (real_ip, port, target_group) DO NOTHING;