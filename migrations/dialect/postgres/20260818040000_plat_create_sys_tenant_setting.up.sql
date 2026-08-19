-- PostgreSQL 方言覆盖：TIMESTAMP(3) 与 BOOLEAN，索引名在 schema 内全局唯一。
CREATE TABLE sys_tenant (
  id BIGINT NOT NULL,
  code VARCHAR(64) NOT NULL,
  name VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  expire_at TIMESTAMP(3) NULL,
  quota_json TEXT,
  contact_name VARCHAR(64) NOT NULL DEFAULT '',
  contact_email VARCHAR(128) NOT NULL DEFAULT '',
  contact_mobile VARCHAR(32) NOT NULL DEFAULT '',
  remark VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3) NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_tenant_code ON sys_tenant (code, deleted_at);
CREATE INDEX idx_tenant_status ON sys_tenant (status, expire_at);

CREATE TABLE sys_setting (
  id BIGINT NOT NULL,
  group_key VARCHAR(64) NOT NULL,
  item_key VARCHAR(128) NOT NULL,
  item_value TEXT,
  value_type VARCHAR(32) NOT NULL DEFAULT 'string',
  default_value TEXT,
  title VARCHAR(128) NOT NULL DEFAULT '',
  description VARCHAR(512) NOT NULL DEFAULT '',
  is_encrypted BOOLEAN NOT NULL DEFAULT FALSE,
  is_public BOOLEAN NOT NULL DEFAULT FALSE,
  sort INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
);

CREATE UNIQUE INDEX uk_setting_key ON sys_setting (tenant_id, item_key);
CREATE INDEX idx_setting_group ON sys_setting (group_key, sort);
