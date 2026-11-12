-- 题目 01 订单交易系统 DDL（MySQL 8，utf8mb4）
CREATE TABLE IF NOT EXISTS product (
  id          VARCHAR(32)  NOT NULL PRIMARY KEY,
  name        VARCHAR(128) NOT NULL,
  price       BIGINT       NOT NULL COMMENT '单价（分）',
  stock       INT          NOT NULL COMMENT '可售数量',
  on_sale     TINYINT(1)   NOT NULL DEFAULT 1,
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS stock_flow (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  product_id  VARCHAR(32) NOT NULL,
  order_no    VARCHAR(32) NOT NULL,
  change_type VARCHAR(16) NOT NULL COMMENT 'occupy/release/restore',
  qty         INT         NOT NULL COMMENT '变动数量（负为占用）',
  stock_after INT         NOT NULL,
  created_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_product (product_id),
  KEY idx_order (order_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS orders (
  order_no     VARCHAR(32) NOT NULL PRIMARY KEY,
  buyer_id     VARCHAR(32) NOT NULL,
  product_id   VARCHAR(32) NOT NULL,
  product_name VARCHAR(128) NOT NULL COMMENT '下单时快照',
  price        BIGINT      NOT NULL COMMENT '下单时单价快照（分）',
  qty          INT         NOT NULL,
  amount       BIGINT      NOT NULL COMMENT '订单金额（分）',
  status       VARCHAR(20) NOT NULL,
  created_at   DATETIME    NOT NULL,
  pay_deadline DATETIME    NOT NULL,
  paid_at      DATETIME    NULL,
  closed_at    DATETIME    NULL,
  close_reason VARCHAR(20) NULL,
  KEY idx_buyer (buyer_id),
  KEY idx_status_deadline (status, pay_deadline)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS payment (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  order_no   VARCHAR(32) NOT NULL,
  trade_no   VARCHAR(64) NOT NULL UNIQUE COMMENT '渠道交易号，全局唯一',
  amount     BIGINT      NOT NULL,
  status     VARCHAR(16) NOT NULL DEFAULT 'success',
  created_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_order (order_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS pay_notify_log (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  notify_id  VARCHAR(64) NOT NULL,
  order_no   VARCHAR(32) NOT NULL,
  trade_no   VARCHAR(64) NOT NULL,
  amount     BIGINT      NOT NULL,
  verify_ok  TINYINT(1)  NOT NULL,
  result     VARCHAR(20) NOT NULL COMMENT 'success/duplicate/amount_mismatch/unknown_order/bad_sign/late/conflict',
  raw        TEXT        NULL,
  created_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_order (order_no),
  KEY idx_trade (trade_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS refund (
  refund_no         VARCHAR(32) NOT NULL PRIMARY KEY,
  order_no          VARCHAR(32) NOT NULL,
  amount            BIGINT      NOT NULL,
  type              VARCHAR(16) NOT NULL COMMENT 'presale/late',
  status            VARCHAR(20) NOT NULL,
  reason            VARCHAR(255) NOT NULL DEFAULT '',
  cs_note           VARCHAR(255) NOT NULL DEFAULT '',
  channel_refund_no VARCHAR(64) NOT NULL DEFAULT '',
  attempt           INT         NOT NULL DEFAULT 0,
  created_at        DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at        DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_order (order_no),
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS refund_notify_log (
  id                BIGINT AUTO_INCREMENT PRIMARY KEY,
  notify_id         VARCHAR(64) NOT NULL,
  refund_no         VARCHAR(32) NOT NULL,
  channel_refund_no VARCHAR(64) NOT NULL,
  amount            BIGINT      NOT NULL,
  verify_ok         TINYINT(1)  NOT NULL,
  result            VARCHAR(20) NOT NULL,
  raw               TEXT        NULL,
  created_at        DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_refund (refund_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS shipment (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  order_no    VARCHAR(32) NOT NULL UNIQUE COMMENT '每单至多一条',
  carrier     VARCHAR(64) NOT NULL,
  tracking_no VARCHAR(64) NOT NULL,
  operator    VARCHAR(32) NOT NULL,
  created_at  DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS exception_payment (
  id           BIGINT AUTO_INCREMENT PRIMARY KEY,
  order_no     VARCHAR(32) NOT NULL,
  trade_no     VARCHAR(64) NOT NULL UNIQUE COMMENT '迟到通知幂等键',
  amount       BIGINT      NOT NULL,
  status       VARCHAR(16) NOT NULL DEFAULT 'pending' COMMENT 'pending/refunding/refunded',
  handler      VARCHAR(32) NULL,
  handled_at   DATETIME    NULL,
  refund_no    VARCHAR(32) NULL,
  created_at   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS user (
  id            VARCHAR(32) NOT NULL PRIMARY KEY,
  username      VARCHAR(64) NOT NULL UNIQUE,
  password_hash VARCHAR(128) NOT NULL,
  name          VARCHAR(64) NOT NULL,
  role          VARCHAR(16) NOT NULL COMMENT 'buyer/op/wh/cs',
  created_at    DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
