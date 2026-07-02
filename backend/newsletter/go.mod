module github.com/trip-manager-htwg/application/backend/newsletter

go 1.25.8

require (
	github.com/trip-manager-htwg/application/backend/shared/authclient v0.0.0
	github.com/trip-manager-htwg/application/backend/shared/middleware v0.0.0-20260602001039-6c776cc628c5
	github.com/trip-manager-htwg/application/backend/shared/email v0.0.0
	github.com/jmoiron/sqlx v1.4.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/lib/pq v1.10.9
	github.com/neo4j/neo4j-go-driver/v5 v5.26.0
	github.com/oapi-codegen/runtime v1.4.2
	github.com/robfig/cron/v3 v3.0.1
)

require (
	github.com/trip-manager-htwg/application/backend/shared/tenantdb v0.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
)

replace (
    github.com/trip-manager-htwg/application/backend/shared/email => ../shared/email
	github.com/trip-manager-htwg/application/backend/shared/authclient => ../shared/authclient
	github.com/trip-manager-htwg/application/backend/shared/tenantdb => ../shared/tenantdb
)
