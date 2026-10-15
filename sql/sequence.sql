-- 第 5 层建表脚本：订单序列表（照抄原版 aibit_spot_sequence 结构）
-- 运行：mysql -uroot -p < sql/sequence.sql

CREATE DATABASE IF NOT EXISTS market_match DEFAULT CHARSET utf8mb4;
USE market_match;

CREATE TABLE IF NOT EXISTS aibit_spot_sequence_test (
  id           BIGINT UNSIGNED NOT NULL,      -- 序列号：全局唯一顺序，SeqId 的来源，也是拉单断点
  type         INT NOT NULL,                  -- 订单类型编码（见 puller.go 常量：0撤单/1市价买/2市价卖/3限价买/4限价卖/7FOK买/8FOK卖）
  order_id     BIGINT NOT NULL,               -- 用户看到的订单号（撤单时按它找目标）
  amount       DECIMAL(36,18) NOT NULL,       -- 数量；市价买单这里存的是"钱"
  price        DECIMAL(36,18) NOT NULL,       -- 价格；市价单填 0
  circuit_rate DECIMAL(36,18) NOT NULL DEFAULT 0, -- 熔断比例，0=不启用
  created_at   BIGINT NOT NULL,               -- 毫秒时间戳
  user_id      BIGINT NOT NULL,
  stp          TINYINT NOT NULL DEFAULT 0,    -- 自成交保护策略（本版未实现，留位）
  taker        VARCHAR(32) NOT NULL DEFAULT '',
  maker        VARCHAR(32) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY idx_user (user_id),
  KEY idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
