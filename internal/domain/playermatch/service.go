package playermatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/contracts"
	"github.com/aneviaro/stratz-mcp/internal/domain/batch"
	"github.com/aneviaro/stratz-mcp/internal/domain/pagination"
	"github.com/aneviaro/stratz-mcp/internal/graphql/generated"
	"github.com/aneviaro/stratz-mcp/internal/stratz"
)

const (
	playerListOperationVersion   = "matches-player-history/v3"
	fullMatchAvailabilityWarning = "Fight and economy breakdowns are unavailable from the current STRATZ match playback data"
	heroNameUnavailableWarning   = "Hero names are unavailable for this response because the STRATZ constants request failed or the per-call request budget was exhausted"
	detailLevelPlayers           = contracts.DetailLevel("players")
)

// HeroNamer resolves numeric hero IDs to localized display names from the
// STRATZ constants aggregate. It is optional: when a Service is constructed
// without one, hero-bearing rows serialize hero_name as null rather than
// failing the call.
type HeroNamer interface {
	HeroNames(ctx context.Context, ids []int64, budget *stratz.RequestBudget) (map[int64]string, []stratz.RateLimit, error)
}

// Options configures player and match domain execution.
type Options struct {
	Executor            stratz.Executor
	Token               string
	SchemaVersion       string
	MaxUpstreamRequests int
	MaxBatchSize        int
	Heroes              HeroNamer
	Now                 func() time.Time
}

// Service executes curated player and match operations.
type Service struct {
	executor            stratz.Executor
	token               string
	schemaVersion       string
	maxUpstreamRequests int
	maxBatchSize        int
	heroes              HeroNamer
	cursor              *pagination.Codec
}

// New constructs the player and match domain s.
func New(options Options) (*Service, error) {
	if options.Executor == nil {
		return nil, errors.New("STRATZ executor is required")
	}
	if strings.TrimSpace(options.Token) == "" {
		return nil, errors.New("cursor token is required")
	}
	if strings.TrimSpace(options.SchemaVersion) == "" {
		return nil, errors.New("schema version is required")
	}
	if options.MaxUpstreamRequests < 1 || options.MaxUpstreamRequests > 5 {
		return nil, errors.New("upstream request budget must be between 1 and 5")
	}
	if options.MaxBatchSize == 0 {
		options.MaxBatchSize = 25
	}
	if options.MaxBatchSize < 1 || options.MaxBatchSize > 25 {
		return nil, errors.New("batch size must be between 1 and 25")
	}
	return &Service{
		executor:            options.Executor,
		token:               options.Token,
		schemaVersion:       options.SchemaVersion,
		maxUpstreamRequests: options.MaxUpstreamRequests,
		maxBatchSize:        options.MaxBatchSize,
		heroes:              options.Heroes,
		cursor:              pagination.NewCodec(pagination.Options{Now: options.Now}),
	}, nil
}

// FetchPlayer returns one normalized player.
func (s *Service) FetchPlayer(
	ctx context.Context,
	identifier string,
) (*Result[contracts.Player], error) {
	id, err := NormalizePlayerID(identifier)
	if err != nil {
		return nil, err
	}
	budget := s.budget()
	response, err := s.execute(ctx, budget, generated.StratzGetPlayer_Operation, "StratzGetPlayer", map[string]any{
		"steamAccountId": int64(id.AccountID),
	})
	if err != nil {
		return nil, err
	}
	var envelope playerEnvelope
	if err := decodeData(response.Data, &envelope); err != nil {
		return nil, protocol("STRATZ returned an invalid player payload")
	}
	if envelope.Player == nil {
		return nil, notFound("Player was not found", map[string]any{"account_id": playerKey(id)})
	}
	if envelope.Player.IsPrivate {
		return nil, private("The requested STRATZ player profile is private")
	}
	return &Result[contracts.Player]{
		Data:       mapPlayer(envelope.Player, true),
		Raw:        rawData(response.Data),
		RateLimits: response.RateLimits,
	}, nil
}

