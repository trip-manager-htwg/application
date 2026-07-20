CREATE TABLE IF NOT EXISTS newsletters
(
    firebase_uid VARCHAR(255) NOT NULL,
    tenant_id    VARCHAR(255) NOT NULL,
    content      JSONB        NOT NULL,
    generated_at TIMESTAMP    NOT NULL DEFAULT NOW(),
    PRIMARY KEY (firebase_uid, tenant_id)
);