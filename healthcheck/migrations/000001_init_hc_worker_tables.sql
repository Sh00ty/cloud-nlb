create table if not exists settings
(
    id                          bigint          not null,
    pool_name                   text            not null,
    updated_at                  timestamptz     default now() not null,
    interval                    interval        not null,
    success_before_passing      smallint        not null,
    failures_before_critical    smallint        not null,
    initial_state               boolean         default false not null,
    strategy                    varchar (32)    not null,
    strategy_settings           jsonb,
    primary key                 (id, pool_name)
);

create table if not exists targets
(
    real_ip     varchar (64)    not null,
    port        smallint        not null,
    setting_id  bigint          not null,
    vshard      smallint,
    primary key (real_ip, port, setting_id)
);

create index if not exists targets_by_vshards
    on targets (vshard);

create table if not exists target_statuses
(
    real_ip     varchar (64)    not null,
    port        smallint        not null,
    setting_id  bigint          not null,
    updated_at  timestamptz     not null default now(),
    last_error  varchar(128)    default null,
    status      boolean,
    primary key (real_ip, port, setting_id)
);