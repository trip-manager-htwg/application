module github.com/trip-manager-htwg/application/backend/shared/authclient

go 1.25.8

require (
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb" v0.0.0
)

replace (
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb" => ../tenantdb
)