// BatchPlayers gets up to 25 players atomically in caller order.
func (s *Service) BatchPlayers(
	ctx context.Context,
	identifiers []string,
) (*Result[[]contracts.Player], error) {
	plan, err := batch.NewPlan(identifiers, s.maxBatchSize, func(value string) (string, error) {
		id, normalizeErr := NormalizePlayerID(value)
		if normalizeErr != nil {
			return "", normalizeErr
		}
		return playerKey(id), nil
	})
	if err != nil {
		return nil, batchInputError(err)
	}
	ids := make([]int64, 0, len(plan.Unique()))
	for index, value := range plan.Unique() {
		id, normalizeErr := NormalizePlayerID(value)
		if normalizeErr != nil {
			domainErr := normalizeErr.(*Error)
			domainErr.FailedInput = failedInput(index, value)
			return nil, domainErr
		}
		ids = append(ids, int64(id.AccountID))
	}
	response, err := s.execute(ctx, s.budget(), generated.StratzGetPlayers_Operation, "StratzGetPlayers", map[string]any{
		"steamAccountIds": ids,
	})
	if err != nil {
		return nil, err
	}
	var envelope playersEnvelope
	if err := decodeData(response.Data, &envelope); err != nil {
		return nil, protocol("STRATZ returned an invalid player batch payload")
	}
	results := make(map[string]contracts.Player, len(envelope.Players))
	for _, player := range envelope.Players {
		if player == nil {
			continue
		}
		if player.IsPrivate {
			domainErr := private("A requested STRATZ player profile is private")
			domainErr.FailedInput = strconv.FormatInt(player.SteamAccountID, 10)
			return nil, domainErr
		}
		results[strconv.FormatInt(player.SteamAccountID, 10)] = mapPlayer(player, true)
	}
	items, reconstructErr := batch.Reconstruct(plan, results)
	if reconstructErr != nil {
		for index, value := range plan.Inputs() {
			id, _ := NormalizePlayerID(value)
			if _, ok := results[playerKey(id)]; !ok {
				domainErr := notFound("A requested player was not found", nil)
				domainErr.FailedInput = failedInput(index, value)
				return nil, domainErr
			}
		}
		return nil, protocol("STRATZ returned an incomplete player batch")
	}
	return &Result[[]contracts.Player]{
		Data:       items,
		Raw:        rawData(response.Data),
		RateLimits: response.RateLimits,
	}, nil
}

// QueryPlayers resolves one to 25 exact player identifiers. Search-like input is
// rejected because STRATZ has no bounded player directory source.
func (s *Service) QueryPlayers(ctx context.Context, request contracts.StratzQueryPlayersRequest) (*Result[PlayerQueryData], error) {
	input, err := playerRequestMap(request)
	if err != nil {
		return nil, invalid("Player query input is invalid", map[string]any{"reason": err.Error()})
	}
	if mode, _ := input["mode"].(string); mode != "exact" {
		return nil, invalid("Unsupported player query mode", map[string]any{"mode": input["mode"]})
	}
	for key := range input {
		switch key {
		case "mode", "player_ids", "include_profile_statistics", "fresh", "include_raw":
		default:
			return nil, invalid("Player directory search is not supported", map[string]any{"field": key})
		}
	}
	selectors, err := playerAnySlice(input["player_ids"])
	if err != nil || len(selectors) < 1 || len(selectors) > 25 {
		return nil, invalid("Player exact query requires between 1 and 25 identifiers", nil)
	}
	plan, err := batch.NewPlan(selectors, 25, func(value any) (string, error) {
		text, ok := value.(string)
		if !ok {
			return "", invalid("Player identifier must be a string", nil)
		}
		id, normalizeErr := NormalizePlayerID(text)
		if normalizeErr != nil {
			return "", normalizeErr
		}
		return playerKey(id), nil
	})
	if err != nil {
		return nil, invalid("Player exact query input is invalid", map[string]any{"reason": err.Error()})
	}
	ids := make([]int64, 0, len(plan.Unique()))
	for index, value := range plan.Unique() {
		id, normalizeErr := NormalizePlayerID(value.(string))
		if normalizeErr != nil {
			domainErr := normalizeErr.(*Error)
			domainErr.FailedInput = failedInput(index, value)
			return nil, domainErr
		}
		ids = append(ids, int64(id.AccountID))
	}
	includeStatistics, _ := input["include_profile_statistics"].(bool)
	query, operation := generated.StratzGetPlayersLean_Operation, "StratzGetPlayersLean"
	if includeStatistics {
		query, operation = generated.StratzGetPlayers_Operation, "StratzGetPlayers"
	}
	response, err := s.execute(ctx, s.budget(), query, operation, map[string]any{"steamAccountIds": ids})
	if err != nil {
		return nil, err
	}
	var envelope playersEnvelope
	if err := decodeData(response.Data, &envelope); err != nil {
		return nil, protocol("STRATZ returned an invalid player batch payload")
	}
	results := make(map[string]contracts.Player, len(envelope.Players))
	for _, player := range envelope.Players {
		if player == nil {
			continue
		}
		if player.IsPrivate {
			domainErr := private("A requested STRATZ player profile is private")
			domainErr.FailedInput = strconv.FormatInt(player.SteamAccountID, 10)
			return nil, domainErr
		}
		results[strconv.FormatInt(player.SteamAccountID, 10)] = mapPlayer(player, includeStatistics)
	}
	items, reconstructErr := batch.Reconstruct(plan, results)
	if reconstructErr != nil {
		for index, value := range plan.Inputs() {
			id, normalizeErr := NormalizePlayerID(value.(string))
			if normalizeErr == nil {
				if _, ok := results[playerKey(id)]; !ok {
					domainErr := notFound("A requested player was not found", nil)
					domainErr.FailedInput = failedInput(index, value)
					return nil, domainErr
				}
			}
		}
		return nil, protocol("STRATZ returned an incomplete player batch")
	}
	return &Result[PlayerQueryData]{
		Data: PlayerQueryData{Mode: "exact", Items: items}, Raw: rawData(response.Data), RateLimits: response.RateLimits,
	}, nil
}

