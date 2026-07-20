CREATE TABLE IF NOT EXISTS advertiser_insights
(
    advertiser_id VARCHAR(255) NOT NULL,
    tenant_id     VARCHAR(255) NOT NULL,
    content       JSONB        NOT NULL,
    generated_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
    PRIMARY KEY (advertiser_id, tenant_id)
);
CREATE INDEX IF NOT EXISTS idx_advertiser_insights_advertiser ON advertiser_insights (advertiser_id);
