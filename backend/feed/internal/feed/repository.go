package feed

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	openapi_types "github.com/oapi-codegen/runtime/types"
	generated "github.com/trip-manager-htwg/application/backend/feed/generated"
)

type Repository interface {
	GetFeed(ctx context.Context, tenantID string, limit, offset int) ([]generated.FeedTrip, int, error)
	GetPersonalizedFeed(ctx context.Context, tenantID, userID string, limit, offset int) ([]generated.FeedTrip, int, error)
}

type repository struct {
	driver neo4j.DriverWithContext
}

func NewRepository(driver neo4j.DriverWithContext) Repository {
	return &repository{driver: driver}
}

func (r *repository) GetFeed(ctx context.Context, tenantID string, limit, offset int) ([]generated.FeedTrip, int, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer func(session neo4j.SessionWithContext, ctx context.Context) {
		_ = session.Close(ctx)
	}(session, ctx)

	result, err := session.Run(ctx, `
		MATCH (t:Trip {tenantId: $tenantId})
		OPTIONAL MATCH (t)<-[:CREATED]-(creator:User)
		OPTIONAL MATCH (t)<-[l:LIKED]-()
		OPTIONAL MATCH (t)<-[c:COMMENTED]-()
		WITH t, creator,
		     count(DISTINCT l) AS likes,
		     count(DISTINCT c) AS comments
		WITH t, creator, likes, comments,
		     toFloat(likes * 2 + comments) / (duration.inSeconds(datetime(t.createdAt), datetime()).seconds / 3600.0 + 2)^1.5 AS score
		RETURN t.id        AS tripId,
		       t.title     AS title,
		       t.createdAt AS createdAt,
		       coalesce(creator.id, '') AS creatorId,
		       likes, comments, toFloat(score) AS score
		ORDER BY score DESC, t.createdAt DESC
		SKIP $offset
		LIMIT $limit
	`, map[string]any{"tenantId": tenantID, "limit": limit, "offset": offset})
	if err != nil {
		return nil, 0, err
	}

	trips, err := collectTrips(ctx, result)
	if err != nil {
		return nil, 0, err
	}

	total, err := countTrips(ctx, session, tenantID)
	if err != nil {
		return nil, 0, err
	}

	return trips, total, nil
}

func (r *repository) GetPersonalizedFeed(ctx context.Context, tenantID, userID string, limit, offset int) ([]generated.FeedTrip, int, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer func(session neo4j.SessionWithContext, ctx context.Context) {
		_ = session.Close(ctx)
	}(session, ctx)

	result, err := session.Run(ctx, `
		MATCH (me:User {id: $userId, tenantId: $tenantId})
		MATCH (t:Trip {tenantId: $tenantId})
		WHERE NOT (me)-[:LIKED]->(t) AND NOT (me)-[:CREATED]->(t)
		OPTIONAL MATCH (t)<-[:CREATED]-(creator:User)
		OPTIONAL MATCH (t)<-[l:LIKED]-()
		OPTIONAL MATCH (t)<-[c:COMMENTED]-()
		WITH me, t, creator,
		     count(DISTINCT l) AS likes,
		     count(DISTINCT c) AS comments
		OPTIONAL MATCH (me)-[:LIKED|:COMMENTED]->(myInteraction:Trip)<-[:CREATED]-(creator)
		WITH me, t, creator, likes, comments,
		     count(DISTINCT myInteraction) AS creatorScore
		OPTIONAL MATCH (me)-[:LIKED]->(:Trip)<-[:CREATED]-(followed:User)-[:LIKED]->(t)
		WITH me, t, creator, likes, comments, creatorScore,
		     count(DISTINCT followed) AS socialGraphScore
		OPTIONAL MATCH (me)-[:LIKED]->(common:Trip)<-[:LIKED]-(similar:User)-[:LIKED]->(t)
		WITH t, creator, likes, comments, creatorScore, socialGraphScore,
		     count(DISTINCT similar) AS collaborativeScore
		WITH t, creator, likes, comments,
		     (toFloat(creatorScore) * 4.0 + toFloat(socialGraphScore) * 2.0 + toFloat(collaborativeScore) * 1.0) AS score
		WHERE score > 0
		RETURN t.id        AS tripId,
		       t.title     AS title,
		       t.createdAt AS createdAt,
		       coalesce(creator.id, '') AS creatorId,
		       likes, comments, toFloat(score) AS score
		ORDER BY score DESC, t.createdAt DESC
		SKIP $offset
		LIMIT $limit
	`, map[string]any{
		"userId":   userID,
		"tenantId": tenantID,
		"limit":    limit,
		"offset":   offset,
	})
	if err != nil {
		return nil, 0, err
	}

	trips, err := collectTrips(ctx, result)
	if err != nil {
		return nil, 0, err
	}

	total := offset + len(trips)
	if len(trips) == limit {
		total = offset + limit + 1
	}

	return trips, total, nil
}

func collectTrips(ctx context.Context, result neo4j.ResultWithContext) ([]generated.FeedTrip, error) {
	var trips []generated.FeedTrip
	for result.Next(ctx) {
		rec := result.Record()
		trips = append(trips, generated.FeedTrip{
			TripId:    uuidVal(rec, "tripId"),
			Title:     stringVal(rec, "title"),
			CreatedAt: timeVal(rec, "createdAt"),
			CreatorId: uuidVal(rec, "creatorId"),
			Likes:     int64Val(rec, "likes"),
			Comments:  int64Val(rec, "comments"),
			Score:     float32Val(rec, "score"),
		})
	}
	return trips, result.Err()
}

func countTrips(ctx context.Context, session neo4j.SessionWithContext, tenantID string) (int, error) {
	countResult, err := session.Run(ctx,
		`MATCH (t:Trip {tenantId: $tenantId}) RETURN count(t) AS total`,
		map[string]any{"tenantId": tenantID},
	)
	if err != nil {
		return 0, err
	}
	if countResult.Next(ctx) {
		if v, ok := countResult.Record().Get("total"); ok {
			if n, ok := v.(int64); ok {
				return int(n), nil
			}
		}
	}
	return 0, nil
}

func stringVal(rec *neo4j.Record, key string) string {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func int64Val(rec *neo4j.Record, key string) int64 {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return 0
	}
	n, _ := v.(int64)
	return n
}

func float32Val(rec *neo4j.Record, key string) float32 {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0
		}
		return float32(n)
	case int64:
		return float32(n)
	}
	return 0
}

func uuidVal(rec *neo4j.Record, key string) openapi_types.UUID {
	s := stringVal(rec, key)
	id, err := uuid.Parse(s)
	if err != nil {
		return openapi_types.UUID{}
	}
	return openapi_types.UUID(id)
}

func timeVal(rec *neo4j.Record, key string) time.Time {
	v, ok := rec.Get(key)
	if !ok || v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return time.Time{}
		}
		return parsed
	}
	return time.Time{}
}
