BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

-- 旧卡默认未填写；旧钱包地址保留，不将文本猜测转换成二维码。
ALTER TABLE bank_cards
  ADD COLUMN IF NOT EXISTS cvc_ciphertext TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS wallet_qr_image TEXT NOT NULL DEFAULT '' CHECK (octet_length(wallet_qr_image)<=2800000);
COMMENT ON TABLE bank_cards IS '银行卡：加密卡号及安全码、平台、备注、钱包地址二维码截图、历史钱包地址、USD 美分余额、冻结金额、状态和限额；图片仅供人工转账使用，不自动付款。';
COMMIT;
