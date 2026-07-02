package newsletter

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
)

func GetNewsletterHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		newsletter, err := svc.GetNewsletter(r.Context(), userID)
		if err != nil {
			log.Printf("newsletter: GetNewsletter error: %v", err)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(newsletter)
		if err != nil {
			log.Printf("newsletter: error encoding response: %v", err)
			return
		}
	}
}
