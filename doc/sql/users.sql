-- d2brain.users 定义

CREATE TABLE `users` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '自增主键',
  `no` varchar(6) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT '6位用户编号（1字母+5数字）',
  `sn` varchar(58) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT '用户序列号，如 u-xxx-xxx',
  `tel` varchar(18) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '' COMMENT '手机号，空=未验证',
  `nickname` varchar(38) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT '默认昵称',
  `password` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT '密码: MD5(rawPassword + salt)',
  `salt` char(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL COMMENT '密码盐值（16字节随机 -> 32 hex）',
  `deleted` tinyint(1) NOT NULL DEFAULT '0' COMMENT '软删除: 0=正常, 1=已删除',
  `settings_ver` int NOT NULL DEFAULT '0',
  `settings` json NOT NULL DEFAULT (_utf8mb4'{}') COMMENT '用户设置（JSON 对象）',
  `create_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `update_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_no` (`no`),
  UNIQUE KEY `uk_sn` (`sn`),
  KEY `users_tel_IDX` (`tel`) USING BTREE
) ENGINE=InnoDB AUTO_INCREMENT=3 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户账户表';