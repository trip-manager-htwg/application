package authclient

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
	"net/http"
)

type contextKey string

const (
	contextKeyUserID     contextKey = "userId"
	contextKeyUserEmail  contextKey = "userEmail"
	contextKeyUserClaims contextKey = "userClaims"
	contextKeyTenantID   contextKey = "tenantId"
	contextKeyUserRole   contextKey = "userRole"
)

func RequireAuth(client *Client) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				respondError(w, http.StatusUnauthorized, "authorization header required")
				return
			}
			result, err := client.ValidateBearerToken(r.Context(), authHeader)
			if err != nil {
				respondError(w, http.StatusUnauthorized, err.Error())
				return
			}
			if !result.Valid {
				respondError(w, http.StatusUnauthorized, result.Error)
				return
			}

			tenantID := extractTenantID(result.Claims)
			role := extractRole(result.Claims)

			ctx := context.WithValue(r.Context(), contextKeyUserID, result.UserID)
			ctx = context.WithValue(ctx, contextKeyUserEmail, result.Email)
			ctx = context.WithValue(ctx, contextKeyUserClaims, result.Claims)
			ctx = context.WithValue(ctx, contextKeyTenantID, tenantID)
			ctx = context.WithValue(ctx, contextKeyUserRole, role)
			ctx = tenantdb.WithTenantID(ctx, tenantID) // ← neu

			next(w, r.WithContext(ctx))
		}
	}
}

func OptionalAuth(client *Client) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			var userID, userEmail string
			if authHeader != "" {
				result, err := client.ValidateBearerToken(r.Context(), authHeader)
				if err == nil && result.Valid {
					userID = result.UserID
					userEmail = result.Email
				}
			}
			ctx := context.WithValue(r.Context(), contextKeyUserID, userID)
			ctx = context.WithValue(ctx, contextKeyUserEmail, userEmail)
			next(w, r.WithContext(ctx))
		}
	}
}

func GetUserID(r *http.Request) (string, bool) {
	userID, ok := r.Context().Value(contextKeyUserID).(string)
	return userID, ok && userID != ""
}

func GetUserEmail(r *http.Request) (string, bool) {
	email, ok := r.Context().Value(contextKeyUserEmail).(string)
	return email, ok && email != ""
}

func GetUserClaims(r *http.Request) map[string]interface{} {
	claims, ok := r.Context().Value(contextKeyUserClaims).(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}
	return claims
}

func respondError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	err := json.NewEncoder(w).Encode(map[string]string{"error": message})
	if err != nil {
		fmt.Println(err)
	}
}

func extractTenantID(claims map[string]interface{}) string {
	if claims == nil {
		return "default"
	}
	if v, ok := claims["tenant_id"].(string); ok && v != "" {
		return v
	}
	return "default"
}

func extractRole(claims map[string]interface{}) string {
	if claims == nil {
		return "tenant_member"
	}
	if v, ok := claims["role"].(string); ok && v != "" { // ← "role" statt "tenant_id"
		return v
	}
	return "tenant_member"
}

func GetTenantID(r *http.Request) string {
	tenantID, ok := r.Context().Value(contextKeyTenantID).(string)
	if !ok || tenantID == "" {
		return "default"
	}
	return tenantID
}

func GetUserRole(r *http.Request) string {
	role, ok := r.Context().Value(contextKeyUserRole).(string)
	if !ok || role == "" {
		return "tenant_member"
	}
	return role
}

func RequireTenantAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := GetUserRole(r)
		if role != "tenant_admin" && role != "tenant_owner" && role != "platform_admin" {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}
		next(w, r)
	}
}

func RequirePlatformAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role := GetUserRole(r)
		if role != "platform_admin" {
			respondError(w, http.StatusForbidden, "permission denied")
			return
		}
		next(w, r)
	}
}
