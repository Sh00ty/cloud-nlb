create table if not exists settings
(
    target_group                text            not null,
    updated_at                  timestamptz     default now() not null,
    interval                    interval        not null,
    success_before_passing      smallint        not null,
    failures_before_critical    smallint        not null,
    initial_state               boolean         default false not null,
    strategy                    varchar (32)    not null,
    strategy_settings           jsonb,
    primary key                 (target_group)
);

create table if not exists targets
(
    real_ip         varchar (64)    not null,
    port            smallint        not null,
    target_group    text            not null,
    vshard          smallint,
    primary key (real_ip, port, target_group)
);

create index if not exists targets_by_vshards
    on targets (vshard);

create table if not exists target_statuses
(
    real_ip         varchar (64)    not null,
    port            smallint        not null,
    target_group    text            not null,
    updated_at      timestamptz     not null default now(),
    last_error      varchar(128)    default null,
    status          boolean,
    primary key (real_ip, port, target_group)
);