func matchRequestMap(request any) (map[string]any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if value, ok := request.(map[string]any); ok {
		return value, nil
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func matchAnySlice(value any) ([]any, error) {
	if value == nil {
		return nil, errors.New("array is required")
	}
	if result, ok := value.([]any); ok {
		return result, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result []any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func queryDetail(input map[string]any, history bool) (contracts.DetailLevel, *Error) {
	value, present := input["detail_level"]
	if !present {
		if history {
			return contracts.DetailLevelSummary, nil
		}
		return contracts.DetailLevelStandard, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", invalid("detail_level must be a string", nil)
	}
	detail := contracts.DetailLevel(text)
	if _, detailErr := requireDetail(detail); detailErr != nil {
		return "", detailErr.(*Error)
	}
	if history && detail != contracts.DetailLevelSummary && detail != detailLevelPlayers {
		return "", invalid("Only summary or players detail is available for player history", nil)
	}
	return detail, nil
}

func queryPlayerMatchFilters(input map[string]any) (PlayerMatchFilters, *Error) {
	for key := range input {
		switch key {
		case "mode", "cursor", "detail_level", "fresh", "include_raw", "player_id", "from", "to", "hero", "role", "game_mode_id", "lobby_type_id", "result", "minimum_duration_seconds", "patch_id", "limit":
		default:
			return PlayerMatchFilters{}, invalid("Unsupported player-history field", map[string]any{"field": key})
		}
	}
	player, ok := input["player_id"].(string)
	if !ok || strings.TrimSpace(player) == "" {
		return PlayerMatchFilters{}, invalid("player_id is required", nil)
	}
	filters := PlayerMatchFilters{PlayerID: player, Limit: integerValue(input["limit"], 0)}
	if value, exists := input["from"]; exists {
		parsed, parseErr := queryDate(value, "from")
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.From = parsed
	}
	if value, exists := input["to"]; exists {
		parsed, parseErr := queryDate(value, "to")
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.To = parsed
	}
	if value, exists := input["hero"]; exists {
		id, parseErr := queryInteger(value, "hero")
		if parseErr != nil {
			return PlayerMatchFilters{}, parseErr
		}
		filters.HeroID = &id
	}
	if value, exists := input["role"]; exists {
		role, roleOK := value.(string)
		if !roleOK || strings.TrimSpace(role) == "" {
			return PlayerMatchFilters{}, invalid("role must be a non-empty string", nil)
		}
		filters.Role = &role
	}
	for key, target := range map[string]**int64{"game_mode_id": &filters.GameModeID, "lobby_type_id": &filters.LobbyTypeID, "minimum_duration_seconds": &filters.MinimumDurationSeconds} {
		if value, exists := input[key]; exists {
			parsed, parseErr := queryInteger(value, key)
			if parseErr != nil {
				return PlayerMatchFilters{}, parseErr
			}
			*target = &parsed
		}
	}
	if value, exists := input["result"]; exists {
		result, resultOK := value.(string)
		if !resultOK || (result != "win" && result != "loss") {
			return PlayerMatchFilters{}, invalid("result must be win or loss", nil)
		}
		filters.Result = &result
	}
	if value, exists := input["patch_id"]; exists {
		patch, patchOK := value.(string)
		if !patchOK || strings.TrimSpace(patch) == "" {
			return PlayerMatchFilters{}, invalid("patch_id must be a non-empty string", nil)
		}
		filters.PatchID = &patch
	}
	return filters, nil
}

func queryDate(value any, field string) (*time.Time, *Error) {
	text, ok := value.(string)
	if !ok {
		return nil, invalid(field+" must be an RFC3339 date-time", nil)
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return nil, invalid(field+" must be an RFC3339 date-time", nil)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func queryInteger(value any, field string) (int64, *Error) {
	switch number := value.(type) {
	case int:
		return int64(number), nil
	case int64:
		return number, nil
	case float64:
		if number != float64(int64(number)) {
			return 0, invalid(field+" must be an integer", nil)
		}
		return int64(number), nil
	case json.Number:
		parsed, err := number.Int64()
		if err == nil {
			return parsed, nil
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(number), 10, 64)
		if err == nil {
			return parsed, nil
		}
	}
	return 0, invalid(field+" must be an integer", nil)
}

func playerRequestMap(request any) (map[string]any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if value, ok := request.(map[string]any); ok {
		return value, nil
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func playerAnySlice(value any) ([]any, error) {
	if value == nil {
		return nil, errors.New("array is required")
	}
	if result, ok := value.([]any); ok {
		return result, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result []any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// FetchMatch returns one normalized match at the requested detail level.
func (s *Service) FetchMatch(
	ctx context.Context,
	identifier string,
	detail contracts.DetailLevel,
) (*Result[Match], error) {
	detail, err := requireDetail(detail)
	if err != nil {
		return nil, err
	}
	id, err := NormalizeMatchID(identifier)
	if err != nil {
		return nil, err
	}
	query, operation := matchOperation(detail)
	budget := s.budget()
	response, err := s.execute(ctx, budget, query, operation, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	var envelope matchEnvelope
	if err := decodeData(response.Data, &envelope); err != nil {
		return nil, protocol("STRATZ returned an invalid match payload")
	}
	if envelope.Match == nil {
		return nil, notFound("Match was not found", map[string]any{"match_id": matchKey(id)})
	}
	if availabilityErr := ensureAvailable(envelope.Match, detail); availabilityErr != nil {
		return nil, availabilityErr
	}
	matchData := mapMatch(envelope.Match, detail)
	names, nameRateLimits, nameWarnings := s.resolveHeroNames(ctx, budget, collectMatchHeroIDs(matchData))
	if len(names) > 0 {
		applyMatchHeroNames(&matchData, names)
	}
	rateLimits := append([]stratz.RateLimit(nil), response.RateLimits...)
	rateLimits = append(rateLimits, nameRateLimits...)
	warnings := matchWarnings(detail)
	warnings = append(warnings, nameWarnings...)
	return &Result[Match]{
		Data:       matchData,
		Raw:        rawData(response.Data),
		RateLimits: rateLimits,
		Warnings:   warnings,
	}, nil
}

// BatchMatches gets up to 25 matches atomically in caller order.
// QueryMatches resolves the exact and player-history branches of the
// consolidated match query. League-history and live are owned by leaguelive.
func (s *Service) QueryMatches(ctx context.Context, request contracts.StratzQueryMatchesRequest) (*Result[MatchQueryData], error) {
	input, err := matchRequestMap(request)
	if err != nil {
		return nil, invalid("Match query input is invalid", map[string]any{"reason": err.Error()})
	}
	mode, _ := input["mode"].(string)
	switch mode {
	case "exact":
		return s.queryExactMatches(ctx, input)
	case "player_history":
		return s.queryPlayerHistory(ctx, input)
	case "league_history", "live":
		return nil, invalid("Match query mode is owned by the league-live domain", map[string]any{"mode": mode})
	default:
		return nil, invalid("Unsupported match query mode", map[string]any{"mode": mode})
	}
}

func (s *Service) queryExactMatches(ctx context.Context, input map[string]any) (*Result[MatchQueryData], error) {
	for key := range input {
		switch key {
		case "mode", "match_ids", "detail_level", "fresh", "include_raw":
		default:
			return nil, invalid("Unsupported exact match field", map[string]any{"field": key})
		}
	}
	values, err := matchAnySlice(input["match_ids"])
	if err != nil || len(values) < 1 || len(values) > s.maxBatchSize {
		return nil, invalid("Exact match query requires between 1 and 25 IDs", nil)
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, invalid("Match ID must be a string", nil)
		}
		ids = append(ids, text)
	}
	detail, detailErr := queryDetail(input, false)
	if detailErr != nil {
		return nil, detailErr
	}
	result, err := s.BatchMatches(ctx, ids, detail)
	if err != nil {
		return nil, err
	}
	return &Result[MatchQueryData]{Data: MatchQueryData{Mode: "exact", Items: result.Data}, Raw: result.Raw, RateLimits: result.RateLimits, Warnings: result.Warnings}, nil
}

func (s *Service) queryPlayerHistory(ctx context.Context, input map[string]any) (*Result[MatchQueryData], error) {
	filters, domainErr := queryPlayerMatchFilters(input)
	if domainErr != nil {
		return nil, domainErr
	}
	detail, detailErr := queryDetail(input, true)
	if detailErr != nil {
		return nil, detailErr
	}
	filters.IncludePlayer = detail == detailLevelPlayers
	result, err := s.ListPlayerMatches(ctx, filters)
	if err != nil {
		return nil, err
	}
	page := &contracts.Page{NextCursor: result.Data.Page.NextCursor, HasMore: result.Data.Page.HasMore}
	return &Result[MatchQueryData]{Data: MatchQueryData{Mode: "player_history", Items: result.Data.Items, Page: page}, Raw: result.Raw, RateLimits: result.RateLimits, Warnings: result.Warnings}, nil
}

// BatchMatches gets up to 25 matches atomically in caller order.
func (s *Service) BatchMatches(
	ctx context.Context,
	identifiers []string,
	detail contracts.DetailLevel,
) (*Result[[]Match], error) {
	detail, err := requireDetail(detail)
	if err != nil {
		return nil, err
	}
	plan, err := batch.NewPlan(identifiers, s.maxBatchSize, func(value string) (string, error) {
		id, normalizeErr := NormalizeMatchID(value)
		if normalizeErr != nil {
			return "", normalizeErr
		}
		return matchKey(id), nil
	})
	if err != nil {
		return nil, batchInputError(err)
	}
	ids := make([]int64, 0, len(plan.Unique()))
	for _, value := range plan.Unique() {
		id, _ := NormalizeMatchID(value)
		ids = append(ids, id)
	}
	query, operation := batchMatchOperation(detail)
	budget := s.budget()
	results := make(map[string]Match, len(ids))
	rawMatches := make([]any, 0, len(ids))
	var rateLimits []stratz.RateLimit
	const matchesPerRequest = 5
	for offset := 0; offset < len(ids); offset += matchesPerRequest {
		end := min(offset+matchesPerRequest, len(ids))
		chunk := ids[offset:end]
		variables := make(map[string]any, matchesPerRequest)
		for index := range matchesPerRequest {
			id := chunk[min(index, len(chunk)-1)]
			variables[fmt.Sprintf("id%d", index)] = id
		}
		response, executeErr := s.execute(
			ctx,
			budget,
			query,
			operation,
			variables,
		)
		if executeErr != nil {
			var domainErr *Error
			if errors.As(executeErr, &domainErr) {
				domainErr.FailedInput = matchKey(chunk[0])
			}
			return nil, executeErr
		}
		var matches map[string]*upstreamMatch
		if err := decodeData(response.Data, &matches); err != nil {
			return nil, protocol("STRATZ returned an invalid match batch payload")
		}
		rawMatches = append(rawMatches, rawData(response.Data))
		rateLimits = append(rateLimits, response.RateLimits...)
		for index, id := range chunk {
			match := matches[fmt.Sprintf("match%d", index)]
			if match == nil {
				continue
			}
			if availabilityErr := ensureAvailable(match, detail); availabilityErr != nil {
				availabilityErr.FailedInput = matchKey(id)
				return nil, availabilityErr
			}
			results[matchKey(match.ID)] = mapMatch(match, detail)
		}
	}
	items, reconstructErr := batch.Reconstruct(plan, results)
	if reconstructErr != nil {
		for index, value := range plan.Inputs() {
			id, _ := NormalizeMatchID(value)
			if _, ok := results[matchKey(id)]; !ok {
				domainErr := notFound("A requested match was not found", nil)
				domainErr.FailedInput = failedInput(index, value)
				return nil, domainErr
			}
		}
		return nil, protocol("STRATZ returned an incomplete match batch")
	}
	warnings := matchWarnings(detail)
	if heroIDs := collectBatchHeroIDs(items); len(heroIDs) > 0 {
		names, nameRateLimits, nameWarnings := s.resolveHeroNames(ctx, budget, heroIDs)
		if len(names) > 0 {
			for index := range items {
				applyMatchHeroNames(&items[index], names)
			}
		}
		rateLimits = append(rateLimits, nameRateLimits...)
		warnings = append(warnings, nameWarnings...)
	}
	return &Result[[]Match]{
		Data:       items,
		Raw:        map[string]any{"matches": rawMatches},
		RateLimits: rateLimits,
		Warnings:   warnings,
	}, nil
}

func matchWarnings(detail contracts.DetailLevel) []string {
	if detail == contracts.DetailLevelFull {
		return []string{fullMatchAvailabilityWarning}
	}
	return []string{}
}

// PlayerMatchFilters contains native and bounded client-side list filters.
type PlayerMatchFilters struct {
	PlayerID               string
	From                   *time.Time
	To                     *time.Time
	HeroID                 *int64
	Role                   *string
	GameModeID             *int64
	LobbyTypeID            *int64
	Result                 *string
	MinimumDurationSeconds *int64
	PatchID                *string
	IncludePlayer          bool
	Limit                  int
	Cursor                 string
}

// ListPlayerMatches returns a bounded filtered page and authenticated continuation.
func (s *Service) ListPlayerMatches(
	ctx context.Context,
	filters PlayerMatchFilters,
) (*Result[PlayerMatchesData], error) {
	return s.ListPlayerMatchesWithBudget(ctx, filters, s.budget())
}

// ListPlayerMatchesWithBudget executes the list using the caller's shared
// per-MCP-call request budget.
func (s *Service) ListPlayerMatchesWithBudget(
	ctx context.Context,
	filters PlayerMatchFilters,
	budget *stratz.RequestBudget,
) (*Result[PlayerMatchesData], error) {
	playerID, err := NormalizePlayerID(filters.PlayerID)
	if err != nil {
		return nil, err
	}
	if filters.Limit == 0 {
		filters.Limit = 20
	}
	if filters.Limit < 1 || filters.Limit > 100 {
		return nil, invalid("Player match limit must be between 1 and 100", nil)
	}
	if filters.From != nil && filters.To != nil && filters.From.After(*filters.To) {
		return nil, invalid("Player match from date must not be after to date", nil)
	}
	if filters.MinimumDurationSeconds != nil &&
		(*filters.MinimumDurationSeconds < 0 || *filters.MinimumDurationSeconds > 21600) {
		return nil, invalid("minimum_duration_seconds must be between 0 and 21600", nil)
	}
	binding := pagination.Binding{
		Tool:             "stratz_query_matches",
		Filters:          filterBinding(playerID, filters),
		PageSize:         filters.Limit,
		Token:            s.token,
		SchemaVersion:    s.schemaVersion,
		OperationVersion: playerListOperationVersion,
	}
	var state pagination.ScanState[int64]
	if filters.Cursor != "" {
		if _, err := s.cursor.Decode(filters.Cursor, binding, &state); err != nil {
			return nil, cursorError(err)
		}
	}
	rawPages := make([]any, 0, s.maxUpstreamRequests)
	var rateLimits []stratz.RateLimit
	query, operation := listPlayerMatchesOperation(filters.IncludePlayer)
	pageSize := filters.Limit
	if pageSize < 20 {
		pageSize = 20
	}
	scan, err := pagination.Scan(ctx, pagination.ScanOptions[int64, upstreamMatch]{
		Limit:    filters.Limit,
		MaxPages: s.maxUpstreamRequests,
		State:    optionalState(filters.Cursor, &state),
		Fetch: func(ctx context.Context, skip *int64) (pagination.Page[int64, upstreamMatch], error) {
			offset := int64(0)
			if skip != nil {
				offset = *skip
			}
			variables := map[string]any{
				"steamAccountId": int64(playerID.AccountID),
				"request":        nativePlayerMatchRequest(filters, pageSize, offset),
			}
			response, executeErr := s.execute(ctx, budget, query, operation, variables)
			if executeErr != nil {
				return pagination.Page[int64, upstreamMatch]{}, executeErr
			}
			var envelope playerMatchesEnvelope
			if decodeErr := decodeData(response.Data, &envelope); decodeErr != nil {
				return pagination.Page[int64, upstreamMatch]{}, protocol("STRATZ returned an invalid player match page")
			}
			if envelope.Player == nil {
				return pagination.Page[int64, upstreamMatch]{}, notFound("Player was not found", map[string]any{"account_id": playerKey(playerID)})
			}
			rawPages = append(rawPages, rawData(response.Data))
			rateLimits = response.RateLimits
			next := offset + int64(len(envelope.Player.Matches))
			hasMore := len(envelope.Player.Matches) == pageSize
			return pagination.Page[int64, upstreamMatch]{
				Items:   envelope.Player.Matches,
				Next:    &next,
				HasMore: hasMore,
			}, nil
		},
		Advance: func(offset *int64, consumed int) *int64 {
			value := int64(consumed)
			if offset != nil {
				value += *offset
			}
			return &value
		},
		Accept: func(match upstreamMatch) bool {
			return filters.MinimumDurationSeconds == nil ||
				(match.DurationSeconds != nil && *match.DurationSeconds >= *filters.MinimumDurationSeconds)
		},
	})
	if err != nil {
		return nil, err
	}
	items := make([]PlayerMatchSummary, 0, len(scan.Items))
	for index := range scan.Items {
		items = append(items, mapPlayerMatchSummary(&scan.Items[index], int64(playerID.AccountID), filters.IncludePlayer))
	}
	warnings := []string{}
	if heroIDs := collectSummaryHeroIDs(items); len(heroIDs) > 0 {
		names, nameRateLimits, nameWarnings := s.resolveHeroNames(ctx, budget, heroIDs)
		if len(names) > 0 {
			applySummaryHeroNames(items, names)
		}
		rateLimits = append(rateLimits, nameRateLimits...)
		warnings = append(warnings, nameWarnings...)
	}
	var nextCursor *string
	if scan.Next != nil {
		encoded, encodeErr := s.cursor.Encode(binding, pagination.LifetimeRecent, scan.Next)
		if encodeErr != nil {
			return nil, &Error{Code: contracts.ErrorCodeInternalError, Message: "Failed to create pagination cursor", Details: map[string]any{}}
		}
		nextCursor = &encoded
	}
	return &Result[PlayerMatchesData]{
		Data: PlayerMatchesData{
			Items: items,
			Page: PageInfo{
				NextCursor: nextCursor,
				HasMore:    nextCursor != nil,
			},
		},
		Raw:        rawPages,
		RateLimits: rateLimits,
		Warnings:   warnings,
	}, nil
}

func (s *Service) budget() *stratz.RequestBudget {
	budget, _ := stratz.NewRequestBudget(s.maxUpstreamRequests)
	return budget
}

// resolveHeroNames best-effort resolves hero display names using the shared
// per-call budget. A nil namer or empty input yields no names. An upstream or
// budget failure yields nil names plus a warning so callers still return hero
// identifiers without the localized name.
func (s *Service) resolveHeroNames(
	ctx context.Context,
	budget *stratz.RequestBudget,
	ids []int64,
) (map[int64]string, []stratz.RateLimit, []string) {
	if s.heroes == nil || len(ids) == 0 {
		return nil, nil, nil
	}
	names, rateLimits, err := s.heroes.HeroNames(ctx, ids, budget)
	if err != nil {
		return nil, rateLimits, []string{heroNameUnavailableWarning}
	}
	return names, rateLimits, nil
}

func (s *Service) execute(
	ctx context.Context,
	budget *stratz.RequestBudget,
	query string,
	operation string,
	variables any,
) (*stratz.Response, error) {
	response, err := s.executor.Execute(ctx, budget, stratz.Request{
		Query:         query,
		OperationName: operation,
		Variables:     variables,
		Mode:          stratz.ModeCurated,
		AllowRetries:  true,
	})
	if err != nil {
		return nil, mapUpstreamError(err)
	}
	if response == nil || len(response.Data) == 0 {
		return nil, protocol("STRATZ returned no curated GraphQL data")
	}
	return response, nil
}

func listPlayerMatchesOperation(includePlayer bool) (string, string) {
	if includePlayer {
		return generated.StratzListPlayerMatchesWithPlayers_Operation, "StratzListPlayerMatchesWithPlayers"
	}
	return generated.StratzListPlayerMatches_Operation, "StratzListPlayerMatches"
}

func matchOperation(detail contracts.DetailLevel) (string, string) {
	switch detail {
	case contracts.DetailLevelSummary, detailLevelPlayers:
		return generated.StratzGetMatchSummary_Operation, "StratzGetMatchSummary"
	case contracts.DetailLevelFull:
		return generated.StratzGetMatchFull_Operation, "StratzGetMatchFull"
	default:
		return generated.StratzGetMatchStandard_Operation, "StratzGetMatchStandard"
	}
}

func batchMatchOperation(detail contracts.DetailLevel) (string, string) {
	switch detail {
	case contracts.DetailLevelSummary, detailLevelPlayers:
		return generated.StratzGetMatchBatchSummary_Operation, "StratzGetMatchBatchSummary"
	case contracts.DetailLevelFull:
		return generated.StratzGetMatchBatchFull_Operation, "StratzGetMatchBatchFull"
	default:
		return generated.StratzGetMatchBatchStandard_Operation, "StratzGetMatchBatchStandard"
	}
}

func ensureAvailable(match *upstreamMatch, detail contracts.DetailLevel) *Error {
	if detail == contracts.DetailLevelSummary || detail == detailLevelPlayers {
		return nil
	}
	summary := mapSummary(match)
	reason := "parse_status"
	if summary.ParseStatus == "parsed" && match.PlaybackData != nil {
		return nil
	}
	if summary.ParseStatus == "parsed" {
		reason = "playback_data"
	}
	return &Error{
		Code:    contracts.ErrorCodeDataNotReady,
		Message: "The requested match detail is not parsed and ready",
		Details: map[string]any{"parse_status": summary.ParseStatus, "missing": reason},
		Context: contracts.MatchAvailabilityContext{
			Type:                 "match_availability",
			Match:                summary,
			RequestedDetailLevel: detail,
		},
	}
}

func nativePlayerMatchRequest(filters PlayerMatchFilters, take int, skip int64) map[string]any {
	request := map[string]any{"take": take, "skip": skip}
	if filters.From != nil {
		request["startDateTime"] = filters.From.Unix()
	}
	if filters.To != nil {
		request["endDateTime"] = filters.To.Unix()
	}
	if filters.HeroID != nil {
		request["heroIds"] = []int64{*filters.HeroID}
	}
	if filters.Role != nil {
		request["roleIds"] = []string{*filters.Role}
	}
	if filters.GameModeID != nil {
		request["gameModeIds"] = []int64{*filters.GameModeID}
	}
	if filters.LobbyTypeID != nil {
		request["lobbyTypeIds"] = []int64{*filters.LobbyTypeID}
	}
	if filters.Result != nil {
		request["isVictory"] = *filters.Result == "win"
	}
	if filters.PatchID != nil {
		request["gameVersionIds"] = []string{*filters.PatchID}
	}
	return request
}

func filterBinding(id PlayerID, filters PlayerMatchFilters) map[string]any {
	result := map[string]any{
		"mode":      "player_history",
		"player_id": playerKey(id),
	}
	if filters.From != nil {
		result["from"] = filters.From.UTC().Format(time.RFC3339)
	}
	if filters.To != nil {
		result["to"] = filters.To.UTC().Format(time.RFC3339)
	}
	if filters.HeroID != nil {
		result["hero_id"] = *filters.HeroID
	}
	if filters.Role != nil {
		result["role"] = *filters.Role
	}
	if filters.GameModeID != nil {
		result["game_mode_id"] = *filters.GameModeID
	}
	if filters.LobbyTypeID != nil {
		result["lobby_type_id"] = *filters.LobbyTypeID
	}
	if filters.Result != nil {
		result["result"] = *filters.Result
	}
	if filters.MinimumDurationSeconds != nil {
		result["minimum_duration_seconds"] = *filters.MinimumDurationSeconds
	}
	if filters.PatchID != nil {
		result["patch_id"] = *filters.PatchID
	}
	if filters.IncludePlayer {
		result["include_player"] = true
	}
	return result
}

func integerValue(value any, fallback int) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, err := number.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func optionalState(cursor string, state *pagination.ScanState[int64]) *pagination.ScanState[int64] {
	if cursor == "" {
		return nil
	}
	return state
}

func rawData(data json.RawMessage) any {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	return value
}

func mapUpstreamError(err error) *Error {
	var upstream *stratz.Error
	if errors.As(err, &upstream) {
		return &Error{
			Code:       upstream.Code,
			Message:    upstream.Message,
			Retryable:  upstream.Retryable,
			RetryAfter: upstream.RetryAfter,
			Details:    upstream.Details,
		}
	}
	return &Error{
		Code:    contracts.ErrorCodeInternalError,
		Message: "Curated STRATZ execution failed internally",
		Details: map[string]any{},
	}
}

func cursorError(err error) *Error {
	var cursorErr *pagination.Error
	if errors.As(err, &cursorErr) {
		return &Error{Code: cursorErr.Code, Message: cursorErr.Message, Details: cursorErr.Details}
	}
	return &Error{Code: contracts.ErrorCodeInternalError, Message: "Pagination cursor validation failed internally", Details: map[string]any{}}
}

func protocol(message string) *Error {
	return &Error{Code: contracts.ErrorCodeUpstreamProtocolError, Message: message, Details: map[string]any{}}
}

func notFound(message string, details map[string]any) *Error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Code: contracts.ErrorCodeNotFound, Message: message, Details: details}
}

func private(message string) *Error {
	return &Error{Code: contracts.ErrorCodePrivate, Message: message, Details: map[string]any{}}
}

func batchInputError(err error) *Error {
	return invalid("Batch input is invalid", map[string]any{"reason": err.Error()})
}
