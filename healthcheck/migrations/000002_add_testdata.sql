
INSERT INTO settings (id, pool_name, interval, success_before_passing, failures_before_critical, initial_state, strategy, strategy_settings)
VALUES
    (1, 'web-frontend', '2s', 1, 2, true, 'mock', '{"description": "Always returns UP"}'),
    
    (2, 'api-backend', '3s', 2, 3, false, 'mock', '{"description": "Mock with custom metadata"}'),

    (3, 'critical-db', '1s', 1, 1, true, 'mock', '{"description": "High-frequency mock"}'),

    (4, 'local-services', '2s', 2, 4, true, 'http', '{"timeout": 0, "path": "/health/", "scheme": "http", "method": "GET"}')
ON CONFLICT (id, pool_name) DO NOTHING;

INSERT INTO targets (real_ip, port, setting_id, vshard)
VALUES
    ('192.168.10.10', 80, 1,  122),
    ('192.168.10.11', 80, 1, 39),
    ('192.168.10.12', 80, 1, 140),

    ('10.0.0.5', 443, 2, 23),
    ('10.0.0.6', 443, 2, 57),

    ('172.16.0.100', 5432, 3, 295),
    ('172.16.0.101', 5432, 3, 48),

    ('192.168.5.2', 9050, 4, 231),
    ('192.168.5.2', 9051, 4, 144),
    ('192.168.5.2', 9052, 4, 149)
ON CONFLICT (real_ip, port, setting_id) DO NOTHING;