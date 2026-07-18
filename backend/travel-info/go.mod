module github.com/trip-manager-htwg/application/backend/travel-info

go 1.25.8

require (
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/redis/go-redis/v9 v9.19.0
	github.com/trip-manager-htwg/application/backend/shared/middleware v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
)

replace github.com/trip-manager-htwg/application/backend/shared/middleware => ../shared/middleware
