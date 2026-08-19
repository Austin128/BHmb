-- MySQL 方言覆盖：DATETIME(3) 避免 TIMESTAMP 的 2038 上限与隐式 ON UPDATE 行为，
-- TINYINT(1) 承载布尔，并显式指定引擎与字符集。
CREATE TABLE `sys_tenant` (
  `id` BIGINT NOT NULL,
  `code` VARCHAR(64) NOT NULL,
  `name` VARCHAR(128) NOT NULL,
  `status` VARCHAR(32) NOT NULL DEFAULT 'active',
  `expire_at` DATETIME(3) NULL,
  `quota_json` TEXT,
  `contact_name` VARCHAR(64) NOT NULL DEFAULT '',
  `contact_email` VARCHAR(128) NOT NULL DEFAULT '',
  `contact_mobile` VARCHAR(32) NOT NULL DEFAULT '',
  `remark` VARCHAR(255) NOT NULL DEFAULT '',
  `created_at` DATETIME(3) NOT NULL,
  `updated_at` DATETIME(3) NOT NULL,
  `deleted_at` DATETIME(3) NULL,
  `created_by` BIGINT NOT NULL DEFAULT 0,
  `tenant_id` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_tenant_code` (`code`,`deleted_at`),
  KEY `idx_tenant_status` (`status`,`expire_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户表';

CREATE TABLE `sys_setting` (
  `id` BIGINT NOT NULL,
  `group_key` VARCHAR(64) NOT NULL,
  `item_key` VARCHAR(128) NOT NULL,
  `item_value` TEXT,
  `value_type` VARCHAR(32) NOT NULL DEFAULT 'string',
  `default_value` TEXT,
  `title` VARCHAR(128) NOT NULL DEFAULT '',
  `description` VARCHAR(512) NOT NULL DEFAULT '',
  `is_encrypted` TINYINT(1) NOT NULL DEFAULT 0,
  `is_public` TINYINT(1) NOT NULL DEFAULT 0,
  `sort` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME(3) NOT NULL,
  `updated_at` DATETIME(3) NOT NULL,
  `created_by` BIGINT NOT NULL DEFAULT 0,
  `tenant_id` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_setting_key` (`tenant_id`,`item_key`),
  KEY `idx_setting_group` (`group_key`,`sort`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='系统设置表';
