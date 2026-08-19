-- MySQL 方言覆盖：RBAC 四表。
CREATE TABLE `sys_role` (
  `id` BIGINT NOT NULL,
  `code` VARCHAR(64) NOT NULL,
  `name` VARCHAR(64) NOT NULL,
  `description` VARCHAR(255) NOT NULL DEFAULT '',
  `is_builtin` TINYINT(1) NOT NULL DEFAULT 0,
  `data_scope` VARCHAR(32) NOT NULL DEFAULT 'self',
  `scope_node_json` TEXT,
  `sort` INT NOT NULL DEFAULT 0,
  `status` VARCHAR(32) NOT NULL DEFAULT 'active',
  `created_at` DATETIME(3) NOT NULL,
  `updated_at` DATETIME(3) NOT NULL,
  `deleted_at` DATETIME(3) NULL,
  `created_by` BIGINT NOT NULL DEFAULT 0,
  `tenant_id` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_role_code` (`tenant_id`,`code`,`deleted_at`),
  KEY `idx_role_tenant` (`tenant_id`,`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='角色表';

CREATE TABLE `sys_user_role` (
  `id` BIGINT NOT NULL,
  `user_id` BIGINT NOT NULL,
  `role_id` BIGINT NOT NULL,
  `created_at` DATETIME(3) NOT NULL,
  `created_by` BIGINT NOT NULL DEFAULT 0,
  `tenant_id` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_ur_user_role` (`user_id`,`role_id`),
  KEY `idx_ur_role` (`role_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户角色关联表';

CREATE TABLE `sys_permission` (
  `id` BIGINT NOT NULL,
  `code` VARCHAR(128) NOT NULL,
  `name` VARCHAR(64) NOT NULL,
  `module` VARCHAR(32) NOT NULL,
  `resource` VARCHAR(64) NOT NULL,
  `action` VARCHAR(32) NOT NULL,
  `api_path` VARCHAR(255) NOT NULL DEFAULT '',
  `http_method` VARCHAR(16) NOT NULL DEFAULT '',
  `is_sensitive` TINYINT(1) NOT NULL DEFAULT 0,
  `need_audit` TINYINT(1) NOT NULL DEFAULT 1,
  `sort` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME(3) NOT NULL,
  `updated_at` DATETIME(3) NOT NULL,
  `created_by` BIGINT NOT NULL DEFAULT 0,
  `tenant_id` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_perm_code` (`code`),
  KEY `idx_perm_module` (`module`,`resource`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='权限点表';

CREATE TABLE `sys_role_permission` (
  `id` BIGINT NOT NULL,
  `role_id` BIGINT NOT NULL,
  `permission_id` BIGINT NOT NULL,
  `permission_code` VARCHAR(128) NOT NULL,
  `created_at` DATETIME(3) NOT NULL,
  `created_by` BIGINT NOT NULL DEFAULT 0,
  `tenant_id` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_rp_role_perm` (`role_id`,`permission_id`),
  KEY `idx_rp_code` (`permission_code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='角色权限关联表';
