# Auth Client

Simple HTTP Client für den Auth Service. Wird in anderen Services verwendet um Tokens zu validieren.

## Installation

Im Social Service (oder anderen Services) `go.mod` hinzufügen:

## Usage

### Basic Usage

```go
import "github.com/trip-manager-htwg/application/backend/shared/authclient"

// Create client
authClient := authclient.NewClient("http://auth:8082")

// Validate token
result, err := authClient.ValidateToken(ctx, "firebase-token")
if !result.Valid {
  // Token invalid
}

// Use UserID
userID := result.UserID
```

### In Middleware (RequireAuth)

```go
import (
  "net/http"
  "github.com/trip-manager-htwg/application/backend/shared/authclient"
)

// Setup
authClient := authclient.NewClient("http://auth:8082")

// Register an auth-required endpoint
mux.HandleFunc("POST /entities/{id}/likes",
  authclient.RequireAuth(authClient)(handler.LikeEntityHandler))

// In handler: extract userID from context
func LikeEntityHandler(w http.ResponseWriter, r *http.Request) {
  userID := authclient.GetUserID(r)
  if userID == "" {
    // This shouldn't happen with RequireAuth
    return
  }
  
  // Process with authenticated userID
  err := svc.LikeEntity(r.Context(), userID, entityID)
  // ...
}
```

### Optional Auth (OptionalAuth)

```go
// Endpoint wo auth optional ist
mux.HandleFunc("GET /entities/{id}",
  authclient.OptionalAuth(authClient)(handler.GetEntityHandler))

// In handler: userID kann leer sein
func GetEntityHandler(w http.ResponseWriter, r *http.Request) {
  userID := authclient.GetUserID(r)
  // userID ist "" wenn nicht authenticated, aber das ist OK
  
  // Get public data
  resp, err := svc.GetEntity(r.Context(), entityID, userID)
  // ...
}
```

### Manual Token Validation

```go
// Get token from request
authHeader := r.Header.Get("Authorization")

// Validate
result, err := authClient.ValidateBearerToken(ctx, authHeader)
if err != nil || !result.Valid {
  http.Error(w, "unauthorized", http.StatusUnauthorized)
  return
}

// Access user info
userID := result.UserID
email := result.Email
claims := result.Claims
```

## Available Methods

### Client Methods

- `NewClient(baseURL string) *Client` - Create client
- `ValidateToken(ctx, token string) (*TokenValidationResponse, error)` - Validate token
- `ValidateTokenFromHeader(ctx, authHeader string) (*TokenValidationResponse, error)` - Validate from header
- `ValidateBearerToken(ctx, authHeader string) (*TokenValidationResponse, error)` - Parse "Bearer <token>" and validate
- `GetUserIDFromHeader(ctx, authHeader string) (string, error)` - Quick helpers to get just UserID
- `HealthCheck(ctx context.Context) error` - Check if auth service is running

### Middleware Functions

- `RequireAuth(authClient) func(handler) handler` - Enforces authentication
- `OptionalAuth(authClient) func(handler) handler` - Auth optional

### Context Helpers

- `GetUserID(r *http.Request) string` - Get userID from context
- `GetUserEmail(r *http.Request) string` - Get email from context
- `GetUserClaims(r *http.Request) map[string]interface{}` - Get all claims

## Example: Social Service Integration

```go
// social/cmd/api/main.go
package main

import (
  "github.com/trip-manager-htwg/application/backend/auth/pkg/authclient"
  "github.com/trip-manager-htwg/application//internal/comment"
  "github.com/trip-manager-htwg/application//internal/like"
  // ...
)

func main() {
  // ... setup ...
  
  // Create auth client
  authClient := authclient.NewClient("http://auth:8082")
  
  // Create handlers
  likeHandler := like.NewHandler(likeService)
  commentHandler := comment.NewHandler(commentService)
  
  // Register routes with auth
  mux := http.NewServeMux()
  
  // Require auth
  mux.HandleFunc("POST /entities/{id}/likes",
    authclient.RequireAuth(authClient)(likeHandler.LikeEntity))
  mux.HandleFunc("DELETE /entities/{id}/likes",
    authclient.RequireAuth(authClient)(likeHandler.UnlikeEntity))
  
  // Optional auth
  mux.HandleFunc("GET /entities/{id}/likes",
    authclient.OptionalAuth(authClient)(likeHandler.GetEntityLikes))
  
  mux.HandleFunc("GET /entities/{id}/comments",
    authclient.OptionalAuth(authClient)(commentHandler.ListComments))
  mux.HandleFunc("POST /entities/{id}/comments",
    authclient.RequireAuth(authClient)(commentHandler.CreateComment))
  
  // ... start server ...
}
```

## Configuration

Auth Service URL kann von überall kommen:
- Hardcoded: `authclient.NewClient("http://auth:8082")`
- Env-Variable: `authclient.NewClient(os.Getenv("AUTH_SERVICE_URL"))`
- Config: `authclient.NewClient(cfg.AuthServiceURL)`

## Error Handling

```go
result, err := authClient.ValidateToken(ctx, token)
if err != nil {
  // Network/connection error
  log.Printf("Failed to call auth service: %v", err)
  return
}

if !result.Valid {
  // Token invalid but auth service working
  log.Printf("Invalid token: %s", result.Error)
  return
}

// Token valid!
userID := result.UserID
```

