-- 站点设置与伪静态（docs/07 7.8、7.10.1）。MySQL 方言。
ALTER TABLE `web_site` ADD COLUMN `settings_json` TEXT COMMENT '站点级设置 JSON：目录浏览/缓存/防盗链/访问限制/重定向';

CREATE TABLE `web_rewrite_rule` (
  `id` BIGINT NOT NULL,
  `site_id` BIGINT NOT NULL COMMENT '所属网站',
  `name` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '规则名称',
  `template_code` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '内置模板或 custom',
  `content` TEXT NOT NULL COMMENT '规则正文（nginx rewrite 片段）',
  `enabled` TINYINT(1) NOT NULL DEFAULT 1,
  `sort` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME(3) NOT NULL,
  `updated_at` DATETIME(3) NOT NULL,
  `deleted_at` DATETIME(3) NULL,
  `created_by` BIGINT NOT NULL DEFAULT 0,
  `tenant_id` BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_rewrite_site` (`site_id`,`enabled`,`sort`),
  UNIQUE KEY `uk_rewrite_site` (`site_id`,`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='伪静态规则表';
