-- 第 6 层：撮合结果表（落库目的地）
-- 运行：Get-Content sql/result.sql -Raw | mysql -uroot -p123456 market_match

CREATE TABLE IF NOT EXISTS t_exchange_match_result_test (
  f_id       BIGINT UNSIGNED NOT NULL,               -- 主键 = 输入订单 SeqId（确定性！幂等落库的根基）
  ts         BIGINT NOT NULL,                        -- 毫秒时间戳
  order_type VARCHAR(32) NOT NULL,                   -- buy-limit / sell-market / submit-cancel / ...
  role       VARCHAR(32) NOT NULL,                   -- taker / maker / cancel
  mr         LONGTEXT NOT NULL,                      -- 完整 MatchResult JSON（给下游行情/账务消费）
  PRIMARY KEY (f_id),
  KEY idx_ts (ts),
  KEY idx_role (role)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
